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

	// In -v output the subtest's own lines follow its "=== RUN" line; anything logged on the parent
	// is instead introduced by a "=== NAME <parent>" switch. The message must be in the first form.
	const run = "=== RUN   TestBuilderItParallelReportsFailureOnSubtestNotParent/D/par-assert\n"
	block := ""
	if i := strings.Index(output, run); i >= 0 {
		block = output[i+len(run):]
		if j := strings.Index(block, "--- FAIL"); j >= 0 {
			block = block[:j]
		}
	}
	if !strings.Contains(block, "expected 1 to equal 2") || strings.Contains(block, "=== NAME") {
		t.Errorf("the failing subtest does not carry the assertion message; output:\n%s", output)
	}
	if !strings.Contains(output, "--- FAIL: TestBuilderItParallelReportsFailureOnSubtestNotParent (") {
		t.Errorf("the parent must still fail; output:\n%s", output)
	}
	if strings.Contains(output, "--- FAIL: TestBuilderItParallelReportsFailureOnSubtestNotParent/D/par-ok") {
		t.Errorf("the passing sibling must not fail; output:\n%s", output)
	}
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
