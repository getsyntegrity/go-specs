// spec_itparallel_dup_name_test.go pins Fix 2 for Spec.ItParallel (issue #245, Codex review P1
// "Serialize duplicate subtest-name assignment"): adjacent ItParallel specs sharing the same
// (normalized) name must be assigned their Go subtest names deterministically, in declaration
// order, exactly as testing's own matcher.unique (match.go) would assign them if every spec were
// registered one at a time — first = the base name, then "#01", "#02"... Before the fix,
// runParallelGroup launched one goroutine per spec that each called testing.T.Run(subtestName, ...)
// concurrently with the identical, un-deduplicated name; whichever goroutine's call reached
// testing's internal name-dedup mutex first won the unsuffixed name, so `-test.run` selecting that
// unsuffixed name picked a nondeterministic body.
//
// The two specs' bodies record which one ran. Because the outcome depends on goroutine scheduling,
// it can only be observed from outside the process, and only reliably across many independent
// registrations, so the suite reruns as a real subprocess with -test.count=20: each of the 20
// iterations re-registers the two "dup"-named specs from scratch, exercising the race fresh every
// time.
package specs

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

const itParallelDupNameTest = "TestSpecItParallel_DuplicateNameSubtestSelectionIsDeterministic"

func TestSpecItParallel_DuplicateNameSubtestSelectionIsDeterministic(t *testing.T) {
	if os.Getenv(parallelHelperEnv) == "1" {
		Describe(t, "suite", func(s *Spec) {
			s.ItParallel("dup", func(*Context) { fmt.Println("RAN:first") })
			s.ItParallel("dup", func(*Context) { fmt.Println("RAN:second") })
		})
		return
	}

	// -test.run selects, at the top level, this test; at the second level, the group "suite"; at
	// the third level, exactly the unsuffixed leaf name "dup" — never "dup#01", the suffix testing
	// gives the second identically named registration.
	cmd := exec.Command(os.Args[0], "-test.v",
		"-test.run=^"+itParallelDupNameTest+"$/^suite$/^dup$",
		"-test.count=20")
	cmd.Env = append(os.Environ(), parallelHelperEnv+"=1")
	raw, err := cmd.CombinedOutput()
	output := string(raw)
	if err != nil {
		t.Fatalf("helper process failed: %v\n%s", err, output)
	}
	first := strings.Count(output, "RAN:first")
	second := strings.Count(output, "RAN:second")
	if first != 20 || second != 0 {
		t.Fatalf("expected -test.run selecting the unsuffixed name %q to run only the first declared "+
			"spec, all 20 times; got first=%d second=%d:\n%s", "suite/dup", first, second, output)
	}
}
