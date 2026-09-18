// recovery.go holds the per-spec panic-recovery invariant, in one place, for every engine that
// executes spec bodies (#176).
//
// The rule itself has two dialects, because the two execution shapes report failures differently:
// a sequential engine writes straight to ctx.backend, while a parallel worker writes into an
// indexed results slice so the report stays deterministic no matter which goroutine finished first.
// Both are here so a change to either is a one-line change, not a sweep across five files — the
// sweep is what made #62, #63, #64 and #65 four separate issues for one missing invariant.
//
// Both helpers take recover()'s result rather than calling recover() themselves: recover only
// works when called directly by a deferred function, so the call has to stay at the engine's own
// defer. Passing recover() as the argument keeps it there while moving the decision here.
package specs

import (
	"fmt"
	"runtime/debug"
)

// recoverSpecFailure records a recovered spec panic on ctx as an ordinary failure, so the spec
// fails instead of the process crashing and the engine's loop can carry on with the next spec.
//
// A nil value means the spec returned normally and nothing is recorded. An isExpectedAbort sentinel
// — what a controlled backend's FailNow/Fatal/Fatalf panics with — is a stop the backend already
// recorded, so it is skipped too: reporting it again would turn one assertion failure into two, the
// second carrying a stack trace into the sentinel rather than into the spec.
//
// The stack trace is captured here, which puts this frame at the top of the reported trace. That is
// the single intentional difference from the per-engine copies this replaces.
func recoverSpecFailure(ctx *Context, recovered any) {
	if recovered == nil || isExpectedAbort(recovered) {
		return
	}
	ctx.recordFailure()
	ctx.backend.Errorf("panic: %v\n%s", recovered, debug.Stack())
}

// recoverParallelSpecFailure records a recovered panic from a worker goroutine into results[idx],
// keyed by spec index so reportFailures can emit failures in deterministic spec order regardless of
// which worker finished first. Recovering here is what stops one spec's panic from crashing the
// whole process: an unrecovered panic in a spawned goroutine takes the process down with it.
//
// A nil value means the spec returned normally. parallelAbort{} is the sentinel a fatal assertion
// panics with when the backend has abortOnFatal set — an expected stop already recorded in
// results[idx], not a failure to report. An existing message wins: the first recorded failure for a
// spec is the one that explains it, and a later panic must not overwrite it.
func recoverParallelSpecFailure(recovered any, results *[]parallelFailure, idx int) {
	switch recovered {
	case nil, parallelAbort{}:
		return
	}
	if (*results)[idx].Message == "" {
		(*results)[idx] = parallelFailure{Message: fmt.Sprintf("panic: %v", recovered)}
	}
}
