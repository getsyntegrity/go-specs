// builder_itparallel_subtest_report_test.go pins where Builder.ItParallel reports a failing spec's
// message (#330 follow-up). Each spec runs in its own subtest on a real *testing.T, so the message
// belongs to that subtest: `go test -v` must show the text under the failing `--- FAIL` line and not
// replay it on the parent as "spec[N]: ...". On the *testing.B shape (no subtest) the replay through
// the parent backend stays, since there is nowhere else to put it.
package specs

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

const itParallelSubtestReportEnv = "GO_SPECS_ITPARALLEL_SUBTEST_REPORT_HELPER"

func TestBuilderItParallelReportsFailureOnSubtestNotParent(t *testing.T) {
	if os.Getenv(itParallelSubtestReportEnv) == "1" {
		b := NewBuilder()
		b.Describe("D", func() {
			b.ItParallel("par-assert", func(c *Context) { c.Expect(1).To(Equal(2)) })
			b.ItParallel("par-ok", func(c *Context) {})
		})
		NewRunner(b.Build()).Run(t)
		return
	}

	cmd := exec.Command(os.Args[0], "-test.v=true",
		"-test.run=^TestBuilderItParallelReportsFailureOnSubtestNotParent$")
	cmd.Env = append(os.Environ(), itParallelSubtestReportEnv+"=1")
	out, err := cmd.CombinedOutput()
	output := string(out)
	if err == nil {
		t.Fatalf("expected the helper process to fail, got success:\n%s", output)
	}
	if strings.Contains(output, "spec[") {
		t.Errorf("the failure was replayed on the parent as spec[N]; want it only on the subtest:\n%s", output)
	}

	// In -v output every logged line belongs to the test named by the closest "=== RUN" or "=== NAME"
	// header above it. The concurrent subtests interleave those headers in any order, so find the
	// message line and check that its closest header names the failing subtest, not the parent.
	if owner := testOwningLine(output, "expected 1 to equal 2"); !strings.HasSuffix(owner, "/D/par-assert") {
		t.Errorf("the failing subtest does not carry the assertion message (logged under %q); output:\n%s", owner, output)
	}
	if !strings.Contains(output, "--- FAIL: TestBuilderItParallelReportsFailureOnSubtestNotParent (") {
		t.Errorf("the parent must still fail; output:\n%s", output)
	}
	if strings.Contains(output, "--- FAIL: TestBuilderItParallelReportsFailureOnSubtestNotParent/D/par-ok") {
		t.Errorf("the passing sibling must not fail; output:\n%s", output)
	}
}

// testOwningLine returns the test name from the closest "=== RUN" or "=== NAME" header above the
// first line of output containing text, or "" when no line contains it or no header precedes it.
func testOwningLine(output, text string) string {
	lines := strings.Split(output, "\n")
	for i, line := range lines {
		if !strings.Contains(line, text) {
			continue
		}
		for j := i - 1; j >= 0; j-- {
			for _, header := range []string{"=== RUN ", "=== NAME "} {
				if rest, ok := strings.CutPrefix(lines[j], header); ok {
					return strings.TrimSpace(rest)
				}
			}
		}
		return ""
	}
	return ""
}

// On the *testing.B shape parallelStep has no subtest to report on, so reportFailures still replays
// each failure on the parent backend as "spec[N]: ...", and the parent Context is still failed.
func TestParallelStepKeepsParentReplayWithoutSubtests(t *testing.T) {
	backend := &collectingBackend{}
	ctx := &Context{backend: backend}
	parallelStep([]step{
		func(*Context) {},
		func(c *Context) { c.backend.Error("bad") },
	}, []string{"ok", "bad"}, nil)(ctx)

	if !ctx.hasFailed() {
		t.Error("the parent Context must be failed")
	}
	if len(backend.messages) != 1 || !strings.HasPrefix(backend.messages[0], "spec[1]: ") {
		t.Errorf("want one spec[1] replay on the parent, got %v", backend.messages)
	}
}
