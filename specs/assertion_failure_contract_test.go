package specs

import (
	"testing"

	"github.com/getsyntegrity/go-specs/report"
)

// This file is the contract for #175: every public built-in assertion entry point records its
// failure through the one authoritative path (Context.failf, see failure.go), so what a reporter
// and a suite count observe can never diverge from what the backend was told.
//
// The unit tests in matcher_assertion_test.go assert the mechanism (the failure record itself).
// These assert the observable result — report.SpecResultEvent.Failed and
// report.SuiteEndEvent.FailedSpecs — for every entry point at once, which is what #115 (snapshot)
// and #149 (typed matcher) actually broke: in both, the backend heard about the failure while the
// reporter was told the spec passed. Fixing those two call sites individually left every other one
// free to make the same mistake. A table keyed by entry point is what makes "one of them forgot"
// fail a build instead of shipping a green report on a red run.

// builtInAssertionEntryPoint is one public assertion surface, in both of its outcomes. Adding an
// entry point to the framework without adding it here is the gap this table exists to close.
type builtInAssertionEntryPoint struct {
	// fail must fail through the real assertion path — not ctx.recordFailure(), which is the
	// execution paths' own entry and would prove nothing about the assertion.
	fail func(ctx *Context)
	// pass must succeed through that same path, so the table also pins that the entry point does
	// not report a failure that never happened.
	pass func(ctx *Context)
}

// snapshotBaselineValue is stored in __snapshots__/assertion_failure_contract_test.snap.json under
// "contract-baseline"; it is the only deterministic way to drive Context.Snapshot's passing branch.
var snapshotBaselineValue = map[string]any{"contract": "baseline"}

// builtInAssertionEntryPoints is every public assertion surface go-specs ships. Keep it exhaustive:
// the value of this table is entirely in it being complete.
func builtInAssertionEntryPoints() map[string]builtInAssertionEntryPoint {
	return map[string]builtInAssertionEntryPoint{
		"specs.EqualTo": {
			fail: func(ctx *Context) { EqualTo(ctx, 1, 2) },
			pass: func(ctx *Context) { EqualTo(ctx, 1, 1) },
		},
		"specs.ExpectT(...).ToEqual": {
			fail: func(ctx *Context) { ExpectT(ctx, 1).ToEqual(2) },
			pass: func(ctx *Context) { ExpectT(ctx, 1).ToEqual(1) },
		},
		"specs.ExpectT(...).To": {
			fail: func(ctx *Context) { ExpectT(ctx, true).To(BeFalse()) },
			pass: func(ctx *Context) { ExpectT(ctx, true).To(BeTrue()) },
		},
		"ctx.Expect(...).ToEqual": {
			fail: func(ctx *Context) { ctx.Expect([]int{1}).ToEqual([]int{2}) },
			pass: func(ctx *Context) { ctx.Expect([]int{1}).ToEqual([]int{1}) },
		},
		"ctx.Expect(...).To": {
			fail: func(ctx *Context) { ctx.Expect(42).To(Equal(43)) },
			pass: func(ctx *Context) { ctx.Expect(42).To(Equal(42)) },
		},
		"ctx.Snapshot": {
			// A snapshot key that was never stored is a mismatch, which is exactly the shape #115
			// reported green.
			fail: func(ctx *Context) { ctx.Snapshot("contract-missing-on-purpose", 42) },
			pass: func(ctx *Context) { ctx.Snapshot("contract-baseline", snapshotBaselineValue) },
		},
	}
}

func TestEveryBuiltInAssertionEntryPointReportsItsFailureToTheReporterAndTheSuiteCount(t *testing.T) {
	for name, entryPoint := range builtInAssertionEntryPoints() {
		t.Run(name, func(t *testing.T) {
			rep, counter := runSpecThroughPlan(t, entryPoint.fail)

			if len(rep.specFinished) != 1 {
				t.Fatalf("expected exactly one SpecFinished event, got %d", len(rep.specFinished))
			}
			if !rep.specFinished[0].Failed {
				t.Error("expected SpecResultEvent.Failed=true; a reporter-driven consumer would be told this spec passed")
			}
			if counter.failed != 1 {
				t.Errorf("expected SuiteEndEvent.FailedSpecs=1, got %d", counter.failed)
			}
			if counter.total != 1 {
				t.Errorf("expected TotalSpecs=1, got %d", counter.total)
			}
		})
	}
}

func TestEveryBuiltInAssertionEntryPointLeavesAPassingSpecGreen(t *testing.T) {
	for name, entryPoint := range builtInAssertionEntryPoints() {
		t.Run(name, func(t *testing.T) {
			rep, counter := runSpecThroughPlan(t, entryPoint.pass)

			if len(rep.specFinished) != 1 {
				t.Fatalf("expected exactly one SpecFinished event, got %d", len(rep.specFinished))
			}
			if rep.specFinished[0].Failed {
				t.Errorf("expected a passing assertion to report Failed=false, got %+v", rep.specFinished[0])
			}
			if counter.failed != 0 {
				t.Errorf("expected SuiteEndEvent.FailedSpecs=0, got %d", counter.failed)
			}
		})
	}
}

// TestEveryBuiltInAssertionEntryPointStopsAFailFastRun holds the third consumer of the same record
// to the same table. FailFast reading a different bit than the reporter is precisely the divergence
// #175 is about, so it is asserted here rather than left to the two reporter tests above.
func TestEveryBuiltInAssertionEntryPointStopsAFailFastRun(t *testing.T) {
	for name, entryPoint := range builtInAssertionEntryPoints() {
		t.Run(name, func(t *testing.T) {
			ctx := &Context{}
			ctx.Reset(&planBackend{})

			entryPoint.fail(ctx)

			if !ctx.hasFailed() {
				t.Error("expected the failure to be recorded, so FailFast stops the run after this spec")
			}
		})
	}
}

// silentMatcher fails with no message at all. It is not a contrived shape: a user-written Matcher
// is free to return "" from FailureMessage, and parallelBackend records an empty message for
// Fatal() with no arguments too.
type silentMatcher struct{}

func (silentMatcher) Match(any) bool            { return false }
func (silentMatcher) FailureMessage(any) string { return "" }

// TestAnEmptyFailureMessageIsStillAFailureOnTheSequentialPath pins the invariant the sequential
// path always had and must keep: the failure bit is recorded by the assertion, never derived from
// the text it produced.
func TestAnEmptyFailureMessageIsStillAFailureOnTheSequentialPath(t *testing.T) {
	rep, counter := runSpecThroughPlan(t, func(ctx *Context) {
		ctx.Expect(1).To(silentMatcher{})
	})

	if len(rep.specFinished) != 1 || !rep.specFinished[0].Failed {
		t.Fatalf("expected the empty-message failure to be reported as failed, got %+v", rep.specFinished)
	}
	if counter.failed != 1 {
		t.Errorf("expected SuiteEndEvent.FailedSpecs=1, got %d", counter.failed)
	}
}

// TestAnEmptyFailureMessageIsStillAFailureOnTheParallelPath is the one #175 actually fixes. The
// parallel path used to carry no failure bit at all: every consumer asked whether the recorded
// message was non-empty, so a failure with an empty message was reported as a passing spec, was
// skipped by reportFailures — and therefore never reached `go test` either — and left the group's
// parent Context unfailed, so FailFast ran straight past it. A silent green on a red run, in all
// three consumers at once.
func TestAnEmptyFailureMessageIsStillAFailureOnTheParallelPath(t *testing.T) {
	rep := &recordingReporter{}
	obs := &reporterObserver{rep: rep}
	backend := &capturingBackend{}
	ctx := &Context{backend: backend, execObserver: obs}

	run := parallelStep([]step{
		runAll([]step{func(ctx *Context) { ctx.Expect(1).To(silentMatcher{}) }}),
	}, []string{"silent"}, nil)
	run(ctx)

	if len(rep.specFinished) != 1 {
		t.Fatalf("expected one SpecFinished event, got %+v", rep.specFinished)
	}
	if got := rep.specFinished[0]; !got.Failed {
		t.Errorf("expected SpecResultEvent.Failed=true for an empty-message failure, got %+v", got)
	}
	if !ctx.hasFailed() {
		t.Error("expected the group's parent Context to be failed, so FailFast stops at the group boundary")
	}
	if !backend.failed {
		t.Error("expected reportFailures to surface the empty-message failure on the outer backend; without it a plain `go test` run stays green")
	}
}

// TestReportFailuresSurfacesAFailureWithNoMessage is the same invariant at the level reportFailures
// itself works at, so a future change there cannot reintroduce the message-as-failure-bit inference
// without this failing.
func TestReportFailuresSurfacesAFailureWithNoMessage(t *testing.T) {
	tb := &capturingBackend{}

	reportFailures(tb, []failureRecord{{}, {Failed: true}})

	if !tb.failed {
		t.Fatal("expected a failed record with an empty message to be reported")
	}
	if tb.message != "spec[1]: " {
		t.Errorf("expected the second record to be the one reported, got %q", tb.message)
	}
}

// TestSpecResultEventFailedComesFromTheSameRecordAsTheSuiteCount pins that the two consumers are
// not merely both right today but read the same source: one failing spec among passing ones must
// show up as exactly one failed event and exactly one counted failure.
func TestSpecResultEventFailedComesFromTheSameRecordAsTheSuiteCount(t *testing.T) {
	c := newBytecodeCompiler()
	c.PushScope("MixedSuite")
	s := &Spec{name: "MixedSuite", compiler: c}
	s.It("passes", func(ctx *Context) { EqualTo(ctx, 1, 1) })
	s.It("fails", func(ctx *Context) { EqualTo(ctx, 1, 2) })
	s.It("passes too", func(ctx *Context) { EqualTo(ctx, 2, 2) })
	plan := c.TakePlan()

	rep := &recordingReporter{}
	counter := &specCounter{EventReporter: rep}
	runPlanSpecsInOrder(&planBackend{}, counter, plan)

	var failedEvents []report.SpecResultEvent
	for _, e := range rep.specFinished {
		if e.Failed {
			failedEvents = append(failedEvents, e)
		}
	}
	if len(failedEvents) != 1 {
		t.Fatalf("expected exactly one failed SpecFinished event, got %+v", failedEvents)
	}
	if counter.failed != len(failedEvents) {
		t.Errorf("SuiteEndEvent.FailedSpecs=%d disagrees with the %d failed events reported", counter.failed, len(failedEvents))
	}
	if counter.total != 3 {
		t.Errorf("expected TotalSpecs=3, got %d", counter.total)
	}
}
