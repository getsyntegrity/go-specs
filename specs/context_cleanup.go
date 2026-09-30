// context_cleanup.go implements ctx.Cleanup (#357): the per-case seam that lets a package built on
// go-specs, such as mock, register teardown work on any engine without knowing which engine runs the
// case. Its companion, ctx.Errorf, is in context_errorf.go.
//
// The problem it solves: on a real *testing.T, t.Cleanup is enough, but ctx.T is nil inside
// Builder.ItParallel and RunParallel, and their backends drop Cleanup silently. A package that
// verifies expectations "when the case ends" therefore had no place to hang that check on those
// engines. So the cleanup list is owned by the Context and drained by the engines themselves, at the
// seam where they already finish a case: right after the last ctx.Go settle, which is itself after
// AfterEach. One mechanism for every engine keeps the ordering identical everywhere.
//
// Storage. The list lives in the case's goState (context_go.go), the lazily allocated per-case
// bookkeeping a task context already shares with its spec. That is what lets a cleanup registered from
// inside a ctx.Go task land on the spec's list, and what makes the cost model the same as ctx.Go's: a
// case that never calls Cleanup pays one nil check at the end of the case and nothing else. The list is
// cleared with the rest of the bookkeeping when the next case starts (resetFailure), so a pooled
// Context can never run one case's cleanups in another.
package specs

import (
	"fmt"
	"runtime/debug"
)

// cleanupFinishedMessage is the panic raised when ctx.Cleanup is called on a Context whose case is
// over. Like ctx.Go's, it fails loudly at the offending call: the function would otherwise be attached
// to a Context idle in the pool, and run at the end of whichever unrelated case takes it next.
const cleanupFinishedMessage = "specs: ctx.Cleanup called after its case finished. " +
	"ctx.Cleanup registers a function that runs when the case ends, so it must be called while the case " +
	"is running: from the It body, a BeforeEach/AfterEach hook, or a ctx.Go task. " +
	"Register teardown before starting work that outlives the case, not from a goroutine launched with a plain `go` statement"

const cleanupNilMessage = "specs: ctx.Cleanup requires a non-nil function; pass the teardown as func()"

// Cleanup registers fn to run when the current case ends, on every execution engine (Spec.It,
// Spec.ItParallel, Builder.It, Builder.ItParallel, RunParallel and the flat runners) and on a fake
// backend. It is the case-scoped counterpart of testing.T's Cleanup, and the hook that packages such as
// mock build their automatic verification on.
//
// The contract:
//
//   - fn runs after the case's AfterEach hooks and after every ctx.Go task has finished, so it sees the
//     final state of everything the case touched.
//   - Cleanups run last registered first (LIFO), like defer and testing.T.Cleanup.
//   - They run however the body ended: normally, after a failed fatal assertion (ctx.Expect, or
//     ctx.T.FailNow on a real *testing.T), or after a panic.
//   - A panic inside fn is recovered and reported as an error of this case, exactly like a panic in the
//     body (a stack trace in the report, so it is classified as an error rather than a failure); the
//     remaining cleanups still run.
//   - fn may call ctx.Errorf or an assertion to report a failure of the case. A fatal assertion ends
//     only that cleanup; the rest still run.
//   - A cleanup registered from a ctx.Go task goes onto the spec's list, and fn may itself register
//     another cleanup while the list is draining; it runs before the drain ends. A failure that such a
//     cleanup reports through the task's own (finished) Context, such as task.Errorf, is folded into the
//     spec when the drain ends, so it fails the spec on every engine.
//   - It is not delegated to ctx.T.Cleanup, even where a real subtest T is bound, so the order is the
//     same everywhere. Consequently ctx.Cleanup functions run before anything registered with
//     ctx.T.Cleanup, which testing runs after the subtest function returns.
//   - Outside a case body, in a BeforeAll/AfterAll hook, the cleanups run when that hook returns.
//
// Calling Cleanup once the case is over panics with a specs: message, but only until the pooled Context
// is reused by a later case; after that a stale handle is indistinguishable from the new case's own,
// exactly as for ctx.Go. Retaining ctx past its case is unsupported. A nil fn panics immediately.
//
// A case that never calls Cleanup pays one nil check and no allocation.
func (c *Context) Cleanup(fn func()) {
	if fn == nil {
		panic(cleanupNilMessage)
	}
	if c == nil || c.backend == nil {
		panic(cleanupFinishedMessage)
	}
	gs := c.gs
	if gs == nil {
		// Only a case's own Context gets here: a task context is created with its state in place.
		gs = &goState{}
		c.gs = gs
	}
	gs.mu.Lock()
	if gs.cleanupsDone {
		gs.mu.Unlock()
		panic(cleanupFinishedMessage)
	}
	gs.cleanups = append(gs.cleanups, fn)
	gs.mu.Unlock()
}

// runCleanups is the engines' end-of-case seam for Cleanup: it runs every registered cleanup, last
// registered first, and returns the first cleanup panic's message and stack, already reported through
// reportRecoveredPanic like a body panic. The nil check comes first and the rest lives in
// drainCleanups, so the call inlines to a single load and branch for a case that never used Cleanup or
// ctx.Go.
func (c *Context) runCleanups() (message, output string) {
	if c == nil || c.gs == nil {
		return "", ""
	}
	return c.gs.drainCleanups(c, true)
}

// runCleanupsQuiet is runCleanups for the parallel engines, whose panics are recorded into a per-spec
// failureRecord instead of a backend (see settleParallelTasks): the panic is returned, not reported.
func (c *Context) runCleanupsQuiet() (message, output string) {
	if c == nil || c.gs == nil {
		return "", ""
	}
	return c.gs.drainCleanups(c, false)
}

// drainCleanups pops and runs cleanups until the list is empty, then marks it done so a later
// Cleanup on this case panics. report says whether a panic is delivered to c's backend.
func (gs *goState) drainCleanups(c *Context, report bool) (message, output string) {
	for {
		gs.mu.Lock()
		n := len(gs.cleanups)
		if n == 0 {
			gs.cleanupsDone = true
			gs.mu.Unlock()
			// A cleanup, including one a task registered, may report through a task Context that is
			// already finished (mock.NewController(task).Verify) or through ctx.Errorf: nothing folds
			// after the cleanups, so fold here, on the spec goroutine, before the case is finalized.
			gs.fold(c)
			return message, output
		}
		fn := gs.cleanups[n-1]
		gs.cleanups[n-1] = nil
		gs.cleanups = gs.cleanups[:n-1]
		gs.mu.Unlock()
		if m, o := gs.runCleanup(c, fn, report); message == "" {
			message, output = m, o
		}
	}
}

// runCleanup runs one cleanup, recovering a panic so it fails just this case and cannot stop the
// remaining cleanups. The deferred function also covers runtime.Goexit, which is how a fatal
// assertion on a real *testing.T ends the goroutine: recover() cannot observe it, so an unfinished
// fn with no recovered value is that case, and the rest of the list is drained from the defer,
// because Goexit will not return to the loop in drainCleanups.
func (gs *goState) runCleanup(c *Context, fn func(), report bool) (message, output string) {
	completed := false
	defer func() {
		if completed {
			return
		}
		if recovered := recover(); recovered != nil && !isTaskAbort(recovered) {
			message = fmt.Sprintf("panic in cleanup: %v", recovered)
			output = string(debug.Stack())
			if report {
				reportRecoveredPanic(c, message, output)
			}
		}
		if m, o := gs.drainCleanups(c, report); message == "" {
			message, output = m, o
		}
	}()
	fn()
	completed = true
	return
}
