// context_errorf.go implements ctx.Errorf and ctx.Helper (#357): a non-fatal failure any package built
// on go-specs, such as mock, can report through, together with the Helper marker such packages call
// first. With ctx.Cleanup (context_cleanup.go), *Context satisfies mock's TB interface on every engine.
package specs

import "fmt"

// Errorf reports a non-fatal failure of the current case: it marks the case failed, keeps the message,
// and returns, so the case continues, like testing.T.Errorf. It goes through the same path as a
// failed assertion, so SpecResultEvent.Failed, the suite's failed count and FailFast all see it.
//
// The message is recorded like a failed assertion's (#272) and reported in SpecResultEvent.Message.
// When a case has already failed, Errorf still reaches the backend but never replaces the recorded
// message, so the first Errorf (or the assertion that failed before it) is the one reported. That is
// one-directional: a failed assertion after an Errorf records its own text, as one assertion's message
// has always replaced an earlier one's on the sequential engines. The failure is attributed to the caller of Errorf: on a real *testing.T
// through Helper marking, and on Builder.ItParallel and RunParallel through the stack walk that
// skips go-specs' own frames (this package, snapshots and mock).
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
	if !c.failure.Failed {
		c.failure.Message = msg // Errorf never replaces the message of an earlier failure
	}
	c.failure.Failed = true
	c.backend.Errorf("%s", msg)
}

// Helper marks the calling function as a test helper on the backend that has one, and is a no-op
// otherwise (a nil Context, no backend, parallelBackend). It exists so *Context satisfies the
// interface packages such as mock ask of a test.
//
// Like every wrapper it marks its own frame, not its caller's: testing.T.Helper marks the function
// that called it, and here that function is Context.Helper. A helper package that must be invisible in
// the reported file:line on a real *testing.T should be handed ctx.T; on Builder.ItParallel and
// RunParallel attribution never needs Helper, because the stack walk skips go-specs' frames.
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
