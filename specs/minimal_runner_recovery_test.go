package specs

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
)

// TestRunMinimalSpecsRecoversPanicSiblingsStillRun proves issue #15's core requirement for
// MinimalRunner.Run: a panicking spec is recorded as a failure instead of crashing the process, and
// the next spec in the same batch still runs.
func TestRunMinimalSpecsRecoversPanicSiblingsStillRun(t *testing.T) {
	backend := &controlledBackend{}
	ctx := &Context{backend: backend}
	var ranSpec2 bool
	specs := []RunSpec{
		{Name: "s1", Fn: func(*Context) { panic("boom") }},
		{Name: "s2", Fn: func(*Context) { ranSpec2 = true }},
	}
	runMinimalSpecs(ctx, specs)

	if !ranSpec2 {
		t.Fatal("expected the second spec to run after the first one panicked")
	}
	if len(backend.errors) != 1 || !strings.Contains(backend.errors[0], "boom") {
		t.Fatalf("expected exactly one recorded failure mentioning the panic message, got %v", backend.errors)
	}
}

// TestRunMinimalSpecsRecoversPanicAcrossBatchBoundary proves a panic in one RunBatchSize batch
// doesn't stop the next batch from running — no results are discarded across the batching boundary.
func TestRunMinimalSpecsRecoversPanicAcrossBatchBoundary(t *testing.T) {
	backend := &controlledBackend{}
	ctx := &Context{backend: backend}
	n := RunBatchSize + 1
	specs := make([]RunSpec, n)
	specs[0] = RunSpec{Name: "s0", Fn: func(*Context) { panic("boom") }}
	var ranLast bool
	for i := 1; i < n-1; i++ {
		specs[i] = RunSpec{Name: "s", Fn: func(*Context) {}}
	}
	specs[n-1] = RunSpec{Name: "last", Fn: func(*Context) { ranLast = true }}
	runMinimalSpecs(ctx, specs)

	if !ranLast {
		t.Fatal("expected the spec in the next batch to still run after the first batch's spec panicked")
	}
	if len(backend.errors) != 1 || !strings.Contains(backend.errors[0], "boom") {
		t.Fatalf("expected exactly one recorded failure mentioning the panic message, got %v", backend.errors)
	}
}

// TestRunMinimalSpecsDoesNotDoubleReportExpectedAbort proves that a controlled backend's FailNow
// sentinel is not double-reported as an unexpected panic.
func TestRunMinimalSpecsDoesNotDoubleReportExpectedAbort(t *testing.T) {
	backend := &controlledBackend{}
	ctx := &Context{backend: backend}
	var ranSpec2 bool
	specs := []RunSpec{
		{Name: "s1", Fn: func(c *Context) { c.backend.FailNow() }},
		{Name: "s2", Fn: func(*Context) { ranSpec2 = true }},
	}
	runMinimalSpecs(ctx, specs)

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

// TestMinimalRunnerRunRecoversPanicRealProcess proves the fix end-to-end through the real public
// MinimalRunner.Run(tb testing.TB) entry point, in a subprocess (mirrors the pattern already used for
// #61/#62/#65 — avoids polluting the outer test's pass/fail via the real backend's Errorf).
func TestMinimalRunnerRunRecoversPanicRealProcess(t *testing.T) {
	if os.Getenv("MINIMAL_RUNNER_RECOVERY_SUBPROCESS") == "1" {
		var ranSpec2 bool
		r := NewMinimalRunner(2)
		r.Add("s1", func(*Context) { panic("boom") })
		r.Add("s2", func(*Context) { ranSpec2 = true })
		r.Run(t)
		if ranSpec2 {
			fmt.Println("spec2 ran")
		}
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestMinimalRunnerRunRecoversPanicRealProcess")
	cmd.Env = append(os.Environ(), "MINIMAL_RUNNER_RECOVERY_SUBPROCESS=1")
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

// TestMinimalRunnerRunParallelAlreadyRecoversPanic confirms RunParallel (via scheduler.go's
// runWorker/runWorkerSpec, unchanged by this fix) isolates a panic to the panicking spec's result:
// sibling specs across workers still complete and the process doesn't crash. Documents that the
// parallel path was already safe before this fix — only the sequential Run needed one.
func TestMinimalRunnerRunParallelAlreadyRecoversPanic(t *testing.T) {
	const n = 20
	r := NewMinimalRunner(n)
	var mu sync.Mutex
	ran := make(map[int]bool)
	for i := 0; i < n; i++ {
		i := i
		r.Add("s", func(*Context) {
			if i == 5 {
				panic("boom")
			}
			mu.Lock()
			ran[i] = true
			mu.Unlock()
		})
	}
	var reported string
	reporter := &fakeReporter{fatalf: func(format string, args ...any) { reported = fmt.Sprintf(format, args...) }}
	r.RunParallel(reporter, 4)

	if len(ran) != n-1 {
		t.Fatalf("expected %d specs to complete despite one panicking, got %d", n-1, len(ran))
	}
	if !strings.Contains(reported, "boom") {
		t.Fatalf("expected the panicking spec to be reported as a failure, got %q", reported)
	}
}

// TestMinimalRunnerRunParallelBatchedAlreadyRecoversPanic is the RunParallelBatched analogue of
// TestMinimalRunnerRunParallelAlreadyRecoversPanic (via scheduler_batch.go's runWorkerBatched, which
// also delegates to runWorkerSpec).
func TestMinimalRunnerRunParallelBatchedAlreadyRecoversPanic(t *testing.T) {
	const n = 20
	r := NewMinimalRunner(n)
	var mu sync.Mutex
	ran := make(map[int]bool)
	for i := 0; i < n; i++ {
		i := i
		r.Add("s", func(*Context) {
			if i == 5 {
				panic("boom")
			}
			mu.Lock()
			ran[i] = true
			mu.Unlock()
		})
	}
	var reported string
	reporter := &fakeReporter{fatalf: func(format string, args ...any) { reported = fmt.Sprintf(format, args...) }}
	r.RunParallelBatched(reporter, 4, 4)

	if len(ran) != n-1 {
		t.Fatalf("expected %d specs to complete despite one panicking, got %d", n-1, len(ran))
	}
	if !strings.Contains(reported, "boom") {
		t.Fatalf("expected the panicking spec to be reported as a failure, got %q", reported)
	}
}
