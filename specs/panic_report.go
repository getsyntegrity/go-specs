// panic_report.go holds the one reporting path every execution engine's panic recovery goes
// through. Recovery runs while the goroutine is already unwinding a panic, so anything it touches
// must be assumed hostile: a Context may carry no backend at all, and a runnableBackend may already
// have been released back to runnableBackendPool with its testing.TB cleared. Dereferencing either
// inside a recovery defer turns a recoverable spec panic into a dead test process (#171), which is
// the single failure mode this file exists to make impossible.
package specs

import (
	"fmt"
	"os"
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
