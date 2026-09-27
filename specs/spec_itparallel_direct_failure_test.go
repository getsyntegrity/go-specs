// spec_itparallel_direct_failure_test.go pins Fix 1 for Spec.ItParallel (issue #245, Codex review
// P1 "Include direct testing.T failures in parallel outcomes"): an ItParallel body that fails only
// through the real *testing.T (ctx.T.Error/Errorf/Fail, or ctx.T.Fatal/FailNow) must still be
// reported Failed by the reporter and counted in SuiteFinished.FailedSpecs, exactly as it already
// fails the underlying Go subtest. Before the fix, runParallelSpec derived Failed only from
// ctx.hasFailed() (specs/group_hooks.go), which a direct ctx.T call never sets, so the reporter and
// `go test`/the subtest itself disagreed about whether the spec passed. A failing run can only be
// observed from outside the failing process (the direct failure also fails this outer test if run
// in-process), so the suite runs as a real subprocess, reusing runParallelHelper/parallelHelperEnv
// from spec_body_parallel_test.go.
package specs

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const itParallelDirectFailureTest = "TestSpecItParallel_DirectTestingTFailuresCountInOutcomes"

func TestSpecItParallel_DirectTestingTFailuresCountInOutcomes(t *testing.T) {
	if os.Getenv(parallelHelperEnv) == "1" {
		var rep recordingReporter
		DescribeWithReporter(t, "suite", &rep, func(s *Spec) {
			s.ItParallel("alpha-passes", func(*Context) {})
			s.ItParallel("bravo-errors", func(ctx *Context) { ctx.T.Error("bravo direct error") })
			s.ItParallel("charlie-failnow", func(ctx *Context) { ctx.T.FailNow() })
		})
		for _, e := range rep.specFinished {
			fmt.Printf("SPEC_FINISHED name=%s failed=%v\n", e.Name, e.Failed)
		}
		fmt.Printf("SUITE_FINISHED failed=%d\n", rep.suiteFinished[0].FailedSpecs)
		return
	}

	output, failed := runParallelHelper(t, itParallelDirectFailureTest)
	if !failed {
		t.Fatalf("expected ctx.T.Error/ctx.T.FailNow to fail the process, got a passing run:\n%s", output)
	}
	for _, want := range []struct {
		name   string
		failed bool
	}{
		{"alpha-passes", false},
		{"bravo-errors", true},
		{"charlie-failnow", true},
	} {
		re := regexp.MustCompile(`(?m)^SPEC_FINISHED name=` + want.name + ` failed=` + strconv.FormatBool(want.failed) + `$`)
		if !re.MatchString(output) {
			t.Errorf("expected SpecFinished for %q to report Failed=%v:\n%s", want.name, want.failed, output)
		}
	}
	if !strings.Contains(output, "SUITE_FINISHED failed=2") {
		t.Errorf("expected SuiteFinished.FailedSpecs=2 (bravo-errors, charlie-failnow):\n%s", output)
	}
}
