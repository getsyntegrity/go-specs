package specs

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/getsyntegrity/go-specs/report"
)

// before_after_all_failure_test.go pins the failure semantics of once-per-group hooks (issue
// #207, docs/SUITE_HOOKS_CONTRACT.md H4-H7). Every scenario here deliberately fails a BeforeAll or
// AfterAll hook, which — same as a deliberately failing spec anywhere else in this package's test
// suite (see execution_plan_recovery_test.go) — must not itself mark the outer `go test` function
// failed just for exercising the failure path. So these run the compiled plan directly through
// runCompiledSuiteWith against a controlledBackend (this package's established fake backend, see
// execution_plan_test.go), not through Describe/DescribeWithReporter against the real *testing.T:
// a controlledBackend's Fatal/FailNow panics with the local isolatedCaseAbort sentinel and never
// touches the real test, exactly like every other recovery test in this package already does. The
// one exception is TestBeforeAllFailNowGoexitDoesNotAbortSiblingGroupRealProcess, which needs a
// genuine *testing.T to prove real per-hook subtest isolation and therefore uses the subprocess
// pattern established by execution_plan_isolation_test.go instead.
//
// Happy-path ordering (no failures) lives in before_after_all_test.go.

// buildGroupHookSuite compiles fn (without running it) via BuildSuite(nil, ...), ready to run
// directly through runCompiledSuiteWith against a fake backend — the same technique
// spec_concurrency_test.go already uses for BuildSuite(nil, ...).
func buildGroupHookSuite(name string, fn func(*Spec)) *CompiledSuite {
	return BuildSuite(nil, name, fn)
}

// TestBeforeAllFailureSkipsDescendantsReportsOneCaseAndRunsAfterAll covers H4 and H5 together: a
// failing BeforeAll (an ordinary ctx assertion) produces exactly one [BeforeAll] case, every
// descendant spec is reported skipped with a message naming the group, no spec body or BeforeEach
// runs, and the group's own AfterAll still runs.
func TestBeforeAllFailureSkipsDescendantsReportsOneCaseAndRunsAfterAll(t *testing.T) {
	var specRan, afterAllRan bool
	suite := buildGroupHookSuite("FailingGroup", func(s *Spec) {
		s.When("broken", func(w *Spec) {
			w.BeforeAll(func(ctx *Context) { ctx.Expect(false).To(BeTrue()) })
			w.AfterAll(func(*Context) { afterAllRan = true })
			w.BeforeEach(func(*Context) { specRan = true }) // must never run either
			w.It("never runs", func(*Context) { specRan = true })
			w.When("nested", func(n *Spec) {
				n.It("also never runs", func(*Context) { specRan = true })
			})
		})
	})
	rep := &recordingReporter{}
	runCompiledSuiteWith(&controlledBackend{}, rep, suite)

	if specRan {
		t.Fatal("a descendant spec or its BeforeEach ran after the group's BeforeAll failed")
	}
	if !afterAllRan {
		t.Fatal("the entered group's own AfterAll did not run after its BeforeAll failed (H5)")
	}

	var hookCases, skipped int
	var skipMessage string
	for _, e := range rep.specFinished {
		if e.Hook == hookKindBeforeAll {
			hookCases++
			if !e.Failed {
				t.Fatalf("expected the [BeforeAll] case to be Failed, got %+v", e)
			}
		}
		if e.Skipped {
			skipped++
			skipMessage = e.Message
		}
	}
	if hookCases != 1 {
		t.Fatalf("got %d [BeforeAll] hook cases, want exactly 1 (H4: reported once)", hookCases)
	}
	if skipped != 2 {
		t.Fatalf("got %d skipped specs, want 2 (the direct It and the nested It)", skipped)
	}
	if skipMessage == "" {
		t.Fatal("expected the skipped spec's message to name the failing group, got an empty message")
	}
	if !strings.Contains(skipMessage, "broken") {
		t.Fatalf("expected the skip message to name the failing group %q, got %q", "broken", skipMessage)
	}
}

// TestBeforeAllFailureStopsRemainingBeforeAllsInSameGroup proves H4's "stops the remaining
// BeforeAlls of that group": a second BeforeAll registered after a failing one never runs.
func TestBeforeAllFailureStopsRemainingBeforeAllsInSameGroup(t *testing.T) {
	var secondRan bool
	suite := buildGroupHookSuite("TwoBeforeAlls", func(s *Spec) {
		s.BeforeAll(func(ctx *Context) { ctx.Expect(false).To(BeTrue()) })
		s.BeforeAll(func(*Context) { secondRan = true })
		s.It("spec", func(*Context) {})
	})
	runCompiledSuiteWith(&controlledBackend{}, nil, suite)
	if secondRan {
		t.Fatal("a second BeforeAll ran after the first one in the same group already failed")
	}
}

// TestBeforeAllFailureStopsNestedGroupHooks proves H4's "every nested group's hooks" stop too: a
// nested group's own BeforeAll and AfterAll never run when an ancestor's BeforeAll already failed,
// because the nested group is never entered at all.
func TestBeforeAllFailureStopsNestedGroupHooks(t *testing.T) {
	var nestedBeforeRan, nestedAfterRan bool
	suite := buildGroupHookSuite("Outer", func(s *Spec) {
		s.BeforeAll(func(ctx *Context) { ctx.Expect(false).To(BeTrue()) })
		s.When("inner", func(w *Spec) {
			w.BeforeAll(func(*Context) { nestedBeforeRan = true })
			w.AfterAll(func(*Context) { nestedAfterRan = true })
			w.It("spec", func(*Context) {})
		})
	})
	runCompiledSuiteWith(&controlledBackend{}, nil, suite)
	if nestedBeforeRan || nestedAfterRan {
		t.Fatalf("a nested group's hooks ran after its ancestor's BeforeAll failed: before=%v after=%v",
			nestedBeforeRan, nestedAfterRan)
	}
}

// TestBeforeAllFailureDoesNotAffectSiblingGroup proves H4's "does not affect sibling groups": a
// sibling group's BeforeAll/It/AfterAll all run normally after an earlier sibling's BeforeAll
// failed and that sibling's own AfterAll has run.
func TestBeforeAllFailureDoesNotAffectSiblingGroup(t *testing.T) {
	var order []string
	suite := buildGroupHookSuite("Siblings", func(s *Spec) {
		s.When("broken", func(w *Spec) {
			w.BeforeAll(func(ctx *Context) { ctx.Expect(false).To(BeTrue()) })
			w.AfterAll(func(*Context) { order = append(order, "broken:after") })
			w.It("spec", func(*Context) { order = append(order, "broken:spec") })
		})
		s.When("healthy", func(w *Spec) {
			w.BeforeAll(func(*Context) { order = append(order, "healthy:before") })
			w.AfterAll(func(*Context) { order = append(order, "healthy:after") })
			w.It("spec", func(*Context) { order = append(order, "healthy:spec") })
		})
	})
	runCompiledSuiteWith(&controlledBackend{}, nil, suite)
	want := []string{"broken:after", "healthy:before", "healthy:spec", "healthy:after"}
	assertOrder(t, order, want)
}

// TestBeforeAllPanicRecoveredAsFailure proves a panic in BeforeAll is recovered through the same
// authority every other execution path uses (H7) instead of crashing the process, and is reported
// as the [BeforeAll] case's failure with a stack trace.
func TestBeforeAllPanicRecoveredAsFailure(t *testing.T) {
	suite := buildGroupHookSuite("PanicGroup", func(s *Spec) {
		s.BeforeAll(func(*Context) { panic("boom") })
		s.It("skipped", func(*Context) {})
	})
	rep := &recordingReporter{}
	runCompiledSuiteWith(&controlledBackend{}, rep, suite)

	var found bool
	for _, e := range rep.specFinished {
		if e.Hook == hookKindBeforeAll {
			found = true
			if e.Output == "" {
				t.Fatal("expected the recovered panic's stack trace in Output")
			}
			if !strings.Contains(e.Message, "boom") {
				t.Fatalf("expected the panic message in the hook case's Message, got %q", e.Message)
			}
		}
	}
	if !found {
		t.Fatal("no [BeforeAll] hook case reported for a panicking BeforeAll")
	}
}

// TestAfterAllFailureReportsOneCaseAndKeepsPassingSpecStatus covers H6: an AfterAll failure is
// reported once as an [AfterAll] case, and does not retroactively change the status of a spec that
// already passed.
func TestAfterAllFailureReportsOneCaseAndKeepsPassingSpecStatus(t *testing.T) {
	suite := buildGroupHookSuite("AfterAllFails", func(s *Spec) {
		s.AfterAll(func(ctx *Context) { ctx.Expect(false).To(BeTrue()) })
		s.It("already passed", func(*Context) {})
	})
	rep := &recordingReporter{}
	runCompiledSuiteWith(&controlledBackend{}, rep, suite)

	var passedSpec *report.SpecResultEvent
	var hookCases int
	for i, e := range rep.specFinished {
		if e.Hook == hookKindAfterAll {
			hookCases++
			continue
		}
		passedSpec = &rep.specFinished[i]
	}
	if hookCases != 1 {
		t.Fatalf("got %d [AfterAll] hook cases, want exactly 1", hookCases)
	}
	if passedSpec == nil || passedSpec.Failed {
		t.Fatalf("expected the real spec to keep its passed status, got %+v", passedSpec)
	}
}

// TestAfterAllFailureDoesNotStopRemainingAfterAlls covers H6's "remaining AfterAlls (same group
// and outer) still run": a second AfterAll in the same group, and the outer group's own AfterAll,
// both run after the first AfterAll in an inner group fails.
func TestAfterAllFailureDoesNotStopRemainingAfterAlls(t *testing.T) {
	var order []string
	suite := buildGroupHookSuite("AfterAllChain", func(s *Spec) {
		s.AfterAll(func(*Context) { order = append(order, "outer:after") })
		s.When("inner", func(w *Spec) {
			w.AfterAll(func(ctx *Context) {
				order = append(order, "inner:after1")
				ctx.Expect(false).To(BeTrue())
			})
			w.AfterAll(func(*Context) { order = append(order, "inner:after2") })
			w.It("spec", func(*Context) { order = append(order, "spec") })
		})
	})
	runCompiledSuiteWith(&controlledBackend{}, nil, suite)
	want := []string{"spec", "inner:after1", "inner:after2", "outer:after"}
	assertOrder(t, order, want)
}

// TestGroupHookFailuresCountInTotals proves BeforeAll/AfterAll failures reach
// report.Collector's totals like any other failed case (H8), end to end through the exact event
// shapes DescribeWithReporter/CompiledSuite.Run emit (SuiteStarted/SuiteFinished built by hand
// here since runCompiledSuiteWith itself only emits spec-level events).
func TestGroupHookFailuresCountInTotals(t *testing.T) {
	suite := buildGroupHookSuite("TotalsCheck", func(s *Spec) {
		s.When("broken", func(w *Spec) {
			w.BeforeAll(func(ctx *Context) { ctx.Expect(false).To(BeTrue()) })
			w.It("skipped", func(*Context) {})
		})
	})
	c := report.NewCollector()
	c.SuiteStarted(report.SuiteStartEvent{Name: "TotalsCheck"})
	runCompiledSuiteWith(&controlledBackend{}, c, suite)
	c.SuiteFinished(report.SuiteEndEvent{Name: "TotalsCheck"})
	got := c.Report()

	if len(got.Suites) != 1 {
		t.Fatalf("expected one suite, got %+v", got.Suites)
	}
	totals := got.Suites[0].Totals
	// One failed case (the synthetic [BeforeAll]) and one skipped case (the suppressed spec).
	if totals.Failed != 1 || totals.Skipped != 1 {
		t.Fatalf("suite totals = %+v, want Failed=1 Skipped=1", totals)
	}
	var sawHookCase bool
	for _, cs := range got.Suites[0].Cases {
		if cs.Hook == hookKindBeforeAll.String() {
			sawHookCase = true
			if cs.Status != report.StatusFailed {
				t.Fatalf("hook case status = %v, want %v", cs.Status, report.StatusFailed)
			}
		}
	}
	if !sawHookCase {
		t.Fatal("no case in the normalized report carried Hook=\"BeforeAll\"")
	}
}

// TestBeforeAllFailNowGoexitDoesNotAbortSiblingGroupRealProcess proves a real t.Fatal/FailNow
// inside a BeforeAll (runtime.Goexit) is isolated to that one hook's own subtest and does not abort
// a sibling group (H7) — against a genuine *testing.T, in a subprocess, matching the pattern
// TestSpecRunRealFatalfIsolatesJustThatSpecRealProcess already established in
// execution_plan_isolation_test.go: a nested t.Run's real Fatalf would otherwise mark this outer
// test failed itself, making "did isolation work" indistinguishable from "did this test fail for an
// unrelated reason."
func TestBeforeAllFailNowGoexitDoesNotAbortSiblingGroupRealProcess(t *testing.T) {
	if os.Getenv("GO_SPECS_BEFOREALL_GOEXIT_ISOLATION_HELPER") == "1" {
		Describe(t, "GoexitSiblings", func(s *Spec) {
			s.When("broken", func(w *Spec) {
				w.BeforeAll(func(ctx *Context) { ctx.T.Fatal("boom") })
				w.It("skipped", func(*Context) {})
			})
			s.When("healthy", func(w *Spec) {
				w.It("runs", func(*Context) { println("healthy spec ran") })
			})
		})
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestBeforeAllFailNowGoexitDoesNotAbortSiblingGroupRealProcess$", "-test.v")
	cmd.Env = append(os.Environ(), "GO_SPECS_BEFOREALL_GOEXIT_ISOLATION_HELPER=1")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected the failing BeforeAll to fail the process, but it passed: %s", output)
	}
	if !strings.Contains(string(output), "healthy spec ran") {
		t.Fatalf("expected the sibling group's spec to still run after a Fatal-triggered Goexit in another group's BeforeAll, got: %s", output)
	}
}
