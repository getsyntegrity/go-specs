// group_hooks_failfast_test.go proves CompiledSuite.SetFailFast (issue #251) on the group path —
// a suite that registers at least one BeforeAll/AfterAll or ItParallel, which runs through
// runPlanWithGroups (group_hooks.go) instead of the flat path. See
// compiled_suite_failfast_test.go for the flat-path equivalent (T1) and
// docs/SUITE_HOOKS_CONTRACT.md H9 for the normative contract this pins.
package specs

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestGroupPathSetFailFastCutsLaterGroupsAndSpecsButRunsEnteredAfterAll proves H9's core group-path
// claim: a failing spec inside one hooked group stops its own later sibling spec, stops every
// later sibling group entirely (never entered at all), but the failing group's own AfterAll still
// runs because it was already entered when the stop happened.
func TestGroupPathSetFailFastCutsLaterGroupsAndSpecsButRunsEnteredAfterAll(t *testing.T) {
	var log []string
	suite := BuildSuite(nil, "suite", func(s *Spec) {
		s.When("A", func(w *Spec) {
			w.BeforeAll(func(*Context) { log = append(log, "A.BeforeAll") })
			w.AfterAll(func(*Context) { log = append(log, "A.AfterAll") })
			w.It("a1", func(ctx *Context) { log = append(log, "A.a1"); EqualTo(ctx, 1, 2) })
			w.It("a2", func(*Context) { log = append(log, "A.a2") })
		})
		s.When("B", func(w *Spec) {
			w.BeforeAll(func(*Context) { log = append(log, "B.BeforeAll") })
			w.AfterAll(func(*Context) { log = append(log, "B.AfterAll") })
			w.It("b1", func(*Context) { log = append(log, "B.b1") })
		})
	})
	suite.SetFailFast(true)
	suite.Run(failFastFakeTB{})

	want := "A.BeforeAll,A.a1,A.AfterAll"
	if got := strings.Join(log, ","); got != want {
		t.Fatalf("execution log = %q, want %q (a2 and every part of group B must never run)", got, want)
	}
}

// TestGroupPathSetFailFastStopsOnBeforeAllFailure proves a failed BeforeAll counts as a failure for
// FailFast (H9), on top of H4's pre-existing "the group's own specs are skipped": group B, entered
// after group A, must never start at all.
func TestGroupPathSetFailFastStopsOnBeforeAllFailure(t *testing.T) {
	var log []string
	suite := BuildSuite(nil, "suite", func(s *Spec) {
		s.When("A", func(w *Spec) {
			w.BeforeAll(func(ctx *Context) { log = append(log, "A.BeforeAll"); EqualTo(ctx, 1, 2) })
			w.AfterAll(func(*Context) { log = append(log, "A.AfterAll") })
			w.It("a1", func(*Context) { log = append(log, "a1") })
		})
		s.When("B", func(w *Spec) {
			w.BeforeAll(func(*Context) { log = append(log, "B.BeforeAll") })
			w.It("b1", func(*Context) { log = append(log, "b1") })
		})
	})
	suite.SetFailFast(true)
	suite.Run(failFastFakeTB{})

	want := "A.BeforeAll,A.AfterAll"
	if got := strings.Join(log, ","); got != want {
		t.Fatalf("execution log = %q, want %q (a1 is skipped by the failed BeforeAll (H4), "+
			"A.AfterAll still runs (H5/H6), and group B must never start (H9))", got, want)
	}
}

// TestGroupPathSetFailFastStopsOnAfterAllFailure proves a failed AfterAll counts as a failure for
// FailFast (H9) too: group B, a sibling declared after A, must never start.
func TestGroupPathSetFailFastStopsOnAfterAllFailure(t *testing.T) {
	var log []string
	suite := BuildSuite(nil, "suite", func(s *Spec) {
		s.When("A", func(w *Spec) {
			w.AfterAll(func(ctx *Context) { log = append(log, "A.AfterAll"); EqualTo(ctx, 1, 2) })
			w.It("a1", func(*Context) { log = append(log, "a1") })
		})
		s.When("B", func(w *Spec) {
			w.It("b1", func(*Context) { log = append(log, "b1") })
		})
	})
	suite.SetFailFast(true)
	suite.Run(failFastFakeTB{})

	want := "a1,A.AfterAll"
	if got := strings.Join(log, ","); got != want {
		t.Fatalf("execution log = %q, want %q (group B must never start after A's AfterAll fails)", got, want)
	}
}

// TestGroupPathWithoutSetFailFastRunsEveryGroupAndSpec is the control for the three tests above:
// the same shape of suite, without ever calling SetFailFast, must run every group and every spec
// exactly as it did before this feature existed.
func TestGroupPathWithoutSetFailFastRunsEveryGroupAndSpec(t *testing.T) {
	var log []string
	suite := BuildSuite(nil, "suite", func(s *Spec) {
		s.When("A", func(w *Spec) {
			w.BeforeAll(func(*Context) { log = append(log, "A.BeforeAll") })
			w.AfterAll(func(*Context) { log = append(log, "A.AfterAll") })
			w.It("a1", func(ctx *Context) { log = append(log, "A.a1"); EqualTo(ctx, 1, 2) })
			w.It("a2", func(*Context) { log = append(log, "A.a2") })
		})
		s.When("B", func(w *Spec) {
			w.BeforeAll(func(*Context) { log = append(log, "B.BeforeAll") })
			w.AfterAll(func(*Context) { log = append(log, "B.AfterAll") })
			w.It("b1", func(*Context) { log = append(log, "B.b1") })
		})
	})
	suite.Run(failFastFakeTB{})

	want := "A.BeforeAll,A.a1,A.a2,A.AfterAll,B.BeforeAll,B.b1,B.AfterAll"
	if got := strings.Join(log, ","); got != want {
		t.Fatalf("execution log = %q, want %q (every group and spec must run without FailFast)", got, want)
	}
}

// TestGroupPathSetFailFastAllowsItParallelBatchToFinishBeforeStoppingRealProcess proves the
// ItParallel half of H9: an already-launched batch runs every sibling to completion — a real
// concurrent claim, so it needs a real *testing.T and its own goroutines, unlike the tests above —
// and only once every sibling has finished does the stop apply, before the next sequential spec.
//
// A subprocess is required: p1's real assertion failure fails its own subtest, which (as with any
// nested subtest failure) would also mark this very test failed if run in-process.
func TestGroupPathSetFailFastAllowsItParallelBatchToFinishBeforeStoppingRealProcess(t *testing.T) {
	const helperEnv = "GO_SPECS_FAILFAST_ITPARALLEL_HELPER"
	if os.Getenv(helperEnv) == "1" {
		var rep recordingReporter
		suite := BuildSuite(t, "suite", func(s *Spec) {
			s.ItParallel("p1", func(ctx *Context) { EqualTo(ctx, 1, 2) })
			s.ItParallel("p2", func(*Context) { fmt.Println("p2 ran") })
			s.It("after", func(*Context) { fmt.Println("after ran") })
		})
		suite.Reporter = &rep
		suite.SetFailFast(true)
		suite.Run(t)
		fmt.Printf("suite finished total=%d failed=%d\n", rep.suiteFinished[0].TotalSpecs, rep.suiteFinished[0].FailedSpecs)
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestGroupPathSetFailFastAllowsItParallelBatchToFinishBeforeStoppingRealProcess$")
	cmd.Env = append(os.Environ(), helperEnv+"=1")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected the failing parallel spec to fail the process, but it passed: %s", output)
	}
	if !strings.Contains(string(output), "p2 ran") {
		t.Fatalf("expected the sibling ItParallel spec to still finish despite p1 failing, got: %s", output)
	}
	if strings.Contains(string(output), "after ran") {
		t.Fatalf("expected FailFast to stop the spec following the parallel batch from running, got: %s", output)
	}
	if !strings.Contains(string(output), "suite finished total=2 failed=1") {
		t.Fatalf("expected SuiteFinished total=2 failed=1 (only the two parallel specs ran), got: %s", output)
	}
}
