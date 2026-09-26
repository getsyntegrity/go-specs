package specs

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
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
