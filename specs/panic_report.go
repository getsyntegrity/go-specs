// panic_report.go holds the one reporting path every execution engine's panic recovery goes
// through. Recovery runs while the goroutine is already unwinding a panic, so anything it touches
// must be assumed hostile: a Context may carry no backend at all, and a runnableBackend may already
// have been released back to runnableBackendPool with its testing.TB cleared. Dereferencing either
// inside a recovery defer turns a recoverable spec panic into a dead test process (#171), which is
// the single failure mode this file exists to make impossible.
//
// It also holds the two engine-facing wrappers that decide WHETHER a recovered value is a failure at
// all — recoverSpecFailure for the sequential engines, recoverParallelSpecFailure for worker
// goroutines (#176). They live here, next to reportRecoveredPanic rather than in a file of their
// own, so there is exactly one place to look for "what happens when a spec panics" and no second
// reporting authority can grow beside this one.
package specs

import (
	"fmt"
	"os"
	"runtime/debug"
)

// undeliverablePanicPrefix opens the stderr notice emitted when a recovered panic cannot reach any
// backend. It is a constant so the regression can pin the contract rather than the wording.
const undeliverablePanicPrefix = "specs: recovered panic could not be reported to the test backend"

// reportRecoveredPanic is the whole contract for reporting a recovered panic, and the only thing an
// engine's recovery defer should call.
//
// The failure is recorded on ctx first and unconditionally, because that is what survives a missing
// backend: the recorded failure drives FailFast, and the message/output the caller returns still
// reach the reporter's SpecResultEvent. Delivery to the backend is best effort on top of that. When
// it cannot happen — no backend, a released one, or one that panics while reporting — the panic is
// written to stderr instead of being swallowed. A panic that cannot be reported is never converted
// into a pass.
//
// message and output keep the wire format every engine already used: Errorf("%s\n%s", ...).
func reportRecoveredPanic(ctx *Context, message, output string) {
	ctx.recordFailure()
	if deliverPanicReport(ctx.reportingBackend(), message, output) {
		return
	}
	fmt.Fprintf(os.Stderr, "%s (it was absent, already released, or unusable); the spec is still recorded as failed:\n%s\n%s\n",
		undeliverablePanicPrefix, message, output)
}

// reportingBackend returns the backend a recovery defer may report to, or nil. It exists so the
// defer never loads ctx.backend from a Context it has not proven non-nil.
func (c *Context) reportingBackend() testBackend {
	if c == nil {
		return nil
	}
	return c.backend
}

// deliverPanicReport reports message/output through b and says whether that actually happened.
//
// The liveBackend check catches the two states that are knowable up front. The recover() catches
// everything else: a backend released concurrently between the check and the call, a fake backend
// whose Errorf panics, a nil pointer behind some other testBackend implementation. Recovery must not
// be able to fail, so the last line of defence is to absorb the reporting attempt itself.
func deliverPanicReport(b testBackend, message, output string) (delivered bool) {
	if !liveBackend(b) {
		return false
	}
	defer func() {
		if recover() != nil {
			delivered = false
		}
	}()
	b.Errorf("%s\n%s", message, output)
	return true
}

// liveBackend reports whether b can still accept a failure report. A nil interface obviously cannot.
// A *runnableBackend whose tb is nil is one putTestBackend has already released to the pool: it is a
// non-nil interface value whose every method dereferences that nil tb. Any other implementation is
// assumed usable and proven so by deliverPanicReport's recover.
func liveBackend(b testBackend) bool {
	switch v := b.(type) {
	case nil:
		return false
	case *runnableBackend:
		return v != nil && v.tb != nil
	default:
		return true
	}
}

// recoverSpecFailure is what a sequential engine's recovery defer calls with recover()'s result. It
// decides whether the recovered value is a failure at all, builds the message/output pair in the
// wire format every engine already used, and hands the reporting itself to reportRecoveredPanic —
// which stays the only path to a backend, so #171's guarantees apply unchanged to every engine, and
// to any engine added later.
//
// A nil value means the step returned normally. An isExpectedAbort sentinel — what a controlled
// backend's FailNow/Fatal/Fatalf panics with — is a stop the backend already recorded, so it is not
// reported again and yields no message: re-reporting it turns one assertion failure into two, the
// second carrying a stack trace into the sentinel rather than into the spec.
//
// label distinguishes a before/spec panic ("panic") from an after-hook one ("panic in after hook").
// The returned pair is what the plan and Runner models feed into specFinished's specResult; engines
// that emit no events ignore it.
//
// It takes recover()'s result rather than calling recover itself, because recover only works when
// called directly by a deferred function, so that call has to stay at the engine's own defer. The
// stack is captured here, which puts this frame at the top of the reported trace.
func recoverSpecFailure(ctx *Context, recovered any, label string) (message, output string) {
	if recovered == nil || isExpectedAbort(recovered) {
		return "", ""
	}
	message = fmt.Sprintf("%s: %v", label, recovered)
	output = string(debug.Stack())
	reportRecoveredPanic(ctx, message, output)
	return message, output
}

// recoverParallelSpecFailure is the worker-goroutine counterpart: it records a recovered panic into
// results[idx], keyed by spec index so reportFailures can emit failures in deterministic spec order
// however the workers happen to interleave. Recovering here is what stops one spec's panic from
// killing the process — an unrecovered panic in a spawned goroutine takes the whole process with it.
//
// It deliberately does not go through reportRecoveredPanic, and is not a second reporting authority:
// it touches no backend at all, which is what makes it safe by construction against #171's failure
// mode, and why the sequential rule cannot simply be reused here.
//
// A nil value means the spec returned normally. parallelAbort{} is the sentinel a fatal assertion
// panics with when the backend has abortOnFatal set — an expected stop already recorded in
// results[idx]. An existing record wins: the first recorded failure is the one that explains the
// spec, and a later panic must not overwrite it.
//
// "Already recorded" is read from failureRecord.Failed, never from a non-empty Message (#175). A
// fatal assertion can record a real failure with no text at all — backend.Fatal() with no arguments,
// or a Matcher whose FailureMessage returns "" — and a message-based check would treat that slot as
// empty and overwrite the assertion failure with the abort panic that followed it.
func recoverParallelSpecFailure(recovered any, results *[]failureRecord, idx int) {
	switch recovered {
	case nil, parallelAbort{}:
		return
	}
	if !(*results)[idx].Failed {
		(*results)[idx] = failureRecord{Failed: true, Message: fmt.Sprintf("panic: %v", recovered)}
	}
}
