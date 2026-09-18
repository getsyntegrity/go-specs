// scheduler.go implements a parallel test runner: worker pool, shared context pool, deterministic reporting.
//
// Specs are compiled into a flat []RunSpec. RunParallel distributes spec indexes via an atomic
// counter; each worker pulls an index, gets a Context from the pool, runs the spec, returns the
// context. Failures are recorded by spec index; after all workers finish, failures are reported
// in spec order (deterministic). No allocations in the worker execution loop.
package specs

import (
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/getsyntegrity/go-specs/report"
)

// goSpecsInternalPackages identifies a stack frame as go-specs's own dispatch code — this package
// and the other library packages a failing assertion's call chain can pass through (snapshots, for
// ctx.Snapshot) — as opposed to the user's own code. Each entry is a full package import path plus
// the trailing "." that separates it from a function/method name in frame.Function, so e.g.
// "github.com/getsyntegrity/go-specs/specs." matches "specs.EqualTo" but not a *subpackage* like
// "github.com/getsyntegrity/go-specs/specs/testdata/parallel_attribution" — module-prefix matching alone
// would wrongly treat every testdata fixture and example under this module as "internal," since they
// share the module without being part of the library itself. Used by parallelCallerLocation to find
// the first frame that isn't ours.
var goSpecsInternalPackages = []string{
	"github.com/getsyntegrity/go-specs/specs.",
	"github.com/getsyntegrity/go-specs/snapshots.",
}

// isGoSpecsInternalFrame reports whether fn (a runtime.Frame.Function value) belongs to one of
// goSpecsInternalPackages.
func isGoSpecsInternalFrame(fn string) bool {
	for _, prefix := range goSpecsInternalPackages {
		if strings.HasPrefix(fn, prefix) {
			return true
		}
	}
	return false
}

// parallelCallerLocation walks the stack from its own caller (parallelBackend.record, invoked by
// every reporting method) outward, skipping every go-specs frame, and returns the first frame
// outside the module — the user's assertion call site, or their own wrapping helper if they have
// one. This is the parallel path's replacement for testing.T.Helper()'s frame-marking (see
// #101/#106): there is no live testing.TB on a worker goroutine to mark, so the location has to be
// captured here, on the still-live stack, while the frame exists, and carried as data (see
// failureRecord) instead.
//
// A "runtime." frame ends the walk with ok=false rather than being reported as a location: real
// call chains always pass through the user's own frame before unwinding into the runtime (the
// goroutine that launched the spec, or It/ItParallel's closure); reaching "runtime." first means the
// failure was recorded from inside go-specs itself with no user frame above it (e.g. a test that
// calls backend.Fatalf directly), where any reported location would be noise, not the user's line.
//
// Only called on the failure path, matching the fast path's allocation contract from #101's AC 8:
// the passing path never calls this.
//
//go:noinline
func parallelCallerLocation() (file string, line int, ok bool) {
	var pcs [64]uintptr
	n := runtime.Callers(2, pcs[:]) // skip runtime.Callers and parallelCallerLocation itself
	frames := runtime.CallersFrames(pcs[:n])
	for {
		frame, more := frames.Next()
		switch {
		case isGoSpecsInternalFrame(frame.Function):
			// internal dispatch frame; keep walking outward
		case strings.HasPrefix(frame.Function, "runtime."):
			return "", 0, false
		default:
			return frame.File, frame.Line, frame.File != ""
		}
		if !more {
			return "", 0, false
		}
	}
}

// The parallel path records into failureRecord, the same type Context carries for the sequential
// path (see failure.go). Message is exactly what a sequential Fatal/Fatalf/Error/Errorf would have
// recorded — see TestParallelStepWithObserverPassesFailureStringStraightToMessage, which pins that
// a report.EventReporter still receives this string verbatim, with no location text mixed in: an
// EventReporter is a structured consumer and can be handed structured data directly, unlike a plain
// `go test` run's terminal output.
//
// File/Line, when captured (File != ""), are the user's assertion call site from
// parallelCallerLocation, resolved while the worker goroutine's frame was still live. By the time
// reportFailures runs, that goroutine is gone, so this is the only source of that information — see
// parallelCallerLocation's doc comment and #108.

// parallelAbort is the sentinel panic value FailNow/Fatal/Fatalf use, when abortOnFatal is set, to
// unwind just the current spec body — mirroring testing.T.FailNow's runtime.Goexit without ever
// touching a live *testing.T from the wrong goroutine. The recovering caller must distinguish this
// from a genuine panic (see parallelStep in program.go).
type parallelAbort struct{}

// parallelBackend implements testBackend by recording failures to results[specIndex].
// One per worker; worker sets specIndex before running each spec. No reflection, no boxing.
type parallelBackend struct {
	specIndex int
	results   *[]failureRecord
	// abortOnFatal, when true, makes FailNow/Fatal/Fatalf panic(parallelAbort{}) after recording,
	// so a fatal assertion stops the rest of the spec body — matching testing.T.FailNow's abort
	// semantics. Both parallelStep (ItParallel, program.go) and RunParallel's worker pool
	// (runWorker/runWorkerBatched, via MinimalRunner.RunParallel/RunParallelBatched) set this true.
	abortOnFatal bool
}

func (p *parallelBackend) Helper() {}

// record stores msg as this spec's failure, together with the caller's location when
// parallelCallerLocation's walk (from record's own caller — Fatal/Fatalf/FailNow/Error/Errorf, one frame up)
// finds one. Shared by all five reporting methods so the walk depth and bounds check live in one
// place.
//
// Failed is set explicitly rather than left for a reader to infer from Message. A FailNow records
// "fail now", but Fatal() with no arguments, Fatalf("%s", "") and a Matcher whose FailureMessage
// returns "" all record a genuine failure whose message is legitimately empty — and every one of
// those used to vanish, because each consumer tested `Message != ""` (#175).
//
//go:noinline
func (p *parallelBackend) record(msg string) {
	if p.results == nil || p.specIndex < 0 || p.specIndex >= len(*p.results) {
		return
	}
	f := failureRecord{Failed: true, Message: msg}
	f.File, f.Line, _ = parallelCallerLocation()
	(*p.results)[p.specIndex] = f
}

func (p *parallelBackend) FailNow() {
	p.record("fail now")
	if p.abortOnFatal {
		panic(parallelAbort{})
	}
}

func (p *parallelBackend) Fatal(args ...any) {
	p.record(fmt.Sprint(args...))
	if p.abortOnFatal {
		panic(parallelAbort{})
	}
}

func (p *parallelBackend) Fatalf(format string, args ...any) {
	p.record(fmt.Sprintf(format, args...))
	if p.abortOnFatal {
		panic(parallelAbort{})
	}
}

// Error/Errorf never abort, matching testing.T.Error's semantics (unlike Fatal/Fatalf/FailNow
// above, they record the failure but must not panic even when abortOnFatal is set).
func (p *parallelBackend) Error(args ...any) {
	p.record(fmt.Sprint(args...))
}

func (p *parallelBackend) Errorf(format string, args ...any) {
	p.record(fmt.Sprintf(format, args...))
}

func (p *parallelBackend) Log(args ...any)     {}
func (p *parallelBackend) Logf(string, ...any) {}

func (p *parallelBackend) Name() string { return "" }

func (p *parallelBackend) Cleanup(func()) {}

// Run reports subtests as unsupported instead of silently skipping them. Parallel mode (ItParallel,
// RunParallel, RunParallelBatched) has no mechanism to run a subtest body against a live testing.TB
// from a worker goroutine, so fn is never called; unlike the old no-op, that is now a fatal failure
// on this spec (via Fatalf, so it also aborts the rest of the spec body when abortOnFatal is set —
// always true for every parallelBackend constructed in this package) instead of vanishing silently.
// Specs that need t.Run should use the sequential runner (MinimalRunner.Run).
func (p *parallelBackend) Run(name string, fn func(testing.TB)) {
	p.Fatalf("t.Run(%q, ...) is not supported in parallel mode (ItParallel/RunParallel/RunParallelBatched); the subtest was not run — use the sequential runner for specs that need t.Run", name)
}

// runWorker runs specs whose indexes it acquires via next. Uses one Context from the pool for
// the whole worker lifetime; resets it per spec. Backend is the worker's dedicated parallelBackend.
// No allocations in the loop: context from pool, backend is preallocated, specs slice is read-only.
func runWorker(specs []RunSpec, backend *parallelBackend, next *uint32, results *[]failureRecord) {
	ctx, release := acquireContext(backend)
	defer release()

	n := uint32(len(specs))
	for {
		i := atomic.AddUint32(next, 1) - 1
		if i >= n {
			return
		}
		idx := int(i)
		backend.specIndex = idx
		ctx.Reset(backend)
		ctx.SetPathValues(PathValues{})
		runWorkerSpec(specs[idx].Fn, ctx, results, idx)
		ctx.Reset(nil)
	}
}

// runWorkerSpec runs one spec body, recovering the parallelAbort{} sentinel a fatal assertion
// panics with when backend.abortOnFatal is set (see parallelBackend.FailNow/Fatal/Fatalf) — an
// expected stop, already recorded in results[idx], not a failure to report. Any other panic is
// recorded as an ordinary spec failure instead of crashing the worker goroutine. Mirrors
// parallelStep's per-spec recover in program.go, applied per spec here too so one spec's fatal
// assertion or panic doesn't stop the worker from running the rest of its specs.
func runWorkerSpec(fn func(*Context), ctx *Context, results *[]failureRecord, idx int) {
	defer func() {
		switch r := recover(); r {
		case nil, parallelAbort{}:
		default:
			if !(*results)[idx].Failed {
				(*results)[idx] = failureRecord{Failed: true, Message: fmt.Sprintf("panic: %v", r)}
			}
		}
	}()
	fn(ctx)
}

// failureReporter is the minimal interface needed to report failures (avoids requiring full testing.TB in tests).
type failureReporter interface {
	Helper()
	Fatalf(format string, args ...any)
}

// reportFailures reports the first failure in spec index order (deterministic). tb.Fatalf's own
// decoration still names whichever internal frame called it here — there is no live worker frame
// left to mark as a helper (see parallelCallerLocation) — so a captured location is embedded in the message
// text itself via failureRecord.text(), the only way it can reach a plain `go test` run's output at
// all. See #108.
func reportFailures(tb failureReporter, results []failureRecord) {
	for i, r := range results {
		if r.Failed {
			tb.Helper()
			tb.Fatalf("spec[%d]: %s", i, r.text())
			return
		}
	}
}

// RunShard runs a shard of the compiled Program for CI. Sharding operates on already-coalesced
// hook groups (specs sharing a BeforeEach/AfterEach are compiled into one group), not individual
// specs: group indices are assigned to shards by gi % shardCount == shardIndex. A suite with many
// specs under one shared hook lands its whole group on a single shard, so shard runtimes can be
// uneven when hook groups are large or unevenly sized. shardCount must be > 0 and
// 0 <= shardIndex < shardCount. Allocation happens once to build the shard's Program; the runner
// loop is allocation-free.
func RunShard(program *Program, tb testing.TB, shardIndex, shardCount int) {
	if program == nil || tb == nil {
		return
	}
	prog, ok := shardProgram(program, shardIndex, shardCount)
	if !ok {
		return
	}
	NewRunner(prog).Run(tb)
}

// RunShardWithReporter is RunShard with reporting: the runner it builds for the shard's Program
// reports SuiteStarted/SuiteFinished and SpecStarted/SpecFinished exactly as NewRunnerWithReporter's
// Runner would, so SuiteEndEvent.TotalSpecs/FailedSpecs describe only the specs this shard actually
// executed, not the full Program's total — the sharding itself is unchanged from RunShard.
func RunShardWithReporter(program *Program, tb testing.TB, shardIndex, shardCount int, name string, rep report.EventReporter) {
	if program == nil || tb == nil {
		return
	}
	prog, ok := shardProgram(program, shardIndex, shardCount)
	if !ok {
		return
	}
	NewRunnerWithReporter(prog, name, rep).Run(tb)
}

// shardProgram returns the Program for one shard: its groups (sharded groups when shardCount is
// valid, all groups otherwise), or ok=false if this shard has nothing to run.
func shardProgram(program *Program, shardIndex, shardCount int) (prog *Program, ok bool) {
	if shardCount <= 0 || shardIndex < 0 || shardIndex >= shardCount {
		return program, true
	}
	groups := program.Groups
	sharded := make([]group, 0, len(groups)/shardCount+1)
	for gi := range groups {
		if gi%shardCount == shardIndex {
			sharded = append(sharded, groups[gi])
		}
	}
	if len(sharded) == 0 {
		return nil, false
	}
	return &Program{Groups: sharded}, true
}
