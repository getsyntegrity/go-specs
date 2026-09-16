package specs

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestRunBlocksRecoversPanicSiblingsStillRun proves issue #15's core requirement for BlockRunner: a
// panicking spec is recorded as a failure instead of crashing the process, and the next spec in the
// same block still runs.
func TestRunBlocksRecoversPanicSiblingsStillRun(t *testing.T) {
	backend := &controlledBackend{}
	ctx := &Context{backend: backend}
	var ranSpec2 bool
	fns := []func(*Context){
		func(*Context) { panic("boom") },
		func(*Context) { ranSpec2 = true },
	}
	blocks := []specBlock{{start: 0, count: 2}}
	runBlocks(ctx, fns, blocks)

	if !ranSpec2 {
		t.Fatal("expected the second spec in the block to run after the first one panicked")
	}
	if len(backend.errors) != 1 || !strings.Contains(backend.errors[0], "boom") {
		t.Fatalf("expected exactly one recorded failure mentioning the panic message, got %v", backend.errors)
	}
}

// TestRunBlocksContinuesToNextBlockAfterPanic proves a panic in one block doesn't stop the next
// block from running — no results are discarded.
func TestRunBlocksContinuesToNextBlockAfterPanic(t *testing.T) {
	backend := &controlledBackend{}
	ctx := &Context{backend: backend}
	var ranBlock2 bool
	fns := []func(*Context){
		func(*Context) { panic("boom") },
		func(*Context) { ranBlock2 = true },
	}
	blocks := []specBlock{{start: 0, count: 1}, {start: 1, count: 1}}
	runBlocks(ctx, fns, blocks)

	if !ranBlock2 {
		t.Fatal("expected the second block to still run after the first block's spec panicked")
	}
	if len(backend.errors) != 1 || !strings.Contains(backend.errors[0], "boom") {
		t.Fatalf("expected exactly one recorded failure mentioning the panic message, got %v", backend.errors)
	}
}

// TestRunBlocksDoesNotDoubleReportExpectedAbort proves that a controlled backend's FailNow sentinel
// is not double-reported as an unexpected panic (mirrors the same check already made for the default
// execution path and Runner.Run).
func TestRunBlocksDoesNotDoubleReportExpectedAbort(t *testing.T) {
	backend := &controlledBackend{}
	ctx := &Context{backend: backend}
	var ranSpec2 bool
	fns := []func(*Context){
		func(c *Context) { c.backend.FailNow() },
		func(*Context) { ranSpec2 = true },
	}
	blocks := []specBlock{{start: 0, count: 2}}
	runBlocks(ctx, fns, blocks)

	if !backend.failNow {
		t.Fatal("expected the backend to have recorded FailNow")
	}
	if !ranSpec2 {
		t.Fatal("expected the second spec to still run after the expected-abort sentinel")
	}
	if len(backend.errors) != 0 {
		t.Fatalf("expected no unexpected-panic error for an expected abort sentinel, got %v", backend.errors)
	}
}

// TestBlockRunnerRunRecoversPanicRealProcess proves the fix end-to-end through the real public
// BlockRunner.Run(tb testing.TB) entry point, using a subprocess so the intentional panic/failure
// doesn't mark this test itself as failed (mirrors execution_plan_test.go's subprocess pattern).
func TestBlockRunnerRunRecoversPanicRealProcess(t *testing.T) {
	if os.Getenv("BLOCK_RUNNER_RECOVERY_SUBPROCESS") == "1" {
		var ranSpec2 bool
		specs := []RunSpec{
			{Name: "s1", Fn: func(*Context) { panic("boom") }},
			{Name: "s2", Fn: func(*Context) { ranSpec2 = true }},
		}
		fns, blocks := CompileBlocks(specs, 8)
		r := NewBlockRunner(fns, blocks)
		r.Run(t)
		if ranSpec2 {
			fmt.Println("spec2 ran")
		}
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestBlockRunnerRunRecoversPanicRealProcess")
	cmd.Env = append(os.Environ(), "BLOCK_RUNNER_RECOVERY_SUBPROCESS=1")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected the subprocess test to fail (panic recorded via t.Errorf), but it passed:\n%s", out)
	}
	if !strings.Contains(string(out), "spec2 ran") {
		t.Fatalf("expected spec2 to still run after spec1 panicked, output:\n%s", out)
	}
	if !strings.Contains(string(out), "boom") {
		t.Fatalf("expected the panic message in the subprocess output, got:\n%s", out)
	}
}
