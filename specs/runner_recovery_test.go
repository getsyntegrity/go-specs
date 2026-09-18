package specs

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestRunGroupSpecPanicRecoveredSiblingsStillRun proves a panicking spec doesn't stop its siblings
// in the same group from running — matching the default execution path's contract.
func TestRunGroupSpecPanicRecoveredSiblingsStillRun(t *testing.T) {
	backend := &controlledBackend{}
	ctx := &Context{backend: backend}
	var ranSpec2 bool
	g := &group{
		specs: []step{
			func(*Context) { panic("boom") },
			func(*Context) { ranSpec2 = true },
		},
	}
	runGroup(ctx, g)

	if !ranSpec2 {
		t.Fatal("expected spec2 to run after spec1 panicked, but it didn't")
	}
	if len(backend.errors) != 1 || !strings.Contains(backend.errors[0], "boom") {
		t.Fatalf("expected exactly one recorded failure mentioning the panic message, got %v", backend.errors)
	}
}

// TestRunGroupAfterAlwaysRunsDespiteSpecPanic proves the group's after hooks still run when a spec
// panics, matching #61's runProgram contract for the default execution path.
func TestRunGroupAfterAlwaysRunsDespiteSpecPanic(t *testing.T) {
	backend := &controlledBackend{}
	ctx := &Context{backend: backend}
	var afterRan bool
	g := &group{
		specs: []step{func(*Context) { panic("boom") }},
		after: []step{func(*Context) { afterRan = true }},
	}
	runGroup(ctx, g)

	if !afterRan {
		t.Fatal("expected the group's after hook to run despite the spec panic")
	}
	if !ctx.hasFailed() {
		t.Fatal("expected ctx.hasFailed() to be set after an unrecovered spec panic")
	}
}

// TestRunGroupBeforePanicSkipsSpecsButRunsAfter proves the contract decided for #62: a panic in a
// before hook stops the rest of setup and skips this group's specs entirely (they can't be trusted
// to run against setup that never completed), but the group's after still runs.
func TestRunGroupBeforePanicSkipsSpecsButRunsAfter(t *testing.T) {
	backend := &controlledBackend{}
	ctx := &Context{backend: backend}
	var specRan, afterRan bool
	g := &group{
		before: []step{func(*Context) { panic("setup boom") }},
		specs:  []step{func(*Context) { specRan = true }},
		after:  []step{func(*Context) { afterRan = true }},
	}
	runGroup(ctx, g)

	if specRan {
		t.Fatal("expected specs to be skipped after a before-hook panic, but a spec ran")
	}
	if !afterRan {
		t.Fatal("expected the group's after hook to still run after a before-hook panic")
	}
	if len(backend.errors) != 1 || !strings.Contains(backend.errors[0], "setup boom") {
		t.Fatalf("expected exactly one recorded failure mentioning the panic message, got %v", backend.errors)
	}
}

// TestRunGroupAfterHookPanicDoesNotStopSiblingAfterHooks proves one panicking after hook doesn't
// prevent the remaining after hooks (for the same group) from running.
func TestRunGroupAfterHookPanicDoesNotStopSiblingAfterHooks(t *testing.T) {
	backend := &controlledBackend{}
	ctx := &Context{backend: backend}
	var secondRan bool
	// after runs in reverse order (index len-1 down to 0), so the second element here runs first.
	g := &group{
		specs: []step{func(*Context) {}},
		after: []step{
			func(*Context) { secondRan = true },
			func(*Context) { panic("after boom") },
		},
	}
	runGroup(ctx, g)

	if !secondRan {
		t.Fatal("expected the sibling after hook to run despite the first-executed one panicking")
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

// TestRunGroupAfterHookPanicDoesNotStopSiblingAfterHooksUnderFailFast proves that FailFast does not
// leak into after-hook execution: FailFast decides whether we run more specs/groups, not whether we
// leave resources uncleaned. Even with FailFast on and the first-executed after hook panicking (which
// sets ctx.hasFailed()), the remaining after hooks in this group must still run.
func TestRunGroupAfterHookPanicDoesNotStopSiblingAfterHooksUnderFailFast(t *testing.T) {
	backend := &controlledBackend{}
	ctx := &Context{backend: backend}
	ctx.SetFailFast(true)
	var secondRan bool
	// after runs in reverse order (index len-1 down to 0), so the second element here runs first.
	g := &group{
		specs: []step{func(*Context) {}},
		after: []step{
			func(*Context) { secondRan = true },
			func(*Context) { panic("after boom") },
		},
	}
	runGroup(ctx, g)

	if !secondRan {
		t.Fatal("expected the sibling after hook to run despite FailFast and the first-executed one panicking")
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

// TestRunGroupFailFastStopsRemainingSpecsButAfterStillRuns proves the contract decided for #62: a
// panic counts as ctx.hasFailed() (via ctx.recordFailure), so FailFast's existing stop-at-the-next-check
// logic naturally stops the remaining specs in the group — but after still runs regardless, via defer.
func TestRunGroupFailFastStopsRemainingSpecsButAfterStillRuns(t *testing.T) {
	backend := &controlledBackend{}
	ctx := &Context{backend: backend}
	ctx.SetFailFast(true)
	var ranSpec2, afterRan bool
	g := &group{
		specs: []step{
			func(*Context) { panic("boom") },
			func(*Context) { ranSpec2 = true },
		},
		after: []step{func(*Context) { afterRan = true }},
	}
	runGroup(ctx, g)

	if ranSpec2 {
		t.Fatal("expected FailFast to stop the remaining specs in the group after a panic")
	}
	if !afterRan {
		t.Fatal("expected the group's after hook to still run under FailFast")
	}
}

// TestRunGroupsContinuesToNextGroupAfterPanic proves issue #15's core requirement at the group-
// iteration level: a spec that panics in one group is recorded as that group's failure, and the
// next group still runs — the process doesn't crash and other groups' results aren't discarded.
func TestRunGroupsContinuesToNextGroupAfterPanic(t *testing.T) {
	backend := &controlledBackend{}
	ctx := &Context{backend: backend}
	var ranGroup1 bool
	groups := []group{
		{specs: []step{func(*Context) { panic("boom") }}},
		{specs: []step{func(*Context) { ranGroup1 = true }}},
	}
	runGroups(ctx, groups)

	if !ranGroup1 {
		t.Fatal("expected the second group to still run after the first group's spec panicked")
	}
	if len(backend.errors) != 1 || !strings.Contains(backend.errors[0], "boom") {
		t.Fatalf("expected exactly one recorded failure mentioning the panic message, got %v", backend.errors)
	}
}

// TestRunGroupsFailFastStopsSubsequentGroupsAfterPanic proves a panic interacts with FailFast the
// same way an ordinary assertion failure does: it stops progression to the next group.
func TestRunGroupsFailFastStopsSubsequentGroupsAfterPanic(t *testing.T) {
	backend := &controlledBackend{}
	ctx := &Context{backend: backend}
	ctx.SetFailFast(true)
	var ranGroup1 bool
	groups := []group{
		{specs: []step{func(*Context) { panic("boom") }}},
		{specs: []step{func(*Context) { ranGroup1 = true }}},
	}
	runGroups(ctx, groups)

	if ranGroup1 {
		t.Fatal("expected FailFast to stop the second group from running after the first group's panic")
	}
}

// TestRunnerRunRecoversPanicAcrossGroupsRealProcess is a thin end-to-end check that Runner.Run —
// the public entry point, with the real contextPool/testBackend wiring, not the internal helpers
// exercised above — turns a panicking spec into an ordinary test failure instead of crashing the
// process. Runs in a subprocess (same pattern as TestGeneratedFatalUsesRealSubtestBoundary in
// execution_plan_test.go) rather than a nested t.Run, since a nested subtest's real Errorf would
// itself mark this test failed; a subprocess crash, by contrast, would simply never print "group1
// ran" at all — the clearest possible proof the process kept going.
func TestRunnerRunRecoversPanicAcrossGroupsRealProcess(t *testing.T) {
	if os.Getenv("GO_SPECS_RUNNER_PANIC_HELPER") == "1" {
		prog := &Program{
			Groups: []group{
				{specs: []step{func(*Context) { panic("boom") }}},
				{specs: []step{func(*Context) { fmt.Println("group1 ran") }}},
			},
		}
		NewRunner(prog).Run(t)
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestRunnerRunRecoversPanicAcrossGroupsRealProcess$")
	cmd.Env = append(os.Environ(), "GO_SPECS_RUNNER_PANIC_HELPER=1")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected the panicking spec to fail the test, but it passed: %s", output)
	}
	if !strings.Contains(string(output), "group1 ran") {
		t.Fatalf("expected the second group to still run (process must not crash), got: %s", output)
	}
}

// TestRunnerRunRealFatalfIsolatesJustThatSpecRealProcess proves #74's fix end-to-end: a real
// assertion failure (EqualTo, which calls Fatalf under the hood) inside one spec, against a genuine
// *testing.T (not a fake), no longer terminates the whole Runner.Run call. Spec bodies now each run
// in their own subtest (see runSpecRecovered), so Goexit only unwinds that subtest's goroutine — the
// remaining specs in the group still run, and every spec, including the failing one, still gets a
// SpecFinished event. Same subprocess pattern as TestRunnerRunRecoversPanicAcrossGroupsRealProcess: a
// nested t.Run's real Fatalf would otherwise mark this outer test failed itself, which would make
// "did isolation work" indistinguishable from "did this test fail for an unrelated reason."
func TestRunnerRunRealFatalfIsolatesJustThatSpecRealProcess(t *testing.T) {
	if os.Getenv("GO_SPECS_RUNNER_FATALF_ISOLATION_HELPER") == "1" {
		var rep recordingReporter
		prog := &Program{
			Groups: []group{
				{
					specs: []step{
						func(ctx *Context) { EqualTo(ctx, 1, 2) },
						func(*Context) { fmt.Println("spec2 ran") },
						func(*Context) { fmt.Println("spec3 ran") },
					},
					names: []string{"spec one", "spec two", "spec three"},
				},
			},
		}
		NewRunnerWithReporter(prog, "suite", &rep).Run(t)
		fmt.Printf("suite finished total=%d failed=%d\n", rep.suiteFinished[0].TotalSpecs, rep.suiteFinished[0].FailedSpecs)
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestRunnerRunRealFatalfIsolatesJustThatSpecRealProcess$")
	cmd.Env = append(os.Environ(), "GO_SPECS_RUNNER_FATALF_ISOLATION_HELPER=1")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected the failing spec to fail the process, but it passed: %s", output)
	}
	if !strings.Contains(string(output), "spec2 ran") || !strings.Contains(string(output), "spec3 ran") {
		t.Fatalf("expected specs 2 and 3 to still run after spec 1's real Fatal, got: %s", output)
	}
	if !strings.Contains(string(output), "suite finished total=3 failed=1") {
		t.Fatalf("expected SuiteFinished to report total=3 failed=1 (every spec still reported), got: %s", output)
	}
}

// TestRunnerRunFailFastStopsAfterRealFatalfRealProcess proves isolation and FailFast compose
// correctly: EqualTo's recordFailure() sets ctx.hasFailed() synchronously before the Fatalf call that
// triggers Goexit (not learned via recover), and t.Run blocks until the subtest's goroutine
// finishes — so runSpecsRecovered's failFast check, running right after runSpecRecovered returns,
// sees the correct value and stops before spec2, exactly as it would have without isolation.
func TestRunnerRunFailFastStopsAfterRealFatalfRealProcess(t *testing.T) {
	if os.Getenv("GO_SPECS_RUNNER_FATALF_FAILFAST_HELPER") == "1" {
		var rep recordingReporter
		prog := &Program{
			Groups: []group{
				{
					specs: []step{
						func(ctx *Context) { EqualTo(ctx, 1, 2) },
						func(*Context) { fmt.Println("spec2 ran") },
					},
					names: []string{"spec one", "spec two"},
				},
			},
		}
		r := NewRunnerWithReporter(prog, "suite", &rep)
		r.FailFast = true
		r.Run(t)
		fmt.Printf("suite finished total=%d failed=%d\n", rep.suiteFinished[0].TotalSpecs, rep.suiteFinished[0].FailedSpecs)
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestRunnerRunFailFastStopsAfterRealFatalfRealProcess$")
	cmd.Env = append(os.Environ(), "GO_SPECS_RUNNER_FATALF_FAILFAST_HELPER=1")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected the failing spec to fail the process, but it passed: %s", output)
	}
	if strings.Contains(string(output), "spec2 ran") {
		t.Fatalf("expected FailFast to stop spec2 from running after spec1's real Fatal, got: %s", output)
	}
	if !strings.Contains(string(output), "suite finished total=1 failed=1") {
		t.Fatalf("expected SuiteFinished to report total=1 failed=1 (only the failed spec reported before FailFast stopped), got: %s", output)
	}
}

// TestRunnerRunAfterEachRunsDespiteRealFatalfInBeforeRealProcess proves runSpecWithHooks' defer-based
// teardown guarantee: a real testing.T.Fatal (via EqualTo) inside a before hook calls runtime.Goexit,
// which recover() cannot observe (see runStepRecovered) — a plain "run before/body, then run after"
// sequence would never reach the after-hook call at all, since Goexit unwinds straight past it.
// Registering after's execution as a defer, before before/body ever run, is what makes it survive
// Goexit: deferred calls still run while a goroutine unwinds. Same subprocess pattern as
// TestRunnerRunRealFatalfIsolatesJustThatSpecRealProcess (a nested real Fatalf must not fail this
// outer test itself).
func TestRunnerRunAfterEachRunsDespiteRealFatalfInBeforeRealProcess(t *testing.T) {
	if os.Getenv("GO_SPECS_RUNNER_AFTEREACH_AFTER_BEFORE_FATAL_HELPER") == "1" {
		prog := &Program{
			Groups: []group{
				{
					before: []step{func(ctx *Context) { EqualTo(ctx, 1, 2) }},
					after:  []step{func(*Context) { fmt.Println("after ran") }},
					specs:  []step{func(*Context) { fmt.Println("spec ran") }},
				},
			},
		}
		NewRunner(prog).Run(t)
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestRunnerRunAfterEachRunsDespiteRealFatalfInBeforeRealProcess$")
	cmd.Env = append(os.Environ(), "GO_SPECS_RUNNER_AFTEREACH_AFTER_BEFORE_FATAL_HELPER=1")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected the before hook's real Fatal to fail the process, but it passed: %s", output)
	}
	if strings.Contains(string(output), "spec ran") {
		t.Fatalf("expected the spec body to be skipped after before's real Fatal, got: %s", output)
	}
	if !strings.Contains(string(output), "after ran") {
		t.Fatalf("expected the after hook to still run despite before's real Fatal (Goexit), got: %s", output)
	}
}

// TestRunnerRunAfterEachRunsDespiteRealFatalfInBodyRealProcess is
// TestRunnerRunAfterEachRunsDespiteRealFatalfInBeforeRealProcess for a real Fatal in the spec body
// itself, rather than in before: the same defer-based guarantee must hold regardless of which of the
// two Goexit sources fired.
func TestRunnerRunAfterEachRunsDespiteRealFatalfInBodyRealProcess(t *testing.T) {
	if os.Getenv("GO_SPECS_RUNNER_AFTEREACH_AFTER_BODY_FATAL_HELPER") == "1" {
		prog := &Program{
			Groups: []group{
				{
					after: []step{func(*Context) { fmt.Println("after ran") }},
					specs: []step{func(ctx *Context) { EqualTo(ctx, 1, 2) }},
				},
			},
		}
		NewRunner(prog).Run(t)
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestRunnerRunAfterEachRunsDespiteRealFatalfInBodyRealProcess$")
	cmd.Env = append(os.Environ(), "GO_SPECS_RUNNER_AFTEREACH_AFTER_BODY_FATAL_HELPER=1")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected the spec body's real Fatal to fail the process, but it passed: %s", output)
	}
	if !strings.Contains(string(output), "after ran") {
		t.Fatalf("expected the after hook to still run despite the body's real Fatal (Goexit), got: %s", output)
	}
}

// TestRunnerRunSpecNamesWithSpacesAndSlashesReportedVerbatim proves a real *testing.T subtest per
// spec (used for isolation, see runSpecRecovered) never leaks into reported spec identity: Go's
// t.Run sanitizes spaces to "_" and treats "/" as a nested-subtest boundary for -v/-run/test2json
// presentation, but SpecStartEvent/SpecResultEvent.Name always comes straight from group.names, so
// none of that shows up in the reported event. No subprocess needed — neither spec fails.
func TestRunnerRunSpecNamesWithSpacesAndSlashesReportedVerbatim(t *testing.T) {
	var rep recordingReporter
	prog := &Program{
		Groups: []group{
			{
				specs: []step{func(*Context) {}, func(*Context) {}},
				names: []string{"spec with spaces", "spec/with/slashes"},
			},
		},
	}
	NewRunnerWithReporter(prog, "suite", &rep).Run(t)

	if len(rep.specStarted) != 2 {
		t.Fatalf("expected 2 SpecStarted events, got %d", len(rep.specStarted))
	}
	if got := rep.specStarted[0].Name; got != "spec with spaces" {
		t.Fatalf("expected reported name %q, got %q", "spec with spaces", got)
	}
	if got := rep.specStarted[1].Name; got != "spec/with/slashes" {
		t.Fatalf("expected reported name %q, got %q", "spec/with/slashes", got)
	}
}
