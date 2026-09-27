package specs

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// runner_single_recover_test.go pins runSpecWithHooks' exact panic-isolation contract before #235's
// single-recover-per-spec optimization, so the refactor (one shared defer/recover per spec that
// tracks the current phase, falling back to per-hook recovery only after the first panic — see
// runner.go's doc comments) cannot silently change behavior. Every test here talks to
// runSpecWithHooks directly, one level below runGroup/runSpecsRecovered (already covered by
// runner_recovery_test.go), so a phase-tracking bug shows up here even when the group-level
// contract still happens to look right.

// TestRunSpecWithHooksNoPanicRunsEverythingInOrder proves the ordinary case: every before hook runs
// in order, then the body, then every after hook in reverse order — and message/output stay empty,
// since nothing panicked.
func TestRunSpecWithHooksNoPanicRunsEverythingInOrder(t *testing.T) {
	backend := &controlledBackend{}
	ctx := &Context{backend: backend}
	var order []string
	before := []step{
		func(*Context) { order = append(order, "before0") },
		func(*Context) { order = append(order, "before1") },
	}
	body := func(*Context) { order = append(order, "body") }
	after := []step{
		func(*Context) { order = append(order, "after0") },
		func(*Context) { order = append(order, "after1") },
	}

	message, output := runSpecWithHooks(ctx, before, body, after)

	want := "[before0 before1 body after1 after0]"
	if got := "[" + strings.Join(order, " ") + "]"; got != want {
		t.Fatalf("execution order = %s, want %s", got, want)
	}
	if message != "" || output != "" {
		t.Fatalf("expected empty message/output for a spec that never panicked, got message=%q output=%q", message, output)
	}
	if len(backend.errors) != 0 {
		t.Fatalf("expected no recorded errors, got %v", backend.errors)
	}
}

// TestRunSpecWithHooksPanicInBeforeSkipsRemainingBeforeAndBody proves a panic in the first of two
// before hooks stops the second before hook and the body (they may depend on setup that never
// completed), while both after hooks still run — matching #109's "before/after run exactly once per
// spec" contract and runSpecWithHooks' documented before/body panic semantics.
func TestRunSpecWithHooksPanicInBeforeSkipsRemainingBeforeAndBody(t *testing.T) {
	backend := &controlledBackend{}
	ctx := &Context{backend: backend}
	var before1Ran, bodyRan, after0Ran, after1Ran bool
	before := []step{
		func(*Context) { panic("before boom") },
		func(*Context) { before1Ran = true },
	}
	body := func(*Context) { bodyRan = true }
	after := []step{
		func(*Context) { after0Ran = true },
		func(*Context) { after1Ran = true },
	}

	message, output := runSpecWithHooks(ctx, before, body, after)

	if before1Ran {
		t.Fatal("expected the second before hook to be skipped after the first one panicked")
	}
	if bodyRan {
		t.Fatal("expected the body to be skipped after a before-hook panic")
	}
	if !after0Ran || !after1Ran {
		t.Fatalf("expected both after hooks to still run, got after0Ran=%v after1Ran=%v", after0Ran, after1Ran)
	}
	if message != "panic: before boom" {
		t.Fatalf("expected message %q, got %q", "panic: before boom", message)
	}
	if output == "" {
		t.Fatal("expected a non-empty stack trace in output")
	}
	if len(backend.errors) != 1 || !strings.Contains(backend.errors[0], "before boom") {
		t.Fatalf("expected exactly one recorded failure mentioning the panic, got %v", backend.errors)
	}
	if !ctx.hasFailed() {
		t.Fatal("expected ctx.hasFailed() to be set after the before-hook panic")
	}
}

// TestRunSpecWithHooksPanicInBodyRunsAllAfterHooks proves a panic in the spec body itself (every
// before hook having already run) still lets every after hook run.
func TestRunSpecWithHooksPanicInBodyRunsAllAfterHooks(t *testing.T) {
	backend := &controlledBackend{}
	ctx := &Context{backend: backend}
	var before0Ran, after0Ran, after1Ran bool
	before := []step{func(*Context) { before0Ran = true }}
	body := func(*Context) { panic("body boom") }
	after := []step{
		func(*Context) { after0Ran = true },
		func(*Context) { after1Ran = true },
	}

	message, _ := runSpecWithHooks(ctx, before, body, after)

	if !before0Ran {
		t.Fatal("expected the before hook to have run before the body panicked")
	}
	if !after0Ran || !after1Ran {
		t.Fatalf("expected both after hooks to still run, got after0Ran=%v after1Ran=%v", after0Ran, after1Ran)
	}
	if message != "panic: body boom" {
		t.Fatalf("expected message %q, got %q", "panic: body boom", message)
	}
}

// TestRunSpecWithHooksFailFastNonPanicFailureInBeforeSkipsBody proves FailFast is checked after every
// before hook, same as between specs: a non-panic failure (ctx.recordFailure(), what a real assertion
// helper calls) with FailFast set also skips the body — but the after hooks still run regardless,
// since FailFast decides whether more specs/groups run, never whether resources get cleaned up.
func TestRunSpecWithHooksFailFastNonPanicFailureInBeforeSkipsBody(t *testing.T) {
	backend := &controlledBackend{}
	ctx := &Context{backend: backend}
	ctx.SetFailFast(true)
	var before1Ran, bodyRan, afterRan bool
	before := []step{
		func(ctx *Context) { ctx.recordFailure() },
		func(*Context) { before1Ran = true },
	}
	body := func(*Context) { bodyRan = true }
	after := []step{func(*Context) { afterRan = true }}

	runSpecWithHooks(ctx, before, body, after)

	if before1Ran {
		t.Fatal("expected the second before hook to be skipped once FailFast saw a failure")
	}
	if bodyRan {
		t.Fatal("expected the body to be skipped once FailFast saw a before-hook failure")
	}
	if !afterRan {
		t.Fatal("expected the after hook to still run under FailFast")
	}
}

// TestRunSpecWithHooksPanicInFirstExecutedAfterHookStillRunsRemaining proves a panic in the
// first-executed after hook (the highest index, since after hooks run in reverse order) does not
// stop the remaining after hooks from running — the fallback-to-per-hook-recovery path this
// optimization introduces for the tail of the after list.
func TestRunSpecWithHooksPanicInFirstExecutedAfterHookStillRunsRemaining(t *testing.T) {
	backend := &controlledBackend{}
	ctx := &Context{backend: backend}
	var after0Ran bool
	after := []step{
		func(*Context) { after0Ran = true },
		func(*Context) { panic("after boom") }, // index 1, runs first (reverse order)
	}

	message, _ := runSpecWithHooks(ctx, nil, func(*Context) {}, after)

	if !after0Ran {
		t.Fatal("expected the remaining after hook (index 0) to still run after index 1 panicked")
	}
	if message != "panic in after hook: after boom" {
		t.Fatalf("expected message %q, got %q", "panic in after hook: after boom", message)
	}
	if len(backend.errors) != 1 || !strings.Contains(backend.errors[0], "after boom") {
		t.Fatalf("expected exactly one recorded failure mentioning the panic, got %v", backend.errors)
	}
}

// TestRunSpecWithHooksPanicInMiddleAfterHookStillRunsSiblings proves a panic in a middle after hook
// (of three) doesn't stop the one that already ran (higher index, executed earlier) from having run,
// nor the one still due (lower index) from running afterward.
func TestRunSpecWithHooksPanicInMiddleAfterHookStillRunsSiblings(t *testing.T) {
	backend := &controlledBackend{}
	ctx := &Context{backend: backend}
	var order []string
	after := []step{
		func(*Context) { order = append(order, "after0") },       // index 0, runs last
		func(*Context) { panic("middle boom") },                  // index 1, runs second
		func(*Context) { order = append(order, "after2-first") }, // index 2, runs first
	}

	runSpecWithHooks(ctx, nil, func(*Context) {}, after)

	want := "[after2-first after0]"
	if got := "[" + strings.Join(order, " ") + "]"; got != want {
		t.Fatalf("execution order = %s, want %s", got, want)
	}
	if len(backend.errors) != 1 || !strings.Contains(backend.errors[0], "middle boom") {
		t.Fatalf("expected exactly one recorded failure mentioning the panic, got %v", backend.errors)
	}
}

// TestRunSpecWithHooksMultipleAfterHookPanicsFirstWriteWinsMessage proves two separate after-hook
// panics (the first-executed and the last-executed, of three) are both individually recorded as
// failures, while the returned message follows first-write-wins: the first one to panic in execution
// order (the highest index) wins, matching runAfterRecovered's pre-existing contract.
func TestRunSpecWithHooksMultipleAfterHookPanicsFirstWriteWinsMessage(t *testing.T) {
	backend := &controlledBackend{}
	ctx := &Context{backend: backend}
	var middleRan bool
	after := []step{
		func(*Context) { panic("last boom") },  // index 0, runs third (last)
		func(*Context) { middleRan = true },    // index 1, runs second
		func(*Context) { panic("first boom") }, // index 2, runs first
	}

	message, _ := runSpecWithHooks(ctx, nil, func(*Context) {}, after)

	if !middleRan {
		t.Fatal("expected the non-panicking middle after hook to still run")
	}
	if message != "panic in after hook: first boom" {
		t.Fatalf("expected the first-executed panic's message to win, got %q", message)
	}
	foundFirst, foundLast := false, false
	for _, e := range backend.errors {
		if strings.Contains(e, "first boom") {
			foundFirst = true
		}
		if strings.Contains(e, "last boom") {
			foundLast = true
		}
	}
	if !foundFirst || !foundLast {
		t.Fatalf("expected both after-hook panics to be individually recorded, got %v", backend.errors)
	}
	if len(backend.errors) != 2 {
		t.Fatalf("expected exactly two recorded failures, got %v", backend.errors)
	}
}

// TestRunSpecWithHooksBeforePanicMessageWinsOverAfterHookPanic proves the documented priority: when
// both a before/body panic and a later after-hook panic occur for the same spec, the before/body
// panic's message wins (it happened first) — but the after-hook panic is still individually recorded
// as its own failure, it just doesn't win the returned message/output pair.
func TestRunSpecWithHooksBeforePanicMessageWinsOverAfterHookPanic(t *testing.T) {
	backend := &controlledBackend{}
	ctx := &Context{backend: backend}
	before := []step{func(*Context) { panic("before boom") }}
	after := []step{func(*Context) { panic("after boom") }}

	message, _ := runSpecWithHooks(ctx, before, func(*Context) {}, after)

	if message != "panic: before boom" {
		t.Fatalf("expected the before-hook panic's message to win, got %q", message)
	}
	foundBefore, foundAfter := false, false
	for _, e := range backend.errors {
		if strings.Contains(e, "before boom") {
			foundBefore = true
		}
		if strings.Contains(e, "after boom") {
			foundAfter = true
		}
	}
	if !foundBefore || !foundAfter {
		t.Fatalf("expected both panics to be individually recorded, got %v", backend.errors)
	}
}

// TestRunnerRunGoexitInAfterHookStopsRemainingAfterHooksRealProcess pins a pre-existing, real-Goexit
// edge case that the single-recover refactor must not silently change: a real testing.T.Fatal (via
// EqualTo) inside an after hook calls runtime.Goexit, which unwinds the current spec's subtest
// goroutine right there — recover() cannot observe it, and unlike a genuine panic, execution never
// returns to the after-hook loop to continue with the remaining (lower-index) after hooks. This is
// unrelated to FailFast (which never gates after-hook execution) and unrelated to a panic (which
// runSpecWithHooks does recover and continue past, see
// TestRunSpecWithHooksPanicInFirstExecutedAfterHookStillRunsRemaining above) — it is Goexit itself
// that prevents the loop from resuming, the same way it already prevents runStepRecovered's caller
// from resuming. Same subprocess pattern as the Goexit tests in runner_recovery_test.go, since a real
// Fatal must run inside a real *testing.T to trigger Goexit at all (a fake backend panics with a
// sentinel instead — see controlledBackend.FailNow).
func TestRunnerRunGoexitInAfterHookStopsRemainingAfterHooksRealProcess(t *testing.T) {
	if os.Getenv("GO_SPECS_RUNNER_GOEXIT_IN_AFTER_HELPER") == "1" {
		prog := &Program{
			Groups: []group{
				{
					after: []step{
						func(*Context) { fmt.Println("after0 ran") }, // index 0, would run second
						func(ctx *Context) { EqualTo(ctx, 1, 2) },    // index 1, runs first (reverse order)
					},
					specs: []step{func(*Context) { fmt.Println("spec ran") }},
				},
			},
		}
		NewRunner(prog).Run(t)
		fmt.Println("reached end of helper")
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestRunnerRunGoexitInAfterHookStopsRemainingAfterHooksRealProcess$")
	cmd.Env = append(os.Environ(), "GO_SPECS_RUNNER_GOEXIT_IN_AFTER_HELPER=1")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected the after hook's real Fatal to fail the process, but it passed: %s", output)
	}
	if !strings.Contains(string(output), "spec ran") {
		t.Fatalf("expected the spec body to have run, got: %s", output)
	}
	if !strings.Contains(string(output), "reached end of helper") {
		t.Fatalf("expected the outer Run(t) call to still return normally (Goexit only unwinds the spec's own subtest goroutine), got: %s", output)
	}
	if strings.Contains(string(output), "after0 ran") {
		t.Fatalf("expected the remaining after hook (index 0) to NOT run once index 1's real Fatal called runtime.Goexit, got: %s", output)
	}
}
