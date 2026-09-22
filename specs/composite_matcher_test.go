package specs

import "testing"

// The specs.Not/All/Any re-exports are the surface the README/DSL.md document; nothing called them
// end to end. This proves a composite failure — built out of the sub-matchers' own FailureMessage,
// not a generic "combined matcher failed" — reaches the reporter through the real DSL path exactly
// as an ordinary matcher's does (see TestReExportedMatchersDecideAndExplain and
// TestExpectToReportsTheMatcherMessageWhenItFails in matcher_assertion_test.go for the pattern).
func TestReExportedCompositeMatcherFailureReachesTheReporterIntact(t *testing.T) {
	ctx, b := newCapturedContext()

	ctx.Expect(2).To(All(Equal(1), BeTrue()))

	if !b.failed {
		t.Fatal("expected the backend to record a failure")
	}
	want := `All: #1: "expected 2 to equal 1"; #2: "expected true, got 2 (int)"`
	if b.message != want {
		t.Fatalf("got %q, want %q", b.message, want)
	}
	if !ctx.hasFailed() {
		t.Fatal("expected the context to be marked failed after a composite matcher failure")
	}
}

// Not/Any get the same end-to-end proof, driven through the real Describe/It DSL rather than a
// captured backend, so a composite matcher failure is also checked reaching the compiled execution
// plan and its reporter events — not just Context.recordFailure. Per report/events.go,
// SpecResultEvent.Message stays empty for this ordinary Fatalf-based path (the exact wording is
// pinned above, against the backend that actually receives it); this test is about Failed and the
// suite's failure count, the same properties TestExpectTToFailureReachesTheReporterAndTheSuiteCount
// pins for a plain matcher.
func TestCompositeMatcherFailureReachesTheCompiledPlanReporter(t *testing.T) {
	rep, counter := runSpecThroughPlan(t, func(ctx *Context) {
		ctx.Expect(3).To(Not(Any(Equal(1), Equal(3))))
	})

	if len(rep.specFinished) != 1 {
		t.Fatalf("expected exactly one SpecFinished event, got %d", len(rep.specFinished))
	}
	if !rep.specFinished[0].Failed {
		t.Error("expected SpecResultEvent.Failed=true for a failing composite matcher")
	}
	if counter.failed != 1 {
		t.Errorf("expected SuiteEndEvent.FailedSpecs=1, got %d", counter.failed)
	}
}
