package specs

import (
	"context"
	"strings"
	"testing"
)

// TestExecutionPlanRecoversPanicAndContinues proves issue #15's core requirement for the default
// (non-path) execution path: a spec body that panics no longer crashes the process — it's recorded
// as that spec's failure, and the remaining specs in the plan still run.
func TestExecutionPlanRecoversPanicAndContinues(t *testing.T) {
	var ranSpec2 bool
	plan := &ExecutionPlan{
		Instructions: []Instruction{
			{Code: OpBody, Fn: func(*Context) { panic("boom") }},
			{Code: OpBody, Fn: func(*Context) { ranSpec2 = true }},
		},
		ProgramStart: []int{0, 1},
		ProgramLen:   []int{1, 1},
		PathGens:     []*PathGenerator{nil, nil},
	}
	backend := &controlledBackend{}
	runPlanSpecsInOrder(context.Background(), backend, nil, plan)

	if !ranSpec2 {
		t.Fatal("expected spec2 to run after spec1 panicked, but it didn't — the panic aborted the whole plan")
	}
	if len(backend.errors) != 1 {
		t.Fatalf("expected exactly one recorded failure for the panicking spec, got %v", backend.errors)
	}
	if !strings.Contains(backend.errors[0], "boom") {
		t.Fatalf("expected the panic message in the recorded failure, got %q", backend.errors[0])
	}
}

// TestRunProgramAfterHookSurvivesBodyPanic proves after-hooks still run when the body panics,
// matching runIsolatedCaseDirect's established pattern for the path-generated case.
func TestRunProgramAfterHookSurvivesBodyPanic(t *testing.T) {
	backend := &controlledBackend{}
	ctx := &Context{backend: backend}
	var afterRan bool
	program := []Instruction{
		{Code: OpBody, Fn: func(*Context) { panic("boom") }},
		{Code: OpAfterHook, Fn: func(*Context) { afterRan = true }},
	}
	runProgram(program, ctx, nil)

	if !afterRan {
		t.Fatal("expected the after hook to run despite the body panic")
	}
	if !ctx.failed {
		t.Fatal("expected ctx.failed to be set after an unrecovered body panic")
	}
	if len(backend.errors) != 1 || !strings.Contains(backend.errors[0], "boom") {
		t.Fatalf("expected one recorded failure mentioning the panic message, got %v", backend.errors)
	}
}

// TestRunProgramDoesNotDoubleReportExpectedAbort proves that isolatedCaseAbort — the sentinel a
// controlled testBackend panics from FailNow/Fatal/Fatalf to stop one case — is recognized as an
// already-recorded stop, not reported a second time as an unexpected panic. It also confirms the
// after hook still runs, matching an ordinary FailNow.
func TestRunProgramDoesNotDoubleReportExpectedAbort(t *testing.T) {
	backend := &controlledBackend{}
	ctx := &Context{backend: backend}
	var afterRan bool
	program := []Instruction{
		{Code: OpBody, Fn: func(c *Context) { c.backend.FailNow() }},
		{Code: OpAfterHook, Fn: func(*Context) { afterRan = true }},
	}
	runProgram(program, ctx, nil)

	if !backend.failNow {
		t.Fatal("expected the backend to have recorded FailNow")
	}
	if !afterRan {
		t.Fatal("expected the after hook to still run after an expected abort")
	}
	if len(backend.errors) != 0 {
		t.Fatalf("expected no unexpected-panic error for an expected abort sentinel, got %v", backend.errors)
	}
}

// TestRunProgramAfterHookPanicDoesNotStopSiblingAfterHooks proves one panicking after-hook doesn't
// prevent the remaining after-hooks (for the same spec) from running.
func TestRunProgramAfterHookPanicDoesNotStopSiblingAfterHooks(t *testing.T) {
	backend := &controlledBackend{}
	ctx := &Context{backend: backend}
	var secondAfterRan bool
	program := []Instruction{
		{Code: OpBody, Fn: func(*Context) {}},
		{Code: OpAfterHook, Fn: func(*Context) { panic("after boom") }},
		{Code: OpAfterHook, Fn: func(*Context) { secondAfterRan = true }},
	}
	runProgram(program, ctx, nil)

	if !secondAfterRan {
		t.Fatal("expected the second after hook to run despite the first one panicking")
	}
	found := false
	for _, e := range backend.errors {
		if strings.Contains(e, "after boom") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the after-hook panic to be recorded as a failure, got %v", backend.errors)
	}
}
