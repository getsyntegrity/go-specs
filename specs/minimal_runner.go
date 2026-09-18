// minimal_runner.go implements a high-performance runner: slice of specs, single execution loop
// and optional parallel execution via RunParallel.
//
// Architecture:
//   - Runner holds a slice of RunSpec structs (no interface, no reflection).
//   - Run: one context from pool, single loop; zero allocs.
//   - RunParallel: worker pool, one context per worker from pool; failures recorded by spec index, reported in order.
package specs

import (
	"runtime"
	"runtime/debug"
	"sync"
	"testing"
)

// RunSpec is a single runnable test. Minimal struct for cache efficiency (name + func only).
// The hot path in Run() only reads Fn; Name is for reporting (e.g. on failure).
type RunSpec struct {
	Name string
	Fn   func(*Context)
}

// MinimalRunner runs a flat list of specs with a single context. No interface-based tree, no mutex in execution.
type MinimalRunner struct {
	specs []RunSpec
}

// NewMinimalRunner creates a runner with preallocated capacity to avoid slice growth during registration.
func NewMinimalRunner(capacity int) *MinimalRunner {
	if capacity <= 0 {
		capacity = 64
	}
	return &MinimalRunner{
		specs: make([]RunSpec, 0, capacity),
	}
}

// NewMinimalRunnerFromSpecs creates a runner that runs the given specs. The slice is copied so the
// runner owns it and execution is safe. Use with ShardSpecs to run a shard: ShardSpecs(specs, shard, total)
// then NewMinimalRunnerFromSpecs(sharded). Run and RunParallel are unchanged; no allocations in their loop.
func NewMinimalRunnerFromSpecs(specs []RunSpec) *MinimalRunner {
	if len(specs) == 0 {
		return &MinimalRunner{specs: nil}
	}
	copied := make([]RunSpec, len(specs))
	copy(copied, specs)
	return &MinimalRunner{specs: copied}
}

// Add registers one spec. Allocations happen here (append), not in the execution loop.
func (r *MinimalRunner) Add(name string, fn func(*Context)) {
	if r == nil || fn == nil {
		return
	}
	r.specs = append(r.specs, RunSpec{Name: name, Fn: fn})
}

// RunBatchSize is the number of specs executed in a tight inner loop before the outer loop advances.
// Batched execution improves cache locality (sequential access to specs[i..i+RunBatchSize)) and
// reduces branch overhead. No allocations; context is reused for the whole run.
const RunBatchSize = 8

// Run executes all specs in order. One context from pool, reused for every spec; zero allocs in loop.
// Specs are run in batches of RunBatchSize to improve cache behavior and reduce loop overhead.
//
// A panic in a spec is recovered instead of crashing the process: it's recorded as a failure (via
// ctx.recordFailure + ctx.backend.Errorf, message and stack trace), and the next spec still runs —
// same contract as BlockRunner.Run and the default execution path.
func (r *MinimalRunner) Run(tb testing.TB) {
	if r == nil || tb == nil || len(r.specs) == 0 {
		return
	}
	backend := asTestBackend(tb)
	defer putTestBackend(backend)
	ctx, release := acquireContext(backend)
	defer release()
	ctx.SetPathValues(PathValues{})

	runMinimalSpecs(ctx, r.specs)
}

// runMinimalSpecs runs every spec sequentially against ctx, recovering each one individually. Split
// out from Run so it can be tested without a real testing.TB.
func runMinimalSpecs(ctx *Context, specs []RunSpec) {
	n := len(specs)
	for i := 0; i < n; i += RunBatchSize {
		end := min(i+RunBatchSize, n)
		for j := i; j < end; j++ {
			runMinimalSpecRecovered(ctx, specs[j].Fn)
		}
	}
}

// runMinimalSpecRecovered runs a single spec, recovering a panic so it fails just this spec instead
// of crashing the process. isExpectedAbort sentinels (a controlled backend's FailNow) are already
// recorded by the backend and must not be reported a second time.
func runMinimalSpecRecovered(ctx *Context, fn func(*Context)) {
	defer func() {
		if recovered := recover(); recovered != nil && !isExpectedAbort(recovered) {
			ctx.recordFailure()
			ctx.backend.Errorf("panic: %v\n%s", recovered, debug.Stack())
		}
	}()
	fn(ctx)
}

// RunParallel runs specs across workers goroutines. Each worker reuses one Context from contextPool.
// Failures are recorded by spec index; after all workers finish, the first failure is reported in
// spec order (deterministic). workers <= 0 uses GOMAXPROCS. No allocations in the worker loop.
// tb is used only for reporting (Helper, Fatalf) after workers finish; pass testing.T or a type implementing failureReporter.
func (r *MinimalRunner) RunParallel(tb failureReporter, workers int) {
	r.runParallelWith(tb, workers, runWorker)
}

// RunParallelBatched runs specs in parallel with cache-aware batching. Workers claim chunkSize
// specs at a time (e.g. 16), reducing atomic contention on the shared counter. chunkSize <= 1
// behaves like RunParallel (one spec claimed at a time). Deterministic reporting is unchanged.
// No allocations in the worker loop.
func (r *MinimalRunner) RunParallelBatched(tb failureReporter, workers int, chunkSize int) {
	cs := uint32(chunkSize)
	if cs < 1 {
		cs = 1
	}
	r.runParallelWith(tb, workers, func(specs []RunSpec, backend *parallelBackend, next *uint32, results *[]failureRecord) {
		runWorkerBatched(specs, backend, next, results, cs)
	})
}

// runParallelWith holds the setup shared by RunParallel and RunParallelBatched: worker-count
// clamping, per-worker backend/result state, goroutine dispatch, and deterministic failure
// reporting. Only the per-spec claiming strategy (work) differs between the two callers.
func (r *MinimalRunner) runParallelWith(tb failureReporter, workers int, work func(specs []RunSpec, backend *parallelBackend, next *uint32, results *[]failureRecord)) {
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
