// spec_itparallel_failure_test.go pins failure containment for Spec.ItParallel (issue #245): a panic
// or a ctx.T.Fatal inside one parallel spec fails only that spec's own subtest. Its siblings still
// run to completion and pass, AfterEach still runs for every spec (including the failing ones), and
// the process does not crash. A failing run can only be observed from outside the failing process, so
// the suite runs as a real subprocess (runParallelHelper, spec_body_parallel_test.go).
package specs

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

const itParallelFailureTest = "TestSpecItParallel_PanicAndFatalAreContainedPerSpec"

func TestSpecItParallel_PanicAndFatalAreContainedPerSpec(t *testing.T) {
	if os.Getenv(parallelHelperEnv) == "1" {
		Describe(t, "suite", func(s *Spec) {
			s.AfterEach(func(*Context) { fmt.Println("AFTER_EACH_RAN") })
			s.ItParallel("alpha", func(*Context) {})
			s.ItParallel("bravo", func(*Context) { panic("parallel-boom") })
			s.ItParallel("charlie", func(ctx *Context) {
				ctx.T.Fatal("parallel-fatal")
				fmt.Println("BODY_CONTINUED_AFTER_FATAL")
			})
			s.ItParallel("delta", func(*Context) {})
		})
		return
	}

	output, failed := runParallelHelper(t, itParallelFailureTest)

	if !failed {
		t.Fatalf("expected the run to fail (bravo panics, charlie calls Fatal), got a passing run:\n%s", output)
	}
	// The top-level test reaching its own FAIL line means the binary finished the test normally
	// instead of being torn down by an unrecovered panic.
	if !strings.Contains(output, "--- FAIL: "+itParallelFailureTest+" (") {
		t.Fatalf("expected %s to complete and report FAIL, not crash the process:\n%s", itParallelFailureTest, output)
	}
	for _, want := range []struct{ verdict, spec string }{
		{"PASS", "alpha"},
		{"FAIL", "bravo"},
		{"FAIL", "charlie"},
		{"PASS", "delta"},
	} {
		re := regexp.MustCompile(`--- ` + want.verdict + `: ` + itParallelFailureTest + `/\S*` + want.spec + ` \(`)
		if !re.MatchString(output) {
			t.Errorf("expected subtest %q to report %s:\n%s", want.spec, want.verdict, output)
		}
	}
	for _, msg := range []string{"parallel-boom", "parallel-fatal"} {
		if !strings.Contains(output, msg) {
			t.Errorf("expected the failure message %q in the transcript:\n%s", msg, output)
		}
	}
	if got := strings.Count(output, "AFTER_EACH_RAN"); got != 4 {
		t.Errorf("AfterEach ran %d times, want 4 (once per spec, failing ones included):\n%s", got, output)
	}
	if strings.Contains(output, "BODY_CONTINUED_AFTER_FATAL") {
		t.Errorf("ctx.T.Fatal did not stop the rest of charlie's body:\n%s", output)
	}
}
