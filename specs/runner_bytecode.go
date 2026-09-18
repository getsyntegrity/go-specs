// runner_bytecode.go runs a compiled bytecode program with a single loop (Run) or worker pool (RunParallel).
//
// No tree traversal: the program is a flat []instruction. Run uses one context from the pool;
// RunParallel uses spec boundaries (SpecStarts) so each worker runs a contiguous instruction range per spec.
// Hot path remains allocation-free; no reflection, no interface dispatch in the loop.
package specs

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"testing"
)

// BytecodeRunner runs a BCProgram built by BCBuilder.
type BytecodeRunner struct {
	program BCProgram
}

// NewBytecodeRunner creates a runner for the given bytecode program. Program is used as-is (not copied).
func NewBytecodeRunner(p BCProgram) *BytecodeRunner {
	return &BytecodeRunner{program: p}
}

// Run executes all specs in order. One context from the pool, reused for every spec.
//
// A panic anywhere in one spec's instruction range is recovered instead of crashing the process:
// it's recorded as a failure (via ctx.recordFailure + ctx.backend.Errorf, message and stack trace),
// and the next spec still runs. Recovery is per spec, not per instruction, matching RunParallel's
// granularity.
func (r *BytecodeRunner) Run(tb testing.TB) {
	if r == nil || tb == nil || r.program.BCLen() == 0 {
		return
	}
	backend := asTestBackend(tb)
	defer putTestBackend(backend)
	ctx, release := acquireContext(backend)
	defer release()
	ctx.SetPathValues(PathValues{})

	runBytecodeSequential(ctx, r.program.Code, r.program.SpecStarts)
}

// runBytecodeSequential runs every spec's instruction range in order against ctx, recovering each
// spec as a whole unit. Split out from Run so it can be tested without a real testing.TB.
func runBytecodeSequential(ctx *Context, code []instruction, starts []int) {
	nSpecs := len(starts) - 1
	for si := 0; si < nSpecs; si++ {
		runBytecodeSpecRecovered(ctx, code, starts[si], starts[si+1])
	}
}

// runBytecodeSpecRecovered runs one spec's instruction range, recovering a panic so it fails just
// this spec instead of crashing the process. isExpectedAbort sentinels (a controlled backend's
// FailNow) are already recorded by the backend and must not be reported a second time.
func runBytecodeSpecRecovered(ctx *Context, code []instruction, start, end int) {
	defer func() {
		if recovered := recover(); recovered != nil && !isExpectedAbort(recovered) {
			ctx.recordFailure()
			ctx.backend.Errorf("panic: %v\n%s", recovered, debug.Stack())
		}
	}()
	for i := start; i < end; i++ {
		if code[i].fn != nil {
			code[i].fn(ctx)
		}
	}
}

// RunParallel runs each spec (instruction range) on a worker pool. Workers pull spec indexes via
// atomic counter; each worker reuses one Context. Failures are recorded by spec index and reported
// in order (deterministic). No allocations in the worker loop.
func (r *BytecodeRunner) RunParallel(tb failureReporter, workers int) {
	if r == nil || tb == nil || r.program.BCLen() == 0 {
		return
	}
	nSpecs := r.program.NumSpecs()
	if nSpecs == 0 {
		return
	}
	code := r.program.Code
	starts := r.program.SpecStarts
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	if workers > nSpecs {
		workers = nSpecs
	}
	if workers <= 0 {
		workers = 1
	}

	results := make([]failureRecord, nSpecs)
	backends := make([]parallelBackend, workers)
	for i := range backends {
		backends[i].results = &results
		backends[i].specIndex = -1
	}

	var next uint32
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			runBytecodeWorker(code, starts, nSpecs, &backends[workerID], &next, &results)
		}(w)
	}
	wg.Wait()

	reportFailures(tb, results)
}

// runBytecodeWorker runs spec ranges whose spec index it acquires via next. One context per worker.
// No allocations in the loop: context from pool, backend preallocated, code/starts read-only.
func runBytecodeWorker(code []instruction, starts []int, nSpecs int, backend *parallelBackend, next *uint32, results *[]failureRecord) {
	ctx, release := acquireContext(backend)
	defer release()

	for {
		s := atomic.AddUint32(next, 1) - 1
		if s >= uint32(nSpecs) {
			return
		}
		si := int(s)
		start := starts[si]
		end := starts[si+1]
		backend.specIndex = si
		ctx.Reset(backend)
		ctx.SetPathValues(PathValues{})
		runBytecodeWorkerSpec(code, start, end, ctx, results, si)
		ctx.Reset(nil)
	}
}

// runBytecodeWorkerSpec runs one spec's instruction range, recovering the parallelAbort{} sentinel a
// fatal assertion panics with when backend.abortOnFatal is set — an expected stop, already recorded
// in results[idx], not a failure to report. Any other panic is recorded as an ordinary spec failure
// instead of crashing the worker goroutine — which, since this runs inside a spawned goroutine, would
// otherwise crash the entire process. Mirrors scheduler.go's runWorkerSpec.
func runBytecodeWorkerSpec(code []instruction, start, end int, ctx *Context, results *[]failureRecord, idx int) {
	defer func() {
		switch r := recover(); r {
		case nil, parallelAbort{}:
		default:
			if !(*results)[idx].Failed {
				(*results)[idx] = failureRecord{Failed: true, Message: fmt.Sprintf("panic: %v", r)}
			}
		}
	}()
	for i := start; i < end; i++ {
		if code[i].fn != nil {
			code[i].fn(ctx)
		}
	}
}
