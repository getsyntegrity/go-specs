package specs

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/getsyntegrity/go-specs/report"
)

// ctxTSkipMechanisms is one spec per way a spec body can skip its own subtest at runtime (#254),
// plus a failure-then-skip case and an ordinary passing spec as controls.
//
// fails-then-skips must stay Failed, not Skipped, matching go test itself: a test that already
// called Error and then calls SkipNow still reports FAIL, not SKIP (see tRunner in the testing
// package). ctx.T.Error is used rather than Fatal so the SkipNow line is actually reached; Fatal
// would already have ended the goroutine with runtime.Goexit before it.
var ctxTSkipMechanisms = []struct {
	name string
	body func(*Context)
}{
	{"skips-via-ctx-T", func(ctx *Context) { ctx.T.Skip("runtime skip") }},
	{"skipf-via-ctx-T", func(ctx *Context) { ctx.T.Skipf("runtime skip %d", 1) }},
	{"skipnow-via-ctx-T", func(ctx *Context) { ctx.T.SkipNow() }},
	{"fails-then-skips", func(ctx *Context) { ctx.T.Error("boom"); ctx.T.SkipNow() }},
	{"passes", func(*Context) {}},
}

// wantCtxTSkipReport is what every engine must report for ctxTSkipMechanisms: a spec whose own
// subtest skipped at runtime is Skipped and counted in SkippedSpecs, unless it also failed, in
// which case it stays Failed and is not counted as Skipped at all (#254).
var wantCtxTSkipReport = []string{
	"SPEC_FINISHED name=skips-via-ctx-T failed=false skipped=true",
	"SPEC_FINISHED name=skipf-via-ctx-T failed=false skipped=true",
	"SPEC_FINISHED name=skipnow-via-ctx-T failed=false skipped=true",
	"SPEC_FINISHED name=fails-then-skips failed=true skipped=false",
	"SPEC_FINISHED name=passes failed=false skipped=false",
	"SUITE_FINISHED total=5 failed=1 skipped=3",
}

func printCtxTSkipReport(rep *recordingReporter) {
	for _, e := range rep.specFinished {
		fmt.Printf("SPEC_FINISHED name=%s failed=%t skipped=%t\n", e.Name, e.Failed, e.Skipped)
	}
	for _, e := range rep.suiteFinished {
		fmt.Printf("SUITE_FINISHED total=%d failed=%d skipped=%d\n", e.TotalSpecs, e.FailedSpecs, e.SkippedSpecs)
	}
}

// TestCtxTSkipsAreReportedAsSkippedRealProcess proves #254: a spec whose own subtest skips at
// runtime (ctx.T.Skip, Skipf or SkipNow) is reported Skipped and counted in SkippedSpecs, on every
// engine that hands the body a live subtest *testing.T — the same four engines #253 covers for
// failures. Before the fix, each of these engines only ever set Failed from the Context/subtest;
// nothing read the subtest's own Skipped(), so a runtime skip was reported as an ordinary pass.
//
// A subprocess, because fails-then-skips' real subtest failure would otherwise fail this test
// itself, and a skip-only subtest would otherwise print --- SKIP inside this test's own output.
func TestCtxTSkipsAreReportedAsSkippedRealProcess(t *testing.T) {
	const helperEnv = "GO_SPECS_CTX_T_SKIP_REPORT_HELPER"
	engines := map[string]func(t *testing.T, rep *recordingReporter){
		"describe": func(t *testing.T, rep *recordingReporter) {
			DescribeWithReporter(t, "suite", rep, func(s *Spec) {
				for _, m := range ctxTSkipMechanisms {
					s.It(m.name, m.body)
				}
			})
		},
		// A BeforeAll routes the suite through the group engine (runPlanWithGroups), whose runSpec
		// derives the result separately from the flat path's runExecution.
		"describe-with-before-all": func(t *testing.T, rep *recordingReporter) {
			DescribeWithReporter(t, "suite", rep, func(s *Spec) {
				s.BeforeAll(func(*Context) {})
				for _, m := range ctxTSkipMechanisms {
					s.It(m.name, m.body)
				}
			})
		},
		"runner": func(t *testing.T, rep *recordingReporter) {
			g := group{}
			for _, m := range ctxTSkipMechanisms {
				g.specs = append(g.specs, m.body)
				g.names = append(g.names, m.name)
			}
			NewRunnerWithReporter(&Program{Groups: []group{g}}, "suite", rep).Run(t)
		},
		// ItParallel runs every spec of the range concurrently, each on its own goroutine and its own
		// pooled Context, and reports them afterwards in declaration order.
		"it-parallel": func(t *testing.T, rep *recordingReporter) {
			DescribeWithReporter(t, "suite", rep, func(s *Spec) {
				for _, m := range ctxTSkipMechanisms {
					s.ItParallel(m.name, m.body)
				}
			})
		},
	}

	if engine := os.Getenv(helperEnv); engine != "" {
		var rep recordingReporter
		engines[engine](t, &rep)
		printCtxTSkipReport(&rep)
		return
	}

	for engine := range engines {
		t.Run(engine, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestCtxTSkipsAreReportedAsSkippedRealProcess$")
			cmd.Env = append(os.Environ(), helperEnv+"="+engine)
			output, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("expected the failing spec to fail the process, but it passed: %s", output)
			}
			var got []string
			for _, line := range strings.Split(string(output), "\n") {
				if strings.HasPrefix(line, "SPEC_FINISHED ") || strings.HasPrefix(line, "SUITE_FINISHED ") {
					got = append(got, line)
				}
			}
			if strings.Join(got, "\n") != strings.Join(wantCtxTSkipReport, "\n") {
				t.Fatalf("reported results:\n%s\n\nwant:\n%s\n\nfull output:\n%s",
					strings.Join(got, "\n"), strings.Join(wantCtxTSkipReport, "\n"), output)
			}
		})
	}
}

// TestRunnerFailFastDoesNotStopOnCtxTSkip pins that Runner's FailFast does not stop the run after a
// spec that only skipped, mirroring TestRunnerFailFastStopsOnCtxTFailureRealProcess's check that it
// does stop after a ctx.T failure (#253). A skip is not a failure (#254): the same outcome FailFast
// reads stays false for it, so the next spec must still run.
//
// No subprocess needed here, unlike the mixed suite above: a plain SkipNow ends only its own
// subtest's goroutine (runtime.Goexit) and never marks the outer test failed, so this test can run
// in-process and just check that spec two ran.
func TestRunnerFailFastDoesNotStopOnCtxTSkip(t *testing.T) {
	var spec2Ran bool
	prog := &Program{
		Groups: []group{{
			specs: []step{
				func(ctx *Context) { ctx.T.SkipNow() },
				func(*Context) { spec2Ran = true },
			},
			names: []string{"spec one", "spec two"},
		}},
	}
	r := NewRunner(prog)
	r.FailFast = true
	r.Run(t)

	if !spec2Ran {
		t.Fatal("expected FailFast not to stop the run after spec one only skipped, but spec two did not run")
	}
}

// assertOnlySkipped fails t unless e reports exactly Skipped: true and nothing else in
// Skipped/Failed/Filtered/Pending — the "not failed, not passed, not filtered, not pending" shape a
// runtime-skipped spec must have (#254).
func assertOnlySkipped(t *testing.T, label string, e report.SpecResultEvent) {
	t.Helper()
	if !e.Skipped || e.Failed || e.Filtered || e.Pending {
		t.Errorf("%s: got Skipped=%v Failed=%v Filtered=%v Pending=%v, want only Skipped",
			label, e.Skipped, e.Failed, e.Filtered, e.Pending)
	}
}

// TestBeforeEachSkipReportsSpecSkippedAndStillRunsAfterEach pins that ctx.T.Skip/Skipf/SkipNow
// inside a BeforeEach is folded into the spec's own outcome exactly like a skip in the spec body
// itself (#254): the spec is reported Skipped (not failed, not passed) and counted in SkippedSpecs,
// and AfterEach still runs.
//
// No new mechanism is needed for AfterEach to run: BeforeEach/body/AfterEach are one instruction
// stream inside the spec's own subtest (runProgram, execution_plan.go; runSpecWithHooks, runner.go),
// and a SkipNow ends that subtest's goroutine with runtime.Goexit, which runs every deferred call on
// its way out — the same guarantee that already makes AfterEach run after a Fatal/FailNow in a
// BeforeEach (see runSpecWithHooks' and runProgram's doc comments). This test exists to pin that
// guarantee explicitly for Skip, on every engine that has a BeforeEach.
//
// No subprocess needed: a BeforeEach skip never fails the process, unlike ctxTSkipMechanisms'
// fails-then-skips case above.
func TestBeforeEachSkipReportsSpecSkippedAndStillRunsAfterEach(t *testing.T) {
	t.Run("describe", func(t *testing.T) {
		rep := &recordingReporter{}
		var afterEachRan bool
		DescribeWithReporter(t, "suite", rep, func(s *Spec) {
			s.BeforeEach(func(ctx *Context) { ctx.T.Skip("no fixture") })
			s.AfterEach(func(*Context) { afterEachRan = true })
			s.It("spec", func(*Context) {})
		})
		if len(rep.specFinished) != 1 {
			t.Fatalf("expected exactly one SpecFinished event, got %d: %+v", len(rep.specFinished), rep.specFinished)
		}
		assertOnlySkipped(t, "spec", rep.specFinished[0])
		if !afterEachRan {
			t.Error("AfterEach did not run after BeforeEach's ctx.T.Skip")
		}
		if len(rep.suiteFinished) != 1 {
			t.Fatalf("expected exactly one SuiteFinished event, got %d", len(rep.suiteFinished))
		}
		if end := rep.suiteFinished[0]; end.SkippedSpecs != 1 || end.FailedSpecs != 0 || end.TotalSpecs != 1 {
			t.Errorf("suite totals = %+v, want TotalSpecs=1 SkippedSpecs=1 FailedSpecs=0", end)
		}
	})

	// A BeforeAll alongside the spec's own BeforeEach routes the suite through the group engine
	// (runPlanWithGroups / group_hooks.go's runSpec), whose isolation is separate from the flat
	// path's runExecution.
	t.Run("describe-with-before-all", func(t *testing.T) {
		rep := &recordingReporter{}
		var afterEachRan bool
		DescribeWithReporter(t, "suite", rep, func(s *Spec) {
			s.BeforeAll(func(*Context) {})
			s.BeforeEach(func(ctx *Context) { ctx.T.Skipf("no fixture: %d", 1) })
			s.AfterEach(func(*Context) { afterEachRan = true })
			s.It("spec", func(*Context) {})
		})
		if len(rep.specFinished) != 1 {
			t.Fatalf("expected exactly one SpecFinished event, got %d: %+v", len(rep.specFinished), rep.specFinished)
		}
		assertOnlySkipped(t, "spec", rep.specFinished[0])
		if !afterEachRan {
			t.Error("AfterEach did not run after BeforeEach's ctx.T.Skipf")
		}
		if len(rep.suiteFinished) != 1 {
			t.Fatalf("expected exactly one SuiteFinished event, got %d", len(rep.suiteFinished))
		}
		if end := rep.suiteFinished[0]; end.SkippedSpecs != 1 || end.FailedSpecs != 0 || end.TotalSpecs != 1 {
			t.Errorf("suite totals = %+v, want TotalSpecs=1 SkippedSpecs=1 FailedSpecs=0", end)
		}
	})

	// The raw Runner/Builder engine's per-group before/after (group.before/group.after) is the
	// BeforeEach/AfterEach equivalent, run per spec via runSpecWithHooks (runner.go).
	t.Run("runner", func(t *testing.T) {
		rep := &recordingReporter{}
		var afterEachRan bool
		g := group{
			before: []step{func(ctx *Context) { ctx.T.SkipNow() }},
			after:  []step{func(*Context) { afterEachRan = true }},
			specs:  []step{func(*Context) {}},
			names:  []string{"spec"},
		}
		NewRunnerWithReporter(&Program{Groups: []group{g}}, "suite", rep).Run(t)
		if len(rep.specFinished) != 1 {
			t.Fatalf("expected exactly one SpecFinished event, got %d: %+v", len(rep.specFinished), rep.specFinished)
		}
		assertOnlySkipped(t, "spec", rep.specFinished[0])
		if !afterEachRan {
			t.Error("the group's after hook did not run after before's ctx.T.SkipNow")
		}
		if len(rep.suiteFinished) != 1 {
			t.Fatalf("expected exactly one SuiteFinished event, got %d", len(rep.suiteFinished))
		}
		if end := rep.suiteFinished[0]; end.SkippedSpecs != 1 || end.FailedSpecs != 0 || end.TotalSpecs != 1 {
			t.Errorf("suite totals = %+v, want TotalSpecs=1 SkippedSpecs=1 FailedSpecs=0", end)
		}
	})

	// ItParallel compiles to the exact same before/body/after instruction stream as an ordinary It
	// (compiler.go's EmitItParallel), just launched on its own goroutine (group_hooks.go's
	// runParallelSpec) — so a BeforeEach declared on the enclosing scope applies to it too.
	t.Run("it-parallel", func(t *testing.T) {
		rep := &recordingReporter{}
		var afterEachRan bool
		DescribeWithReporter(t, "suite", rep, func(s *Spec) {
			s.BeforeEach(func(ctx *Context) { ctx.T.Skip("no fixture") })
			s.AfterEach(func(*Context) { afterEachRan = true })
			s.ItParallel("spec", func(*Context) {})
		})
		if len(rep.specFinished) != 1 {
			t.Fatalf("expected exactly one SpecFinished event, got %d: %+v", len(rep.specFinished), rep.specFinished)
		}
		assertOnlySkipped(t, "spec", rep.specFinished[0])
		if !afterEachRan {
			t.Error("AfterEach did not run after BeforeEach's ctx.T.Skip")
		}
		if len(rep.suiteFinished) != 1 {
			t.Fatalf("expected exactly one SuiteFinished event, got %d", len(rep.suiteFinished))
		}
		if end := rep.suiteFinished[0]; end.SkippedSpecs != 1 || end.FailedSpecs != 0 || end.TotalSpecs != 1 {
			t.Errorf("suite totals = %+v, want TotalSpecs=1 SkippedSpecs=1 FailedSpecs=0", end)
		}
	})
}

// TestBeforeAllSkipNowReportsEveryGroupSpecSkippedAndRunsAfterAll pins docs/SUITE_HOOKS_CONTRACT.md
// H5's "ctx.T.SkipNow() in a BeforeAll" paragraph on the reporter side: every spec of the group,
// including a nested group's specs, is reported SpecStarted+SpecFinished{Skipped: true} — never
// passed, never dropped from the report — counted in SkippedSpecs and not in FailedSpecs; no
// synthetic [BeforeAll] case is emitted (a skip is not a failure); the group's AfterAll still runs;
// and a sibling spec/group outside the skipped one is unaffected.
//
// This is pre-existing #207 H4/H5 behavior (group_hooks.go's runBeforeAlls, the `case !returned:`
// branch, and reportGroupSuppressedSpec), already pinned end-to-end against a real process by
// TestSkipNowInABeforeAllSkipsTheGroup (before_after_all_scope_test.go). This test adds the
// SpecResultEvent-shape and SuiteEndEvent-totals assertions that test's printingReporter does not
// make (its SuiteFinished is a no-op), using recordingReporter instead, without a subprocess: a
// BeforeAll skip never fails `go test`.
func TestBeforeAllSkipNowReportsEveryGroupSpecSkippedAndRunsAfterAll(t *testing.T) {
	rep := &recordingReporter{}
	var afterAllRan bool
	DescribeWithReporter(t, "suite", rep, func(s *Spec) {
		s.When("skipped", func(w *Spec) {
			w.BeforeAll(func(ctx *Context) { ctx.T.SkipNow() })
			w.AfterAll(func(*Context) { afterAllRan = true })
			w.It("first", func(*Context) { t.Error("must not run: BeforeAll skipped the group") })
			w.When("nested", func(n *Spec) {
				n.It("second", func(*Context) { t.Error("must not run: an ancestor BeforeAll skipped the group") })
			})
		})
		s.It("sibling", func(*Context) {})
	})

	if !afterAllRan {
		t.Error("AfterAll did not run after a BeforeAll ctx.T.SkipNow (docs/SUITE_HOOKS_CONTRACT.md H5)")
	}

	byName := map[string]report.SpecResultEvent{}
	for _, e := range rep.specFinished {
		byName[e.Name] = e
	}

	for _, name := range []string{"first", "second"} {
		e, ok := byName[name]
		if !ok {
			t.Fatalf("missing SpecStarted/SpecFinished for %q: a BeforeAll skip must not drop specs from the report", name)
		}
		assertOnlySkipped(t, name, e)
		if !strings.Contains(e.Message, "BeforeAll skipped group") {
			t.Errorf("%s: Message = %q, want it naming the skipped group (H5)", name, e.Message)
		}
	}

	sibling, ok := byName["sibling"]
	if !ok {
		t.Fatal("missing SpecFinished for the sibling spec outside the skipped group")
	}
	if sibling.Failed || sibling.Skipped || sibling.Filtered || sibling.Pending {
		t.Errorf("sibling spec outside the skipped group must be an ordinary pass, got %+v", sibling)
	}

	for _, e := range rep.specFinished {
		if e.Hook != report.HookNone {
			t.Errorf("no synthetic hook case should be emitted for a BeforeAll skip (H5, unlike H4's failure case): %+v", e)
		}
	}

	if len(rep.suiteFinished) != 1 {
		t.Fatalf("expected exactly one SuiteFinished event, got %d", len(rep.suiteFinished))
	}
	end := rep.suiteFinished[0]
	if end.SkippedSpecs != 2 {
		t.Errorf("SkippedSpecs = %d, want 2 (first and second)", end.SkippedSpecs)
	}
	if end.FailedSpecs != 0 {
		t.Errorf("FailedSpecs = %d, want 0: a BeforeAll skip is not a failure", end.FailedSpecs)
	}
	if end.TotalSpecs != 3 {
		t.Errorf("TotalSpecs = %d, want 3 (first, second, sibling)", end.TotalSpecs)
	}
}
