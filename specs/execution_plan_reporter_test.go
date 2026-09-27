package specs

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/report"
)

// recordingReporter is a report.EventReporter test double that records every event it receives,
// so tests can assert on structured event data instead of parsing formatted strings.
type recordingReporter struct {
	mu            sync.Mutex
	suiteStarted  []report.SuiteStartEvent
	suiteFinished []report.SuiteEndEvent
	specStarted   []report.SpecStartEvent
	specFinished  []report.SpecResultEvent
}

func (r *recordingReporter) SuiteStarted(e report.SuiteStartEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.suiteStarted = append(r.suiteStarted, e)
}

func (r *recordingReporter) SuiteFinished(e report.SuiteEndEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.suiteFinished = append(r.suiteFinished, e)
}

func (r *recordingReporter) SpecStarted(e report.SpecStartEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.specStarted = append(r.specStarted, e)
}

func (r *recordingReporter) SpecFinished(e report.SpecResultEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.specFinished = append(r.specFinished, e)
}

var _ report.EventReporter = (*recordingReporter)(nil)

// TestDescribeWithReporterEmitsSuiteAndSpecEventsForFlatSpec proves the reporter stored on
// Spec/CompiledSuite actually reaches execution now: a single flat (non-Paths) spec emits exactly
// one SpecStarted/SpecFinished pair, wrapped by one SuiteStarted/SuiteFinished pair reporting the
// real spec count.
func TestDescribeWithReporterEmitsSuiteAndSpecEventsForFlatSpec(t *testing.T) {
	rep := &recordingReporter{}
	DescribeWithReporter(t, "FlatSuite", rep, func(s *Spec) {
		s.It("passes", func(ctx *Context) {})
	})

	if len(rep.suiteStarted) != 1 || rep.suiteStarted[0].Name != "FlatSuite" {
		t.Fatalf("expected one SuiteStarted for FlatSuite, got %+v", rep.suiteStarted)
	}
	if len(rep.specStarted) != 1 || rep.specStarted[0].Name != "passes" {
		t.Fatalf("expected one SpecStarted for 'passes', got %+v", rep.specStarted)
	}
	if len(rep.specFinished) != 1 || rep.specFinished[0].Name != "passes" || rep.specFinished[0].Failed {
		t.Fatalf("expected one passing SpecFinished for 'passes', got %+v", rep.specFinished)
	}
	if len(rep.suiteFinished) != 1 {
		t.Fatalf("expected one SuiteFinished, got %+v", rep.suiteFinished)
	}
	end := rep.suiteFinished[0]
	if end.Name != "FlatSuite" || end.TotalSpecs != 1 || end.FailedSpecs != 0 {
		t.Fatalf("expected FlatSuite TotalSpecs=1 FailedSpecs=0, got %+v", end)
	}
}

// TestDescribeWithReporterMarksFailedFlatSpec proves a failing flat spec is reported as failed both
// on its own SpecFinished event and in the enclosing SuiteEndEvent's FailedSpecs count.
//
// The spec body calls ctx.recordFailure() directly (what the framework's own Expect(...) matchers
// do internally on a failed assertion) instead of ctx.T.Errorf/Fatalf: recordFailure only flips
// Context's internal failed flag, so it exercises the same Failed-tracking path a real assertion
// failure would without also failing this test's own *testing.T — which a real t.Errorf on the
// generated subtest would do, since subtest failures bubble up to the parent test.
func TestDescribeWithReporterMarksFailedFlatSpec(t *testing.T) {
	rep := &recordingReporter{}
	DescribeWithReporter(t, "FailingFlatSuite", rep, func(s *Spec) {
		s.It("fails", func(ctx *Context) { ctx.recordFailure() })
	})

	if len(rep.specFinished) != 1 || !rep.specFinished[0].Failed {
		t.Fatalf("expected one failing SpecFinished, got %+v", rep.specFinished)
	}
	if len(rep.suiteFinished) != 1 || rep.suiteFinished[0].FailedSpecs != 1 {
		t.Fatalf("expected FailedSpecs=1, got %+v", rep.suiteFinished)
	}
}

// TestDescribeWithReporterRecordsSpecAndSuiteDuration proves SpecFinished.Duration and
// SuiteFinished.Duration measure real elapsed time, not a placeholder zero value: a spec that
// deliberately sleeps reports a Duration at least as long as the sleep, and the enclosing suite's
// Duration is at least the slept spec's. It also pins down the invariant Duration depends on:
// SpecFinished.SpecStartEvent must be the exact event SpecStarted sent, not one rebuilt with
// time.Now() at finish time — asserted here via the Time fields matching exactly.
func TestDescribeWithReporterRecordsSpecAndSuiteDuration(t *testing.T) {
	const sleep = 20 * time.Millisecond
	rep := &recordingReporter{}
	DescribeWithReporter(t, "DurationSuite", rep, func(s *Spec) {
		s.It("slow", func(ctx *Context) { time.Sleep(sleep) })
	})

	if len(rep.specStarted) != 1 || len(rep.specFinished) != 1 {
		t.Fatalf("expected one SpecStarted/SpecFinished pair, got started=%+v finished=%+v", rep.specStarted, rep.specFinished)
	}
	started, finished := rep.specStarted[0], rep.specFinished[0]
	if !finished.Time.Equal(started.Time) {
		t.Fatalf("expected SpecFinished.SpecStartEvent to be the exact SpecStarted event, got start=%v finish=%v", started.Time, finished.Time)
	}
	if finished.Duration < sleep {
		t.Fatalf("expected spec Duration >= %v (the spec slept that long), got %v", sleep, finished.Duration)
	}
	if len(rep.suiteFinished) != 1 {
		t.Fatalf("expected one SuiteFinished, got %+v", rep.suiteFinished)
	}
	if end := rep.suiteFinished[0]; end.Duration < sleep {
		t.Fatalf("expected suite Duration >= %v (it wraps the slow spec), got %v", sleep, end.Duration)
	}
}

// TestDescribeWithoutReporterEmitsNoEvents proves the default (no reporter) path stays exactly as
// it was: passing a nil reporter causes no events, no panics, and no behavior change.
func TestDescribeWithoutReporterEmitsNoEvents(t *testing.T) {
	var ran bool
	DescribeWithReporter(t, "NoReporterSuite", nil, func(s *Spec) {
		s.It("runs", func(ctx *Context) { ran = true })
	})
	if !ran {
		t.Fatal("expected the spec to still run with a nil reporter")
	}
}

// TestRunProgramReportsPanicMessageAndOutput proves a recovered spec-body panic — the one sequential
// failure path that still reaches reportSpecFinished today, see specResult's doc comment — reports
// Message as the short "panic: value" summary and Output as the panic's stack trace, built once in
// runProgram and reused for both the backend's Errorf call and the reported event.
//
// This drives runProgram/reportSpecFinished directly (not through DescribeWithReporter) with a
// controlledBackend, matching TestExecutionPlanRecoversPanicAndContinues's established pattern in
// execution_plan_recovery_test.go: a real recovered panic calls ctx.backend.Errorf, which — on the
// real *testing.T a DescribeWithReporter(t, ...) call would use — marks this very test failed too.
func TestRunProgramReportsPanicMessageAndOutput(t *testing.T) {
	rep := &recordingReporter{}
	backend := &controlledBackend{}
	ctx := &Context{backend: backend}
	program := []Instruction{
		{Code: OpBody, Fn: func(*Context) { panic("boom") }},
	}

	started := reportSpecStarted(rep, "panics", nil)
	message, output := runProgram(program, ctx)
	reportSpecFinished(rep, started, specResult{Failed: ctx.hasFailed(), Message: message, Output: output})

	if len(rep.specFinished) != 1 {
		t.Fatalf("expected one SpecFinished, got %+v", rep.specFinished)
	}
	got := rep.specFinished[0]
	if !got.Failed {
		t.Fatalf("expected the panicking spec to be reported as failed, got %+v", got)
	}
	if got.Message != "panic: boom" {
		t.Errorf("expected Message %q, got %q", "panic: boom", got.Message)
	}
	if !strings.Contains(got.Output, "runProgram") {
		t.Errorf("expected Output to contain a stack trace naming runProgram, got %q", got.Output)
	}
	if len(backend.errors) != 1 || backend.errors[0] != message+"\n"+output {
		t.Fatalf("expected backend.Errorf to receive exactly message+\"\\n\"+output, got %v", backend.errors)
	}
}

// TestRunProgramAfterHookOnlyPanicStillReportsMessage proves that when the spec body itself doesn't
// panic but its own after-hook does — unlike runner.go's group-shared after hooks, an ExecutionPlan
// program's after instructions are scoped to this one spec — that after-hook's message/output still
// feed the spec's result, so Failed=true is never paired with an empty Message.
func TestRunProgramAfterHookOnlyPanicStillReportsMessage(t *testing.T) {
	backend := &controlledBackend{}
	ctx := &Context{backend: backend}
	program := []Instruction{
		{Code: OpBody, Fn: func(*Context) {}},
		{Code: OpAfterHook, Fn: func(*Context) { panic("after boom") }},
	}

	message, output := runProgram(program, ctx)

	if !ctx.hasFailed() {
		t.Fatal("expected ctx.hasFailed() to be set after an after-hook panic")
	}
	if message != "panic in after hook: after boom" {
		t.Errorf("expected message %q, got %q", "panic in after hook: after boom", message)
	}
	if output == "" {
		t.Error("expected a non-empty stack trace in output")
	}
}
