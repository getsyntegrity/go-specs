// focus_fail_real_process_test.go proves issue #273's fail-on-committed-focus policy end-to-end, in
// a real subprocess: the in-process tests in focus_fail_test.go drive reportFocusPolicy directly
// through spyTB, which never actually fails the test process, so they cannot observe the one thing
// that matters most to a user — whether `go test` itself exits non-zero. These tests do, following
// the same subprocess pattern as TestBuiltInAssertionMessageReachesSpecResultEventRealProcess
// (builtin_assertion_message_report_test.go): a helper env var re-invokes this same test binary
// against a narrowed -test.run, and the parent asserts the child's exit code, printed diagnostic
// message, and every reported SpecResultEvent (Filtered/Failed), for both policy modes
// (GO_SPECS_ALLOW_FOCUS unset vs "1") across all three engines.
package specs

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// focusFailRealProcessHelperSuite is the tree every engine below declares: one plain It (excluded
// by the focus, reported Filtered) and one FIt (kept, runs and passes).
func focusFailRealProcessHelperSuite(s *Spec) {
	s.It("plain", func(*Context) {})
	s.FIt("focused", func(*Context) {})
}

// printFocusFailReport prints one recognizable line per reported spec, for the parent process to
// grep out of the child's combined output.
func printFocusFailReport(rep *recordingReporter) {
	for _, e := range rep.specFinished {
		fmt.Printf("SPEC_FINISHED name=%s filtered=%t failed=%t\n", e.Name, e.Filtered, e.Failed)
	}
}

// TestFocusFailPolicyRealProcess proves the policy end-to-end for the bytecode compiler, the
// Analyze/registry path, and Builder/Runner, in both modes.
func TestFocusFailPolicyRealProcess(t *testing.T) {
	engines := map[string]func(t *testing.T, rep *recordingReporter){
		"describe-compiler": func(t *testing.T, rep *recordingReporter) {
			DescribeWithReporter(t, "suite", rep, focusFailRealProcessHelperSuite)
		},
		"describe-registry": func(t *testing.T, rep *recordingReporter) {
			Analyze(func() {
				DescribeWithReporter(t, "suite", rep, focusFailRealProcessHelperSuite)
			})
		},
		"builder": func(t *testing.T, rep *recordingReporter) {
			b := NewBuilder()
			b.Describe("suite", func() {
				b.It("plain", func(*Context) {})
				b.FIt("focused", func(*Context) {})
			})
			NewRunnerWithReporter(b.Build(), "suite", rep).Run(t)
		},
	}

	const helperEnv = "GO_SPECS_FOCUS_FAIL_HELPER"
	if engine := os.Getenv(helperEnv); engine != "" {
		var rep recordingReporter
		engines[engine](t, &rep)
		printFocusFailReport(&rep)
		return
	}

	for engineName := range engines {
		engineName := engineName
		for _, mode := range []string{"default", "allow"} {
			t.Run(engineName+"/"+mode, func(t *testing.T) {
				cmd := exec.Command(os.Args[0], "-test.run=^TestFocusFailPolicyRealProcess$", "-test.v")
				env := append(os.Environ(), helperEnv+"="+engineName)
				if mode == "allow" {
					env = append(env, allowFocusEnvVar+"=1")
				}
				cmd.Env = env
				output, err := cmd.CombinedOutput()
				out := string(output)

				switch mode {
				case "default":
					if err == nil {
						t.Fatalf("expected the enclosing test to fail (committed focus), but it passed:\n%s", out)
					}
					if !strings.Contains(out, "go-specs: 1 focused spec(s) (FIt) are active, 1 spec(s) excluded") {
						t.Fatalf("expected the fail-on-focus message, got:\n%s", out)
					}
					if !strings.Contains(out, "GO_SPECS_ALLOW_FOCUS=1") {
						t.Fatalf("expected the message to name the opt-out, got:\n%s", out)
					}
				case "allow":
					if err != nil {
						t.Fatalf("expected the enclosing test to pass under GO_SPECS_ALLOW_FOCUS=1, got error %v:\n%s", err, out)
					}
					if strings.Contains(out, "go-specs: ") {
						t.Fatalf("expected no fail-on-focus message under the opt-out, got:\n%s", out)
					}
				}
				if !strings.Contains(out, "SPEC_FINISHED name=focused filtered=false failed=false") {
					t.Fatalf("expected the focused spec to run and pass in both modes, got:\n%s", out)
				}
				if !strings.Contains(out, "SPEC_FINISHED name=plain filtered=true failed=false") {
					t.Fatalf("expected the plain spec reported Filtered in both modes, got:\n%s", out)
				}
			})
		}
	}
}

// TestFocusFailPolicyNestedSkipPendingRealProcess covers a nested focus alongside SkipIt/PendingIt
// marks the focus drops (issue #273's explicit scope: "SkipIt/PendingIt marks that focus currently
// drops" must also be reported Filtered), in a real subprocess, on both build paths behind *Spec.
func TestFocusFailPolicyNestedSkipPendingRealProcess(t *testing.T) {
	build := func(s *Spec) {
		s.It("top-level plain", func(*Context) {})
		s.SkipIt("top-level skip", nil)
		s.When("nested", func(w *Spec) {
			w.PendingIt("nested pending", nil)
			w.FIt("nested focused", func(*Context) {})
		})
	}
	engines := map[string]func(t *testing.T, rep *recordingReporter){
		"compiler": func(t *testing.T, rep *recordingReporter) {
			DescribeWithReporter(t, "suite", rep, build)
		},
		"registry": func(t *testing.T, rep *recordingReporter) {
			Analyze(func() { DescribeWithReporter(t, "suite", rep, build) })
		},
	}

	const helperEnv = "GO_SPECS_FOCUS_FAIL_NESTED_HELPER"
	if engine := os.Getenv(helperEnv); engine != "" {
		var rep recordingReporter
		engines[engine](t, &rep)
		printFocusFailReport(&rep)
		return
	}

	for engineName := range engines {
		engineName := engineName
		t.Run(engineName, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestFocusFailPolicyNestedSkipPendingRealProcess$", "-test.v")
			cmd.Env = append(os.Environ(), helperEnv+"="+engineName)
			output, err := cmd.CombinedOutput()
			out := string(output)

			if err == nil {
				t.Fatalf("expected the enclosing test to fail (committed focus), but it passed:\n%s", out)
			}
			if !strings.Contains(out, "go-specs: 1 focused spec(s) (FIt) are active, 3 spec(s) excluded") {
				t.Fatalf("expected the fail-on-focus message naming 3 excluded specs, got:\n%s", out)
			}
			for _, want := range []string{
				"SPEC_FINISHED name=top-level plain filtered=true failed=false",
				"SPEC_FINISHED name=top-level skip filtered=true failed=false",
				"SPEC_FINISHED name=nested pending filtered=true failed=false",
				"SPEC_FINISHED name=nested focused filtered=false failed=false",
			} {
				if !strings.Contains(out, want) {
					t.Errorf("expected %q in output, got:\n%s", want, out)
				}
			}
		})
	}
}

// TestFocusFailPolicyRunShardRealProcess proves RunShard's contract in real subprocesses: every
// shard fails its own enclosing test on its own (issue #273's "ALWAYS" — each shard is ordinarily
// its own CI job), while the excluded specs are reported Filtered exactly once across the union of
// every shard's own process output — never once per shard, never omitted.
func TestFocusFailPolicyRunShardRealProcess(t *testing.T) {
	const shardCount = 2
	const helperEnv = "GO_SPECS_FOCUS_FAIL_SHARD_HELPER"
	if shardArg := os.Getenv(helperEnv); shardArg != "" {
		var shardIndex int
		if _, err := fmt.Sscanf(shardArg, "%d", &shardIndex); err != nil {
			t.Fatalf("bad shard index %q: %v", shardArg, err)
		}
		suite := BuildSuite(nil, "suite", func(s *Spec) {
			s.It("a", func(*Context) {})
			s.It("b", func(*Context) {})
			s.FIt("focused", func(*Context) {})
		})
		rep := &recordingReporter{}
		suite.Reporter = rep
		suite.RunShard(t, shardIndex, shardCount)
		printFocusFailReport(rep)
		return
	}

	var combined strings.Builder
	anyFailed := false
	for shard := 0; shard < shardCount; shard++ {
		cmd := exec.Command(os.Args[0], "-test.run=^TestFocusFailPolicyRunShardRealProcess$", "-test.v")
		cmd.Env = append(os.Environ(), fmt.Sprintf("%s=%d", helperEnv, shard))
		output, err := cmd.CombinedOutput()
		if err != nil {
			anyFailed = true
		}
		fmt.Fprintf(&combined, "--- shard %d (err=%v) ---\n", shard, err)
		combined.Write(output)
		combined.WriteString("\n")
	}
	out := combined.String()

	if !anyFailed {
		t.Fatalf("expected at least one shard's enclosing test to fail (committed focus), got:\n%s", out)
	}
	if !strings.Contains(out, "go-specs: 1 focused spec(s) (FIt) are active, 2 spec(s) excluded") {
		t.Fatalf("expected every shard's fail-on-focus message to name the suite-wide 2 excluded specs, got:\n%s", out)
	}
	filteredA := strings.Count(out, "SPEC_FINISHED name=a filtered=true failed=false")
	filteredB := strings.Count(out, "SPEC_FINISHED name=b filtered=true failed=false")
	focusedRan := strings.Count(out, "SPEC_FINISHED name=focused filtered=false failed=false")
	if filteredA != 1 {
		t.Errorf("spec a reported Filtered %d times across every shard, want exactly 1:\n%s", filteredA, out)
	}
	if filteredB != 1 {
		t.Errorf("spec b reported Filtered %d times across every shard, want exactly 1:\n%s", filteredB, out)
	}
	if focusedRan != 1 {
		t.Errorf("focused spec ran and reported %d times across every shard, want exactly 1:\n%s", focusedRan, out)
	}
}
