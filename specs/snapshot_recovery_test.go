package specs

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestSnapshotMismatchRealProcessRecordsFailure proves the fix for #115 end to end: a ctx.Snapshot
// mismatch against a real *testing.T — not the fakeSnapshotBackend used elsewhere in this
// package — still reaches the reporter as a failed spec.
//
// Before the fix, the mismatch verdict was decided inside the snapshots package's RunFromFile,
// which called backend.Fatalf itself before returning to Context.Snapshot. Against a real
// *testing.T, Fatalf ends in runtime.Goexit, which unwinds the goroutine right there — control
// never returned to the `if !runSnapshot(...)` check that was supposed to call recordFailure(). The
// fake backend used by TestSnapshotFailureRecordsContextFailure couldn't catch this: its Fatalf
// just records a message and returns normally, so the ordering bug was invisible to it. Only a
// real testing.T, whose Fatalf actually calls FailNow -> runtime.Goexit, exercises the bug.
//
// Same subprocess pattern as TestRunnerRunRealFatalfIsolatesJustThatSpecRealProcess
// (runner_recovery_test.go): a nested t.Run's real Fatalf would mark this outer test itself failed,
// making "did the fix work" indistinguishable from "did this test fail for an unrelated reason."
func TestSnapshotMismatchRealProcessRecordsFailure(t *testing.T) {
	if os.Getenv("GO_SPECS_SNAPSHOT_MISMATCH_HELPER") == "1" {
		var rep recordingReporter
		prog := &Program{
			Groups: []group{
				{
					specs: []step{
						func(ctx *Context) { ctx.Snapshot("nonexistent-key-real-process", 42) },
						func(*Context) { fmt.Println("spec2 ran") },
					},
					names: []string{"spec one", "spec two"},
				},
			},
		}
		NewRunnerWithReporter(prog, "suite", &rep).Run(t)
		fmt.Printf("spec one failed=%v\n", rep.specFinished[0].Failed)
		fmt.Printf("suite finished total=%d failed=%d\n", rep.suiteFinished[0].TotalSpecs, rep.suiteFinished[0].FailedSpecs)
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestSnapshotMismatchRealProcessRecordsFailure$")
	cmd.Env = append(os.Environ(), "GO_SPECS_SNAPSHOT_MISMATCH_HELPER=1")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected the snapshot mismatch to fail the process, but it passed: %s", output)
	}
	if !strings.Contains(string(output), "spec2 ran") {
		t.Fatalf("expected the second spec to still run after the first spec's real Fatalf, got: %s", output)
	}
	if !strings.Contains(string(output), "spec one failed=true") {
		t.Fatalf("expected SpecResultEvent.Failed to be true for the mismatched spec, got: %s", output)
	}
	if !strings.Contains(string(output), "suite finished total=2 failed=1") {
		t.Fatalf("expected SuiteFinished to report total=2 failed=1, got: %s", output)
	}
}
