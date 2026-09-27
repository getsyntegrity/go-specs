// parallel_false_sharing_bench_test.go is the measurement harness for issue #157: does the
// contiguous `backends := make([]parallelBackend, workers)` slice in runParallelWith suffer false
// sharing, given that every worker writes its own backend.specIndex once per spec?
//
// The experiment is an A/B that differs in exactly one thing — where the per-worker backends live:
//
//	baseline: one contiguous []parallelBackend, several workers per 64-byte cache line
//	padded:   one []paddedParallelBackend, each backend alone on its own cache line
//
// Everything else (worker function, context pool, atomic counter, results slice, spec bodies) is
// the production code path, called unchanged. If false sharing on specIndex is material, the padded
// arm wins; if the shared `next` counter dominates, both arms move together and padding buys
// nothing.
package specs

import (
	"runtime"
	"sync"
	"testing"
	"unsafe"
)

// benchParallelSpecCount matches the 2000 specs the existing benchmarks/ suite uses, so numbers
// here are comparable with BenchmarkRunner_MinimalParallel_*.
const benchParallelSpecCount = 2000

// cacheLineSize is the x86-64 line size. Only the experiment's padded arm depends on it; nothing in
// production does.
const cacheLineSize = 64

// paddedParallelBackend isolates one parallelBackend on its own cache line. The padding is computed
// from the real struct size so it stays correct if parallelBackend grows.
type paddedParallelBackend struct {
	b parallelBackend
	_ [cacheLineSize - unsafe.Sizeof(parallelBackend{})%cacheLineSize]byte
}

// TestParallelBackendSize records sizeof(parallelBackend) and how many land on one cache line — the
// first datum issue #157 asks for. It is a measurement, not a contract: it never fails, it reports.
func TestParallelBackendSize(t *testing.T) {
	size := unsafe.Sizeof(parallelBackend{})
	t.Logf("sizeof(parallelBackend) = %d bytes; %d per %d-byte cache line; sizeof(paddedParallelBackend) = %d",
		size, cacheLineSize/size, cacheLineSize, unsafe.Sizeof(paddedParallelBackend{}))
	t.Logf("sizeof(failureRecord) = %d bytes; %d per cache line",
		unsafe.Sizeof(failureRecord{}), cacheLineSize/unsafe.Sizeof(failureRecord{}))
}

// newBenchRunner builds a runner of cheap specs: one assertion per spec, so coordination overhead
// (atomic claim, backend write, context reset) is a visible share of the per-spec cost.
func newBenchRunner() *MinimalRunner {
	r := NewMinimalRunner(benchParallelSpecCount)
	for i := 0; i < benchParallelSpecCount; i++ {
		r.Add("spec", func(ctx *Context) { EqualTo(ctx, 1, 1) })
	}
	return r
}

// runParallelPadded is runParallelWith with cache-line-isolated backends. It is deliberately a copy
// rather than a parameterization of the production function: the baseline arm must run the real
// code with no extra indirection, so the only difference between the arms is backend placement.
func runParallelPadded(r *MinimalRunner, tb failureReporter, workers int, work func([]RunSpec, *parallelBackend, *uint32, *[]failureRecord)) {
	if r == nil || tb == nil || len(r.specs) == 0 {
		return
	}
	specs := r.specs
	n := len(specs)
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	if workers > n {
		workers = n
	}
	if workers <= 0 {
		workers = 1
	}

	results := make([]failureRecord, n)
	backends := make([]paddedParallelBackend, workers)
	for i := range backends {
		backends[i].b.results = &results
		backends[i].b.specIndex = -1
		backends[i].b.abortOnFatal = true
	}

	var next uint32
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			work(specs, &backends[w].b, &next, &results)
		}(w)
	}
	wg.Wait()

	reportFailures(tb, results)
}

// --- chunk size 1 (one spec per atomic claim): the arm where per-spec backend writes are densest.

func BenchmarkFalseSharing_Chunk1_Baseline(b *testing.B) {
	r := newBenchRunner()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.RunParallel(b, 0)
	}
}

func BenchmarkFalseSharing_Chunk1_Padded(b *testing.B) {
	r := newBenchRunner()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runParallelPadded(r, b, 0, runWorker)
	}
}

// --- chunk size 16 (DefaultChunkSize): fewer atomic claims, same per-spec backend write rate.

func BenchmarkFalseSharing_Chunk16_Baseline(b *testing.B) {
	r := newBenchRunner()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.RunParallelBatched(b, 0, DefaultChunkSize)
	}
}

func BenchmarkFalseSharing_Chunk16_Padded(b *testing.B) {
	r := newBenchRunner()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runParallelPadded(r, b, 0, func(s []RunSpec, backend *parallelBackend, next *uint32, results *[]failureRecord) {
			runWorkerBatched(s, backend, next, results, DefaultChunkSize)
		})
	}
}

// --- control arm -------------------------------------------------------------------------------
//
// runParallelLocalPlain is a byte-for-byte copy of runParallelPadded with the padding removed: the
// backends live in a plain contiguous []parallelBackend, exactly as production allocates them.
//
// It exists because the Baseline arm changes TWO things at once relative to Padded — it is both
// unpadded AND the production function reached through MinimalRunner.RunParallel. This arm holds
// the function identical and varies only the padding, so:
//
//	LocalPlain vs Padded    -> the effect of padding alone (the actual #157 question)
//	Baseline   vs LocalPlain -> the cost of the harness copy itself (should be ~0)
//
// Without this control, any Baseline-vs-Padded delta is unattributable.
func runParallelLocalPlain(r *MinimalRunner, tb failureReporter, workers int, work func([]RunSpec, *parallelBackend, *uint32, *[]failureRecord)) {
	if r == nil || tb == nil || len(r.specs) == 0 {
		return
	}
	specs := r.specs
	n := len(specs)
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	if workers > n {
		workers = n
	}
	if workers <= 0 {
		workers = 1
	}

	results := make([]failureRecord, n)
	backends := make([]parallelBackend, workers)
	for i := range backends {
		backends[i].results = &results
		backends[i].specIndex = -1
		backends[i].abortOnFatal = true
	}

	var next uint32
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			work(specs, &backends[w], &next, &results)
		}(w)
	}
	wg.Wait()

	reportFailures(tb, results)
}

func BenchmarkFalseSharing_Chunk1_LocalPlain(b *testing.B) {
	r := newBenchRunner()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runParallelLocalPlain(r, b, 0, runWorker)
	}
}

func BenchmarkFalseSharing_Chunk16_LocalPlain(b *testing.B) {
	r := newBenchRunner()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runParallelLocalPlain(r, b, 0, func(s []RunSpec, backend *parallelBackend, next *uint32, results *[]failureRecord) {
			runWorkerBatched(s, backend, next, results, DefaultChunkSize)
		})
	}
}
