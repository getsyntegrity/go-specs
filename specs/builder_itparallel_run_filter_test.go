// builder_itparallel_run_filter_test.go pins issue #330: under `go test -run`, Builder.ItParallel
// behaves like Spec.ItParallel — the selected spec runs and is reported with its real status, and
// every excluded spec is reported Filtered, one event per spec, in declaration order, with the suite
// totals counting them. The selector is a real process flag, so each case re-executes this test
// binary as a helper.
package specs

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

const builderItParallelRunFilterEnv = "GO_SPECS_BUILDER_ITPARALLEL_RUN_FILTER_HELPER"

// TestBuilderItParallel_RunSelector_RealProcess runs three Builder.ItParallel specs under a
// selector matching one of them, none of them, and no selector at all.
func TestBuilderItParallel_RunSelector_RealProcess(t *testing.T) {
	if os.Getenv(builderItParallelRunFilterEnv) == "1" {
		rec := &recordingReporter{}
		b := NewBuilder()
		b.Describe("suite", func() {
			for _, name := range []string{"one", "two", "three"} {
				b.ItParallel(name, func(*Context) { fmt.Printf("RAN %s\n", name) })
			}
		})
		NewRunnerWithReporter(b.Build(), "suite", rec).Run(t)
		for i, started := range rec.specStarted {
			fmt.Printf("REPORTED name=%q filtered=%v failed=%v\n", started.Name, rec.specFinished[i].Filtered, rec.specFinished[i].Failed)
		}
		for _, e := range rec.suiteFinished {
			fmt.Printf("TOTALS total=%d failed=%d filtered=%d\n", e.TotalSpecs, e.FailedSpecs, e.FilteredSpecs)
		}
		return
	}

	const test = "^TestBuilderItParallel_RunSelector_RealProcess$"
	cases := []struct {
		name     string
		selector string
		wantRan  []string
		want     []string
	}{
		{
			name:     "selector matches one spec",
			selector: test + "/^suite$/^two$",
			wantRan:  []string{"two"},
			want: []string{
				`REPORTED name="one" filtered=true failed=false`,
				`REPORTED name="two" filtered=false failed=false`,
				`REPORTED name="three" filtered=true failed=false`,
				`TOTALS total=3 failed=0 filtered=2`,
			},
		},
		{
			name:     "selector matches none of the batch",
			selector: test + "/^suite$/^nomatch$",
			want: []string{
				`REPORTED name="one" filtered=true failed=false`,
				`REPORTED name="two" filtered=true failed=false`,
				`REPORTED name="three" filtered=true failed=false`,
				`TOTALS total=3 failed=0 filtered=3`,
			},
		},
		{
			name:     "no selector",
			selector: test,
			wantRan:  []string{"one", "two", "three"},
			want: []string{
				`REPORTED name="one" filtered=false failed=false`,
				`REPORTED name="two" filtered=false failed=false`,
				`REPORTED name="three" filtered=false failed=false`,
				`TOTALS total=3 failed=0 filtered=0`,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run="+tc.selector)
			cmd.Env = append(os.Environ(), builderItParallelRunFilterEnv+"=1")
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("helper run failed: %v\n%s", err, output)
			}
			transcript := string(output)

			if got := strings.Count(transcript, "RAN "); got != len(tc.wantRan) {
				t.Fatalf("%d bodies ran, want %v:\n%s", got, tc.wantRan, transcript)
			}
			for _, name := range tc.wantRan {
				if !strings.Contains(transcript, "RAN "+name+"\n") {
					t.Fatalf("spec %q did not run:\n%s", name, transcript)
				}
			}
			// The reported lines must appear in exactly this order: declaration order, then totals.
			rest := transcript
			for _, want := range tc.want {
				idx := strings.Index(rest, want)
				if idx < 0 {
					t.Fatalf("missing or out-of-order %q in:\n%s", want, transcript)
				}
				rest = rest[idx+len(want):]
			}
		})
	}
}

// TestBuilderItParallel_FailingSpecFailsItsOwnSubtest pins that, now that each Builder.ItParallel
// spec runs in its own subtest (#330), `go test` marks the failing spec's subtest FAIL and leaves the
// passing one PASS, as Spec.ItParallel does, instead of a PASS line for a spec that failed.
func TestBuilderItParallel_FailingSpecFailsItsOwnSubtest(t *testing.T) {
	if os.Getenv(builderItParallelRunFilterEnv) == "fail" {
		b := NewBuilder()
		b.Describe("suite", func() {
			b.ItParallel("ok", func(*Context) {})
			b.ItParallel("bad", func(ctx *Context) { ctx.Expect(1).ToEqual(2) })
		})
		NewRunner(b.Build()).Run(t)
		return
	}

	const test = "TestBuilderItParallel_FailingSpecFailsItsOwnSubtest"
	cmd := exec.Command(os.Args[0], "-test.v", "-test.run=^"+test+"$")
	cmd.Env = append(os.Environ(), builderItParallelRunFilterEnv+"=fail")
	output, _ := cmd.CombinedOutput() // the helper fails on purpose
	transcript := string(output)
	for _, want := range []string{
		"--- FAIL: " + test + "/suite/bad ",
		"--- PASS: " + test + "/suite/ok ",
		"--- FAIL: " + test + " ",
	} {
		if !strings.Contains(transcript, want) {
			t.Errorf("missing %q in:\n%s", want, transcript)
		}
	}
}
