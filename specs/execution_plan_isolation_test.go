package specs

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestSpecRunRealFatalfIsolatesJustThatSpecRealProcess proves #74's fix end-to-end for the
// Spec/ExecutionPlan execution model, mirroring TestRunnerRunRealFatalfIsolatesJustThatSpecRealProcess
// in runner_recovery_test.go for Runner/Program: a real assertion failure (EqualTo, which calls
// Fatalf under the hood) inside one spec, against a genuine *testing.T, no longer terminates the
// whole Describe call. Each spec's program now runs in its own subtest when possible (see
// runSpecProgram in execution_plan.go), so Goexit only unwinds that subtest's goroutine — the
// remaining specs still run, and every spec, including the failing one, still gets a SpecFinished
// event. Same subprocess pattern as the Runner test: a nested t.Run's real Fatalf would otherwise
// mark this outer test failed itself, which would make "did isolation work" indistinguishable from
// "did this test fail for an unrelated reason."
func TestSpecRunRealFatalfIsolatesJustThatSpecRealProcess(t *testing.T) {
	if os.Getenv("GO_SPECS_EXECPLAN_FATALF_ISOLATION_HELPER") == "1" {
		var rep recordingReporter
		DescribeWithReporter(t, "suite", &rep, func(s *Spec) {
			s.It("spec one", func(ctx *Context) { EqualTo(ctx, 1, 2) })
			s.It("spec two", func(*Context) { fmt.Println("spec2 ran") })
			s.It("spec three", func(*Context) { fmt.Println("spec3 ran") })
		})
		fmt.Printf("suite finished total=%d failed=%d\n", rep.suiteFinished[0].TotalSpecs, rep.suiteFinished[0].FailedSpecs)
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestSpecRunRealFatalfIsolatesJustThatSpecRealProcess$")
	cmd.Env = append(os.Environ(), "GO_SPECS_EXECPLAN_FATALF_ISOLATION_HELPER=1")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected the failing spec to fail the process, but it passed: %s", output)
	}
	if !strings.Contains(string(output), "spec2 ran") || !strings.Contains(string(output), "spec3 ran") {
		t.Fatalf("expected specs 2 and 3 to still run after spec 1's real Fatal, got: %s", output)
	}
	if !strings.Contains(string(output), "suite finished total=3 failed=1") {
		t.Fatalf("expected SuiteFinished to report total=3 failed=1 (every spec still reported), got: %s", output)
	}
}

// TestSpecRunSpecNamesWithSpacesAndSlashesReportedVerbatim proves a real *testing.T subtest per spec
// (used for isolation, see runSpecProgram) never leaks into reported spec identity for the
// Spec/ExecutionPlan execution model, mirroring TestRunnerRunSpecNamesWithSpacesAndSlashesReportedVerbatim
// for Runner/Program: Go's t.Run sanitizes spaces to "_" and treats "/" as a nested-subtest boundary
// for -v/-run/test2json presentation, but SpecStartEvent/SpecResultEvent.Name always comes straight
// from the plan's own Names, so none of that shows up in the reported event. No subprocess needed:
// neither spec fails.
func TestSpecRunSpecNamesWithSpacesAndSlashesReportedVerbatim(t *testing.T) {
	var rep recordingReporter
	DescribeWithReporter(t, "suite", &rep, func(s *Spec) {
		s.It("spec with spaces", func(*Context) {})
		s.It("spec/with/slashes", func(*Context) {})
	})

	if len(rep.specStarted) != 2 {
		t.Fatalf("expected 2 SpecStarted events, got %d", len(rep.specStarted))
	}
	if got := rep.specStarted[0].Name; got != "spec with spaces" {
		t.Fatalf("expected reported name %q, got %q", "spec with spaces", got)
	}
	if got := rep.specStarted[1].Name; got != "spec/with/slashes" {
		t.Fatalf("expected reported name %q, got %q", "spec/with/slashes", got)
	}
}
