package specs

import "testing"

// TestContextResetClearsFailFast pins a fix for a Context pool leak: contextPool reuses *Context
// across unrelated Runner.Run calls, and failFast must not survive a Reset the way it used to.
// Without this, a Runner{FailFast: true} in one test could leak failFast=true into a later,
// unrelated Runner{FailFast: false} that happened to draw the same pooled Context — silently
// dropping every group after a failing spec, including a trailing skip-only group, with no
// FailFast requested at all.
func TestContextResetClearsFailFast(t *testing.T) {
	ctx := &Context{}
	ctx.SetFailFast(true)

	ctx.Reset(nil)

	if ctx.failFast {
		t.Fatal("expected Reset to clear failFast, but it stayed true")
	}
}
