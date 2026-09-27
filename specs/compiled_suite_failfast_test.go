// compiled_suite_failfast_test.go proves CompiledSuite.SetFailFast (issue #251) on the flat path —
// a suite with no BeforeAll/AfterAll/ItParallel group, which stays on runPlanSpecsInOrder
// (execution_plan.go) exactly as it did before this feature (H10). See
// group_hooks_failfast_test.go for the group-path equivalent (T2,
// docs/SUITE_HOOKS_CONTRACT.md H9).
package specs

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// failFastFakeTB is a testing.TB that records nothing and never aborts the goroutine on
// Fatal/Fatalf/FailNow, mirroring sharding_config_test.go's shardCapturingTB. The embedded TB
// stays nil deliberately: any method this file doesn't override panics with a nil dereference
// instead of silently reaching a real *testing.T, which keeps the fake honest about what it
// covers. CompiledSuite.Run always wraps its tb argument in a *runnableBackend and only opens a
// real per-spec (or per-group) subtest when the wrapped value's concrete type is *testing.T
// (runSpecProgram / group_hooks.go's groupRun.real) — this type deliberately isn't, so every spec
// and group hook below runs on the flat, non-isolated, single-goroutine path, the same one every
// engine falls back to for a *testing.B (docs/SUITE_HOOKS_CONTRACT.md's "Every other backend").
// That is what lets a failing spec or hook here be observed deterministically, in-process, without
// a real Fatalf ending this test's own goroutine or a subtest failure marking it failed.
type failFastFakeTB struct {
	testing.TB
}

func (failFastFakeTB) Helper()                           {}
func (failFastFakeTB) Fatalf(format string, args ...any) {}

// Name is overridden because BuildSuite's bytecode-compiler path never sets CompiledSuite.Name
// from its own name argument (it only feeds it to the compiler's PushScope), so CompiledSuite.Run
// falls back to backend.Name() for every suite this file builds.
func (failFastFakeTB) Name() string { return "failFastFakeTB" }

// TestCompiledSuiteSetFailFastStopsLaterSpecsButRunsTheFailingSpecsAfterEach proves the core T1
// contract on the flat path: a failing spec still runs its own AfterEach, and after it no further
// spec starts — the specs that never start are not reported at all (docs/SUITE_HOOKS_CONTRACT.md
// H9, mirroring Runner.FailFast).
func TestCompiledSuiteSetFailFastStopsLaterSpecsButRunsTheFailingSpecsAfterEach(t *testing.T) {
	var log []string
	suite := BuildSuite(nil, "suite", func(s *Spec) {
		s.AfterEach(func(*Context) { log = append(log, "after") })
		s.It("one", func(*Context) { log = append(log, "one") })
		s.It("two", func(ctx *Context) { log = append(log, "two"); EqualTo(ctx, 1, 2) })
		s.It("three", func(*Context) { log = append(log, "three") })
	})
	rep := &recordingReporter{}
	suite.Reporter = rep
	suite.SetFailFast(true)
	suite.Run(failFastFakeTB{})

	wantLog := "one,after,two,after"
	if got := strings.Join(log, ","); got != wantLog {
		t.Fatalf("execution log = %q, want %q (spec three must never run)", got, wantLog)
	}
	if len(rep.specFinished) != 2 {
		t.Fatalf("expected exactly 2 reported specs (one, two), spec three never started so it must not "+
			"be reported at all: got %d: %+v", len(rep.specFinished), rep.specFinished)
	}
	if got := rep.specFinished[0]; got.Name != "one" || got.Failed {
		t.Errorf("spec one reported wrong: %+v", got)
	}
	if got := rep.specFinished[1]; got.Name != "two" || !got.Failed {
		t.Errorf("spec two reported wrong: %+v", got)
	}
}

// TestCompiledSuiteWithoutSetFailFastRunsEverySpec is the control for the test above: the exact
// same suite shape, without ever calling SetFailFast, must run and report every spec exactly as it
// did before this feature existed.
func TestCompiledSuiteWithoutSetFailFastRunsEverySpec(t *testing.T) {
	var log []string
	suite := BuildSuite(nil, "suite", func(s *Spec) {
		s.It("one", func(*Context) { log = append(log, "one") })
		s.It("two", func(ctx *Context) { log = append(log, "two"); EqualTo(ctx, 1, 2) })
		s.It("three", func(*Context) { log = append(log, "three") })
	})
	rep := &recordingReporter{}
	suite.Reporter = rep
	suite.Run(failFastFakeTB{})

	wantLog := "one,two,three"
	if got := strings.Join(log, ","); got != wantLog {
		t.Fatalf("execution log = %q, want %q (every spec must run without FailFast)", got, wantLog)
	}
	if len(rep.specFinished) != 3 {
		t.Fatalf("expected all 3 specs reported, got %d: %+v", len(rep.specFinished), rep.specFinished)
	}
}

// TestCompiledSuiteSetFailFastFalseLeavesGroupStorageNil proves the H10 cost claim in
// SetFailFast's own doc comment: calling SetFailFast(false) on a suite that has never registered a
// BeforeAll/AfterAll/SkipIt/PendingIt/ItParallel, and never called SetFailFast(true) either, must
// not allocate the lazy group-storage pointer just to remember "false".
func TestCompiledSuiteSetFailFastFalseLeavesGroupStorageNil(t *testing.T) {
	suite := BuildSuite(nil, "suite", func(s *Spec) {
		s.It("one", func(*Context) {})
	})
	suite.SetFailFast(false)
	if suite.groups != nil {
		t.Fatalf("SetFailFast(false) on a suite with no other group storage allocated groups: %+v", suite.groups)
	}

	suite.SetFailFast(true)
	if suite.groups == nil {
		t.Fatal("SetFailFast(true) must allocate group storage to remember the flag")
	}

	suite.SetFailFast(false)
	if suite.groups == nil {
		t.Fatal("SetFailFast(false) after SetFailFast(true) must not free the already-allocated group storage")
	}
}

// TestCompiledSuiteSetFailFastDoesNotStopOnASkippedSpec proves a compile-time SkipIt mark never
// triggers the stop: marks are reported once, up front, independently of the ordered plan a
// failing spec walks (CompiledSuite.runSpecs), so they can never be mistaken for a failure.
func TestCompiledSuiteSetFailFastDoesNotStopOnASkippedSpec(t *testing.T) {
	var log []string
	suite := BuildSuite(nil, "suite", func(s *Spec) {
		s.SkipIt("skipped", func(*Context) { t.Error("a SkipIt body must never run") })
		s.It("one", func(*Context) { log = append(log, "one") })
	})
	rep := &recordingReporter{}
	suite.Reporter = rep
	suite.SetFailFast(true)
	suite.Run(failFastFakeTB{})

	if got := strings.Join(log, ","); got != "one" {
		t.Fatalf("execution log = %q, want \"one\": a SkipIt mark must not trigger FailFast", got)
	}
	if len(rep.specFinished) != 2 {
		t.Fatalf("expected 2 reported specs (the skip mark, then one), got %d: %+v", len(rep.specFinished), rep.specFinished)
	}
	if got := rep.specFinished[0]; got.Name != "skipped" || !got.Skipped || got.Failed {
		t.Errorf("skipped spec reported wrong: %+v", got)
	}
	if got := rep.specFinished[1]; got.Name != "one" || got.Failed {
		t.Errorf("spec one reported wrong: %+v", got)
	}
}

// TestCompiledSuiteSetFailFastDoesNotStopOnAFilteredSpecRealProcess proves a spec `go test -run`
// selects out is never treated as a failure: it is reported Filtered with Failed=false (#111,
// unchanged by this feature), so a spec declared before the -run-selected one that would have
// failed if it ran must not stop the selected spec from running. A real *testing.T and a
// subprocess are required here: only a real subtest can be filtered by -run at all, and a genuine
// assertion failure inside it would otherwise mark this very test failed too (see
// TestSpecRunFilteredSpecsAreReportedAsFilteredRealProcess for the same pattern).
func TestCompiledSuiteSetFailFastDoesNotStopOnAFilteredSpecRealProcess(t *testing.T) {
	const helperEnv = "GO_SPECS_FAILFAST_FILTER_HELPER"
	if os.Getenv(helperEnv) == "1" {
		var rep recordingReporter
		suite := BuildSuite(t, "suite", func(s *Spec) {
			s.It("one", func(ctx *Context) { EqualTo(ctx, 1, 2) }) // filtered out below, so never runs
			s.It("two", func(*Context) { fmt.Println("two ran") })
		})
		suite.Reporter = &rep
		suite.SetFailFast(true)
		suite.Run(t)
		fmt.Printf("suite finished total=%d failed=%d filtered=%d\n",
			rep.suiteFinished[0].TotalSpecs, rep.suiteFinished[0].FailedSpecs, rep.suiteFinished[0].FilteredSpecs)
		return
	}

	cmd := exec.Command(os.Args[0],
		"-test.run=^TestCompiledSuiteSetFailFastDoesNotStopOnAFilteredSpecRealProcess$/^suite$/^two$",
	)
	cmd.Env = append(os.Environ(), helperEnv+"=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helper run failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "two ran") {
		t.Fatalf("expected the -run-selected spec to still run, got: %s", output)
	}
	if !strings.Contains(string(output), "suite finished total=2 failed=0 filtered=1") {
		t.Fatalf("expected SuiteFinished total=2 failed=0 filtered=1 (the filtered-out spec must not "+
			"count as a failure), got: %s", output)
	}
}
