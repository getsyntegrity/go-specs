// spec_itparallel_dup_sequential_sibling_test.go extends the duplicate-name pin for Spec.ItParallel
// (issue #245): Go's testing package numbers a subtest name against every subtest the same parent
// *testing.T has already registered, not only against the parallel group. A sequential It("a")
// declared before two ItParallel("a") in the same Describe is registered first and takes "a", so the
// parallel specs must get "a#01" and "a#02", in declaration order, every time. If the group's name
// table ignored the earlier sequential sibling, it would hand out "a" and "a#01", Go would re-number
// the first of them, and both goroutines would race for "a#01" again.
package specs

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

const itParallelDupSeqSiblingTest = "TestSpecItParallel_DuplicateNameAfterSequentialSiblingIsDeterministic"

func TestSpecItParallel_DuplicateNameAfterSequentialSiblingIsDeterministic(t *testing.T) {
	if os.Getenv(parallelHelperEnv) == "1" {
		Describe(t, "suite", func(s *Spec) {
			s.It("a", func(*Context) { fmt.Println("RAN:sequential") })
			s.ItParallel("a", func(*Context) { fmt.Println("RAN:parallel-1") })
			s.ItParallel("a", func(*Context) { fmt.Println("RAN:parallel-2") })
		})
		return
	}

	for _, tc := range []struct {
		leaf, want string
	}{
		{`a`, "RAN:sequential"},
		{`a#01`, "RAN:parallel-1"},
		{`a#02`, "RAN:parallel-2"},
	} {
		cmd := exec.Command(os.Args[0], "-test.v",
			"-test.run=^"+itParallelDupSeqSiblingTest+"$/^suite$/^"+tc.leaf+"$",
			"-test.count=20")
		cmd.Env = append(os.Environ(), parallelHelperEnv+"=1")
		raw, err := cmd.CombinedOutput()
		output := string(raw)
		if err != nil {
			t.Fatalf("helper process failed for -run leaf %q: %v\n%s", tc.leaf, err, output)
		}
		for _, marker := range []string{"RAN:sequential", "RAN:parallel-1", "RAN:parallel-2"} {
			want := 0
			if marker == tc.want {
				want = 20
			}
			if got := strings.Count(output, marker); got != want {
				t.Errorf("-run leaf %q: %s ran %d times, want %d:\n%s", tc.leaf, marker, got, want, output)
			}
		}
		if got := strings.Count(output, "--- PASS: "+itParallelDupSeqSiblingTest+"/suite/"+tc.leaf+" ("); got != 20 {
			t.Errorf("-run leaf %q: subtest %q passed %d times, want 20 (name not stable):\n%s",
				tc.leaf, "suite/"+tc.leaf, got, output)
		}
	}
}
