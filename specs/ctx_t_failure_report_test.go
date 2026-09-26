package specs

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// ctxTFailureMechanisms is one spec per way a spec body can fail its own subtest. Most fail it
// through ctx.T directly, which never touches the Context's own failure record (#253).
//
// The two cleanup entries use the idiomatic form, reading ctx.T and ctx inside the cleanup rather
// than capturing them first. testing runs a subtest's Cleanup functions only after the subtest's own
// function has returned, so these pin that the spec's Context stays bound to its own subtest until
// t.Run, cleanups included, has finished: ctx.T must still be this spec's subtest, not the parent,
// and ctx.Expect must still fail through this spec's own backend, not one already handed back to the
// pool.
var ctxTFailureMechanisms = []struct {
	name string
	body func(*Context)
}{
	{"errors-via-ctx-T", func(ctx *Context) { ctx.T.Error("boom") }},
	{"fatals-via-ctx-T", func(ctx *Context) { ctx.T.Fatal("boom") }},
	{"fails-via-ctx-T", func(ctx *Context) { ctx.T.Fail() }},
	{"failnow-via-ctx-T", func(ctx *Context) { ctx.T.FailNow() }},
	{"errors-from-ctx-T-cleanup", func(ctx *Context) {
		ctx.T.Cleanup(func() { ctx.T.Error("boom from cleanup") })
	}},
	{"fails-via-expect-in-cleanup", func(ctx *Context) {
		ctx.T.Cleanup(func() { ctx.Expect(1).ToEqual(2) })
	}},
	{"fails-via-expect", func(ctx *Context) { ctx.Expect(1).ToEqual(2) }},
	{"passes", func(*Context) {}},
}

// wantCtxTFailureReport is what every engine must report for ctxTFailureMechanisms: every spec whose
// subtest failed is Failed and counted in FailedSpecs, whatever the failure mechanism.
var wantCtxTFailureReport = []string{
	"SPEC_FINISHED name=errors-via-ctx-T failed=true",
	"SPEC_FINISHED name=fatals-via-ctx-T failed=true",
	"SPEC_FINISHED name=fails-via-ctx-T failed=true",
	"SPEC_FINISHED name=failnow-via-ctx-T failed=true",
	"SPEC_FINISHED name=errors-from-ctx-T-cleanup failed=true",
	"SPEC_FINISHED name=fails-via-expect-in-cleanup failed=true",
	"SPEC_FINISHED name=fails-via-expect failed=true",
	"SPEC_FINISHED name=passes failed=false",
	"SUITE_FINISHED total=8 failed=7",
}

func printCtxTFailureReport(rep *recordingReporter) {
	for _, e := range rep.specFinished {
		fmt.Printf("SPEC_FINISHED name=%s failed=%t\n", e.Name, e.Failed)
	}
	for _, e := range rep.suiteFinished {
		fmt.Printf("SUITE_FINISHED total=%d failed=%d\n", e.TotalSpecs, e.FailedSpecs)
	}
}

// TestCtxTFailuresAreReportedAsFailedRealProcess proves #253: a spec that fails only through ctx.T
// (Error, Fatal, Fail, FailNow, or from a Cleanup) is reported Failed and counted in FailedSpecs, on
// every engine that hands the body a live subtest *testing.T. Before the fix, each of these engines
// derived Failed from the Context alone, or read the subtest's state before its cleanups had run, so
// the subtest printed --- FAIL while the reporter said passed.
//
// A subprocess, because the specs' real subtest failures would otherwise fail this test itself.
func TestCtxTFailuresAreReportedAsFailedRealProcess(t *testing.T) {
	const helperEnv = "GO_SPECS_CTX_T_FAILURE_REPORT_HELPER"
	engines := map[string]func(t *testing.T, rep *recordingReporter){
		"describe": func(t *testing.T, rep *recordingReporter) {
			DescribeWithReporter(t, "suite", rep, func(s *Spec) {
				for _, m := range ctxTFailureMechanisms {
					s.It(m.name, m.body)
				}
			})
		},
		// A BeforeAll routes the suite through the group engine (runPlanWithGroups), whose runSpec
		// derives the result separately from the flat path's runExecution.
		"describe-with-before-all": func(t *testing.T, rep *recordingReporter) {
			DescribeWithReporter(t, "suite", rep, func(s *Spec) {
				s.BeforeAll(func(*Context) {})
				for _, m := range ctxTFailureMechanisms {
					s.It(m.name, m.body)
				}
			})
		},
		"runner": func(t *testing.T, rep *recordingReporter) {
			g := group{}
			for _, m := range ctxTFailureMechanisms {
				g.specs = append(g.specs, m.body)
				g.names = append(g.names, m.name)
			}
			NewRunnerWithReporter(&Program{Groups: []group{g}}, "suite", rep).Run(t)
		},
		// ItParallel runs every spec of the range concurrently, each on its own goroutine and its own
		// pooled Context, and reports them afterwards in declaration order.
		"it-parallel": func(t *testing.T, rep *recordingReporter) {
			DescribeWithReporter(t, "suite", rep, func(s *Spec) {
				for _, m := range ctxTFailureMechanisms {
					s.ItParallel(m.name, m.body)
				}
			})
		},
	}

	if engine := os.Getenv(helperEnv); engine != "" {
		var rep recordingReporter
		engines[engine](t, &rep)
		printCtxTFailureReport(&rep)
		return
	}

	for engine := range engines {
		t.Run(engine, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestCtxTFailuresAreReportedAsFailedRealProcess$")
			cmd.Env = append(os.Environ(), helperEnv+"="+engine)
			output, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("expected the failing specs to fail the process, but it passed: %s", output)
			}
			var got []string
			for _, line := range strings.Split(string(output), "\n") {
				if strings.HasPrefix(line, "SPEC_FINISHED ") || strings.HasPrefix(line, "SUITE_FINISHED ") {
					got = append(got, line)
				}
			}
			if strings.Join(got, "\n") != strings.Join(wantCtxTFailureReport, "\n") {
				t.Fatalf("reported results:\n%s\n\nwant:\n%s\n\nfull output:\n%s",
					strings.Join(got, "\n"), strings.Join(wantCtxTFailureReport, "\n"), output)
			}
		})
	}
}

// TestRunnerFailFastStopsOnCtxTFailureRealProcess pins the behaviour change #253 brought to Runner:
// FailFast reads the same outcome the reporter does, so a spec that fails only through ctx.T now
// stops the run. Before, the Context never saw that failure and the next spec still ran.
//
// ctx.T.Error rather than Fatal, so the spec body itself runs to completion and only FailFast can be
// what stops the next spec.
func TestRunnerFailFastStopsOnCtxTFailureRealProcess(t *testing.T) {
	if os.Getenv("GO_SPECS_CTX_T_FAIL_FAST_HELPER") == "1" {
		prog := &Program{
			Groups: []group{{
				specs: []step{
					func(ctx *Context) { ctx.T.Error("boom"); fmt.Println("spec1 ran") },
					func(*Context) { fmt.Println("spec2 ran") },
				},
				names: []string{"spec one", "spec two"},
			}},
		}
		r := NewRunner(prog)
		r.FailFast = true
		r.Run(t)
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestRunnerFailFastStopsOnCtxTFailureRealProcess$")
	cmd.Env = append(os.Environ(), "GO_SPECS_CTX_T_FAIL_FAST_HELPER=1")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected the failing spec to fail the process, but it passed: %s", output)
	}
	if !strings.Contains(string(output), "spec1 ran") {
		t.Fatalf("expected spec one to run, got: %s", output)
	}
	if strings.Contains(string(output), "spec2 ran") {
		t.Fatalf("expected FailFast to stop the run after spec one's ctx.T failure, but spec two ran: %s", output)
	}
}
