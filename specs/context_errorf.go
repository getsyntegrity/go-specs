// context_errorf.go implements ctx.Errorf and ctx.Helper (#357): a non-fatal failure any package built
// on go-specs, such as mock, can report through, together with the Helper marker such packages call
// first. With ctx.Cleanup (context_cleanup.go), *Context satisfies mock's TB interface on every engine.
package specs

import (
	"fmt"
	"testing"
)

// Errorf reports a non-fatal failure of the current case: it marks the case failed, keeps the message,
// and returns, so the case continues, like testing.T.Errorf. It goes through the same path as a
// failed assertion, so SpecResultEvent.Failed, the suite's failed count and FailFast all see it.
//
// The message is recorded like a failed assertion's (#272) and reported in SpecResultEvent.Message.
// The failure is attributed to the caller of Errorf: on a real *testing.T through Helper marking, and
// on Builder.ItParallel and RunParallel through the stack walk that skips go-specs' own frames (this
// package, snapshots and mock).
//
// Concurrency. Errorf is safe to call from any ctx.Go task, from the spec goroutine and from a cleanup
// at the same time, on the spec's Context or on a task's Context, and it may also be called on a task
// Context after its task ended, in particular from a cleanup a task registered: that is what a
// mock.Controller created with a task's Context does. Every such call fails the owning spec.
//
// How it works. A case that never used ctx.Go or ctx.Cleanup has no other goroutine to race with, so
// Errorf records the failure at once, like a failed assertion. Once the case has task or cleanup state,
// Errorf never touches the spec's failure record or its backend from the calling goroutine. It appends
// the failure to a mutex-protected pending list, and the spec goroutine folds that list into the
// case at the settle points where ctx.Go task failures are already folded: after the body, after the
// AfterEach hooks, and after the cleanups ran. On a real *testing.T the report itself still goes
// straight to the test from the calling goroutine (testing.T is safe for concurrent use), so its
// file:line attribution is unchanged; on the other engines the caller's location is captured at the
// Errorf call and carried with the failure.
//
// Message rule. The first failure's message is the one reported, and a later Errorf never replaces it:
//
//   - Without task or cleanup state, failures apply in call order: the first Errorf, or the assertion
//     that failed before it, is reported. A failed assertion after an Errorf records its own text, as
//     one assertion's message has always replaced an earlier one's on the sequential engines.
//   - With task or cleanup state, an Errorf is deferred to the next settle point. A failure the spec
//     goroutine recorded before that point (a failed assertion in the body) is reported first, then the
//     first failed assertion of a task, then the deferred Errorf calls in the order they reached the
//     pending list. Errorf calls made by different goroutines have no defined relative order, so the
//     message of concurrent Errorf calls is one of theirs, and which one may vary between runs.
//   - Because the fold happens at a settle point, ctx.hasFailed-based reactions such as FailFast see a
//     deferred Errorf at the end of the case, not immediately after the call.
//
// Errorf must not be inlined into its caller: Helper marks the function that called it, and an
// inlined Errorf would mark the user's own frame, moving the reported line to a runner frame.
//
//go:noinline
func (c *Context) Errorf(format string, args ...any) {
	if c == nil || c.backend == nil {
		return
	}
	if c.tb != nil {
		c.tb.Helper()
	}
	msg := fmt.Sprintf(format, args...)
	if gs := c.gs; gs != nil {
		// Task or cleanup state exists: other goroutines may be in Errorf or asserting, so c.failure
		// and the backend belong to the spec goroutine alone. Record under gs.mu; fold applies it.
		f := failureRecord{Failed: true, Message: msg}
		if c.tb == nil {
			f.File, f.Line, _ = parallelCallerLocation()
		}
		gs.mu.Lock()
		gs.pending = append(gs.pending, f)
		gs.mu.Unlock()
		if c.tb != nil {
			// A real testing.TB is safe for concurrent use, and reporting from here keeps testing's
			// file:line attribution: nothing is replayed onto it at fold time.
			c.backend.Errorf("%s", msg)
		}
		return
	}
	if !c.failure.Failed {
		c.failure.Message = msg // Errorf never replaces the message of an earlier failure
	}
	c.failure.Failed = true
	c.backend.Errorf("%s", msg)
}

// Testing returns the testing.TB the current case runs on — the case's own *testing.T, or the
// *testing.B of a benchmark — or nil where there is none: Builder.ItParallel bodies, RunParallel
// workers, fake backends and a nil Context.
//
// It exists for helper packages that report through the Context (Errorf, Cleanup), such as mock: to
// stay out of the reported file:line on a real *testing.T they call Testing().Helper() from their own
// frames, because ctx.Helper() can only mark Context.Helper itself. Where Testing returns nil no such
// marking is needed: the stack walk that attributes those engines' failures skips go-specs' frames.
func (c *Context) Testing() testing.TB {
	if c == nil || c.tb == nil {
		return nil
	}
	return c.tb
}

// Helper marks the calling function as a test helper on the backend that has one, and is a no-op
// otherwise (a nil Context, no backend, parallelBackend). It exists so *Context satisfies the
// interface packages such as mock ask of a test.
//
// Like every wrapper it marks its own frame, not its caller's: testing.T.Helper marks the function
// that called it, and here that function is Context.Helper. A helper package that must be invisible
// in the reported file:line on a real *testing.T calls ctx.Testing().Helper() from its own frames; on
// Builder.ItParallel and RunParallel attribution never needs Helper, because the stack walk skips
// go-specs' frames.
func (c *Context) Helper() {
	if c == nil {
		return
	}
	if c.tb != nil {
		c.tb.Helper()
		return
	}
	if c.backend != nil && liveBackend(c.backend) {
		c.backend.Helper()
	}
}
