// isolation_state_reuse_test.go pins runSpecProgramIsolated's per-spec state reset (isoMessage,
// isoOutput, isoStarted, isoDone, isoSub -- see Context's doc comment and #244/#299) across a
// *Context explicitly reused for two consecutive specs.
//
// Unlike builtin_assertion_message_report_test.go's TestSequentialIsolationDoesNotReusePriorFailure,
// which drives the full Describe/ExecutionPlan engine and observes the same pooled Context reused
// spec to spec only because contextPool happens to hand the same object back on a single sequential
// goroutine, the tests here call runSpecProgramIsolated directly against one *Context value they
// construct and keep themselves. That pins reuse deterministically: sync.Pool reuse is never
// guaranteed by the language or by testing/sync's documented contract (a GC can clear a pool between
// two Gets, and pools are sharded per-P, so a goroutine that hops Ps can miss its own prior Put) --
// see contextPool's doc comment in context.go.
//
// Each scenario is reported as an actual report.SpecResultEvent, built by the same
// reportSpecStarted/reportSpecFinished pair runExecution itself calls (execution_plan.go), not a
// hand-rolled substitute -- so passing here means the exact event a report.EventReporter would
// receive is clean, not just the raw return values.
package specs

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/getsyntegrity/go-specs/report"
)

// runIsolatedSpecEvent drives one spec through runSpecProgramIsolated against ctx and returns the
// report.SpecResultEvent runExecution would report for it (Filtered mirrors !ran exactly as
// runExecution computes it -- see its doc comment).
func runIsolatedSpecEvent(t *testing.T, ctx *Context, program []Instruction, subtestName string) report.SpecResultEvent {
	t.Helper()
	var rep recordingReporter
	started := reportSpecStarted(&rep, subtestName, nil)
	message, output, ran, failed, skipped := runSpecProgramIsolated(t, ctx, program, subtestName)
	reportSpecFinished(&rep, started, specResult{Failed: failed, Message: message, Output: output, Filtered: !ran, Skipped: skipped})
	if len(rep.specFinished) != 1 {
		t.Fatalf("expected exactly one SpecFinished event for %q, got %d", subtestName, len(rep.specFinished))
	}
	return rep.specFinished[0]
}

// printIsolatedSpecEvent prints e in a format the host process (running outside -test.run
// restrictions) can parse back into assertions, deliberately spelling out every field this file
// cares about rather than relying on %+v's layout.
func printIsolatedSpecEvent(label string, e report.SpecResultEvent) {
	fmt.Printf("ISO_EVENT label=%s failed=%t filtered=%t skipped=%t message=%q output=%q\n",
		label, e.Failed, e.Filtered, e.Skipped, e.Message, e.Output)
}

// wantIsolatedSpecEvent is the exact line printIsolatedSpecEvent produces for the given field
// values, so each scenario below asserts against one literal instead of several substring checks
// that could each pass for the wrong reason.
func wantIsolatedSpecEvent(label string, failed, filtered, skipped bool, message, output string) string {
	return fmt.Sprintf("ISO_EVENT label=%s failed=%t filtered=%t skipped=%t message=%q output=%q",
		label, failed, filtered, skipped, message, output)
}

// runIsolationReuseHelperProcess runs cmd (this same test binary) with extra env, returning combined
// output. It never lets a failing helper process abort the host test: the helper's specs are
// deliberately made to fail so their reported events can be inspected, so a non-nil err is the
// expected outcome and only the caller decides whether that expectation held.
func runIsolationReuseHelperProcess(t *testing.T, runPattern string, env string) string {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run="+runPattern, "-test.v=true")
	cmd.Env = append(os.Environ(), env)
	output, _ := cmd.CombinedOutput()
	return string(output)
}

// TestIsolatedContextReuseDoesNotLeakPanicStackIntoFollowingAssertionFailure pins scenario (a): a
// recovered panic populates ctx.isoMessage/isoOutput (the panic text and its stack trace -- see
// runSpecProgramIsolated's doc comment); the very next spec on the SAME Context fails through a
// built-in assertion, which ends via backend.Fatalf/runtime.Goexit before runProgram's normal return
// can overwrite those cached fields (see runProgram's doc comment on the isolation path). The second
// spec's reported event must carry only its own assertion message and no trace of the first spec's
// stack.
func TestIsolatedContextReuseDoesNotLeakPanicStackIntoFollowingAssertionFailure(t *testing.T) {
	const helperEnvKey = "GO_SPECS_ISOLATED_REUSE_PANIC_THEN_ASSERTION"
	if os.Getenv(helperEnvKey) == "1" {
		ctx := &Context{}
		first := runIsolatedSpecEvent(t, ctx, []Instruction{{Code: OpBody, Fn: func(*Context) { panic("boom") }}}, "first")
		second := runIsolatedSpecEvent(t, ctx, []Instruction{{Code: OpBody, Fn: func(ctx *Context) { ctx.Expect(42).To(Equal(43)) }}}, "second")
		printIsolatedSpecEvent("first", first)
		printIsolatedSpecEvent("second", second)
		return
	}

	output := runIsolationReuseHelperProcess(t, "^TestIsolatedContextReuseDoesNotLeakPanicStackIntoFollowingAssertionFailure$", helperEnvKey+"=1")

	wantFirst := wantIsolatedSpecEvent("first", true, false, false, "panic: boom", "")
	if !strings.Contains(output, "ISO_EVENT label=first failed=true filtered=false skipped=false message=\"panic: boom\"") {
		t.Fatalf("first spec did not report its own panic; want prefix of %s, got:\n%s", wantFirst, output)
	}
	wantSecond := wantIsolatedSpecEvent("second", true, false, false, wantEqualFailureMessage, "")
	if !strings.Contains(output, wantSecond) {
		t.Fatalf("second spec inherited the first spec's panic stack/message; want %s, got:\n%s", wantSecond, output)
	}
}

// TestIsolatedContextReuseFilteredSpecDoesNotInheritPriorFailure pins scenario (b): the first spec on
// a reused Context panics and fails; the second is discarded by `go test -run` before its body ever
// runs (testing.T.Run returns true without invoking f -- see runSpecProgramIsolated's doc comment on
// `ran`). The filtered spec must report Filtered (ran=false) with no Failed, Skipped, Message or
// Output carried over from the first -- in particular it must not read ctx.isoSub, which a filtered
// subtest never sets (isoSub is only ever assigned by a closure invocation, which a filtered subtest
// never gets; the first spec's own *testing.T was already cleared from it once that spec finished,
// see #304).
func TestIsolatedContextReuseFilteredSpecDoesNotInheritPriorFailure(t *testing.T) {
	const helperEnvKey = "GO_SPECS_ISOLATED_REUSE_FILTERED"
	if os.Getenv(helperEnvKey) == "1" {
		ctx := &Context{}
		first := runIsolatedSpecEvent(t, ctx, []Instruction{{Code: OpBody, Fn: func(*Context) { panic("boom") }}}, "first")
		second := runIsolatedSpecEvent(t, ctx, []Instruction{{Code: OpBody, Fn: func(ctx *Context) { ctx.Expect(42).To(Equal(43)) }}}, "second")
		printIsolatedSpecEvent("first", first)
		printIsolatedSpecEvent("second", second)
		return
	}

	// Only the "first" subtest of this exact test matches; "second" is filtered by testing itself,
	// so its body (and therefore its Instruction program, which would otherwise fail the process)
	// never runs.
	runPattern := "^TestIsolatedContextReuseFilteredSpecDoesNotInheritPriorFailure$/^first$"
	output := runIsolationReuseHelperProcess(t, runPattern, helperEnvKey+"=1")

	if !strings.Contains(output, "ISO_EVENT label=first failed=true") {
		t.Fatalf("expected the unfiltered first spec to report its own panic failure, got:\n%s", output)
	}
	wantSecond := wantIsolatedSpecEvent("second", false, true, false, "", "")
	if !strings.Contains(output, wantSecond) {
		t.Fatalf("filtered second spec inherited state from the first; want %s, got:\n%s", wantSecond, output)
	}
}

// TestIsolatedContextReusePassingSpecAfterFailureReportsCleanEvent pins scenario (c): the first spec
// on a reused Context fails a built-in assertion; the second spec, run right after on the same
// Context, passes outright. Its event must be entirely clean: Failed=false and both Message and
// Output empty, even though the Context that produced it still carries the first spec's isoSub/
// isoMessage/isoOutput fields until this call resets them.
func TestIsolatedContextReusePassingSpecAfterFailureReportsCleanEvent(t *testing.T) {
	const helperEnvKey = "GO_SPECS_ISOLATED_REUSE_FAIL_THEN_PASS"
	if os.Getenv(helperEnvKey) == "1" {
		ctx := &Context{}
		first := runIsolatedSpecEvent(t, ctx, []Instruction{{Code: OpBody, Fn: func(ctx *Context) { ctx.Expect(42).To(Equal(43)) }}}, "first")
		second := runIsolatedSpecEvent(t, ctx, []Instruction{{Code: OpBody, Fn: func(*Context) {}}}, "second")
		printIsolatedSpecEvent("first", first)
		printIsolatedSpecEvent("second", second)
		return
	}

	output := runIsolationReuseHelperProcess(t, "^TestIsolatedContextReusePassingSpecAfterFailureReportsCleanEvent$", helperEnvKey+"=1")

	wantFirst := wantIsolatedSpecEvent("first", true, false, false, wantEqualFailureMessage, "")
	if !strings.Contains(output, wantFirst) {
		t.Fatalf("expected the unfiltered first spec to report its own assertion failure; want %s, got:\n%s", wantFirst, output)
	}
	wantSecond := wantIsolatedSpecEvent("second", false, false, false, "", "")
	if !strings.Contains(output, wantSecond) {
		t.Fatalf("passing second spec inherited state from the failing first; want %s, got:\n%s", wantSecond, output)
	}
}

// TestIsolatedContextReuseSkippedSpecDoesNotLeakIntoFollowingSpec pins the SkipNow/Goexit-without-
// failure case: the first spec on a reused Context calls ctx.T.SkipNow() directly (Skipped=true,
// Failed=false, no message -- #253/#254's direct-ctx.T path never touches ctx.failure or
// isoMessage/isoOutput at all). Neither spec fails, so this runs in-process, no subprocess needed.
func TestIsolatedContextReuseSkippedSpecDoesNotLeakIntoFollowingSpec(t *testing.T) {
	ctx := &Context{}
	first := runIsolatedSpecEvent(t, ctx, []Instruction{{Code: OpBody, Fn: func(ctx *Context) { ctx.T.SkipNow() }}}, "first")
	second := runIsolatedSpecEvent(t, ctx, []Instruction{{Code: OpBody, Fn: func(*Context) {}}}, "second")

	if !first.Skipped || first.Failed || first.Message != "" {
		t.Fatalf("expected first spec Skipped=true, Failed=false, Message=\"\", got %+v", first)
	}
	if second.Skipped || second.Failed || second.Message != "" || second.Output != "" || second.Filtered {
		t.Fatalf("second spec inherited state from the skipped first; got %+v", second)
	}
}
