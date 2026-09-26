package specs

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// ctxTFailureMechanisms is one spec per way a sequential spec body can fail its own subtest. Only
// the last failing entry goes through ctx.Expect; the others fail the subtest through ctx.T
// directly, which never touches the Context's own failure record (#253).
var ctxTFailureMechanisms = []struct {
	name string
	body func(*Context)
}{
	{"errors-via-ctx-T", func(ctx *Context) { ctx.T.Error("boom") }},
	{"fatals-via-ctx-T", func(ctx *Context) { ctx.T.Fatal("boom") }},
	{"fails-via-ctx-T", func(ctx *Context) { ctx.T.Fail() }},
	{"failnow-via-ctx-T", func(ctx *Context) { ctx.T.FailNow() }},
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
	"SPEC_FINISHED name=fails-via-expect failed=true",
	"SPEC_FINISHED name=passes failed=false",
	"SUITE_FINISHED total=6 failed=5",
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
// (Error, Fatal, Fail, FailNow) is reported Failed and counted in FailedSpecs, on every sequential
// engine that hands the body a live subtest *testing.T. Before the fix, each of these engines derived
// Failed from the Context alone, so the subtest printed --- FAIL while the reporter said passed.
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
