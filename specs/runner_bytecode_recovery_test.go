package specs

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestRunBytecodeSequentialRecoversPanicSiblingsStillRun proves issue #15's core requirement for
// BytecodeRunner.Run: a panicking spec is recorded as a failure instead of crashing the process, and
// the next spec still runs. Recovery is per spec, matching RunParallel's granularity.
func TestRunBytecodeSequentialRecoversPanicSiblingsStillRun(t *testing.T) {
	backend := &controlledBackend{}
	ctx := &Context{backend: backend}
	var ranSpec2 bool
	b := NewBCBuilder(8)
	b.AddSpec(func(*Context) { panic("boom") })
	b.AddSpec(func(*Context) { ranSpec2 = true })
	prog := b.BuildBC()
	runBytecodeSequential(ctx, prog.Code, prog.SpecStarts)

	if !ranSpec2 {
		t.Fatal("expected the second spec to run after the first one panicked")
	}
	if len(backend.errors) != 1 || !strings.Contains(backend.errors[0], "boom") {
		t.Fatalf("expected exactly one recorded failure mentioning the panic message, got %v", backend.errors)
	}
}

// TestRunBytecodeSequentialPanicSkipsRestOfSpecRange proves recovery is per spec, not per
// instruction: a panic partway through one spec's instruction range (e.g. in a before hook) skips
// the rest of that spec's range but does not affect the next spec.
func TestRunBytecodeSequentialPanicSkipsRestOfSpecRange(t *testing.T) {
	backend := &controlledBackend{}
	ctx := &Context{backend: backend}
	var ranSpec1Body, ranSpec2 bool
	b := NewBCBuilder(8)
	b.AddBefore(func(*Context) { panic("boom") })
	b.AddSpec(func(*Context) { ranSpec1Body = true })
	prog := b.BuildBC()
	b2 := NewBCBuilder(8)
	b2.AddSpec(func(*Context) { ranSpec2 = true })
	prog2 := b2.BuildBC()

	// Concatenate the two programs into one, so we get a "before panics, body never runs" spec
	// followed by an unrelated spec, all in one flat instruction stream.
	code := append(append([]instruction{}, prog.Code...), prog2.Code...)
	starts := []int{prog.SpecStarts[0], prog.SpecStarts[1], prog.SpecStarts[1] + prog2.SpecStarts[1]}
	runBytecodeSequential(ctx, code, starts)

	if ranSpec1Body {
		t.Fatal("expected spec1's body to be skipped after its before hook panicked")
	}
	if !ranSpec2 {
		t.Fatal("expected spec2 to still run after spec1's before hook panicked")
	}
	if len(backend.errors) != 1 || !strings.Contains(backend.errors[0], "boom") {
		t.Fatalf("expected exactly one recorded failure mentioning the panic message, got %v", backend.errors)
	}
}

// TestRunBytecodeSequentialDoesNotDoubleReportExpectedAbort proves that a controlled backend's
// FailNow sentinel is not double-reported as an unexpected panic (mirrors the same check already
// made for the default execution path, Runner.Run, and BlockRunner.Run).
func TestRunBytecodeSequentialDoesNotDoubleReportExpectedAbort(t *testing.T) {
	backend := &controlledBackend{}
	ctx := &Context{backend: backend}
	var ranSpec2 bool
	b := NewBCBuilder(8)
	b.AddSpec(func(c *Context) { c.backend.FailNow() })
	b.AddSpec(func(*Context) { ranSpec2 = true })
	prog := b.BuildBC()
	runBytecodeSequential(ctx, prog.Code, prog.SpecStarts)

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

// TestBytecodeRunnerRunRecoversPanicRealProcess proves the fix end-to-end through the real public
// BytecodeRunner.Run(tb testing.TB) entry point, using a subprocess so the intentional panic/failure
// doesn't mark this test itself as failed (mirrors execution_plan_test.go's subprocess pattern).
func TestBytecodeRunnerRunRecoversPanicRealProcess(t *testing.T) {
	if os.Getenv("BYTECODE_RUNNER_RECOVERY_SUBPROCESS") == "1" {
		var ranSpec2 bool
		b := NewBCBuilder(8)
		b.AddSpec(func(*Context) { panic("boom") })
		b.AddSpec(func(*Context) { ranSpec2 = true })
		prog := b.BuildBC()
		r := NewBytecodeRunner(prog)
		r.Run(t)
		if ranSpec2 {
			fmt.Println("spec2 ran")
		}
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestBytecodeRunnerRunRecoversPanicRealProcess")
	cmd.Env = append(os.Environ(), "BYTECODE_RUNNER_RECOVERY_SUBPROCESS=1")
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

// TestBytecodeRunnerRunParallelRecoversPanicSiblingsStillRun proves the fix for RunParallel's more
// severe gap: before this fix, a panic inside a worker goroutine crashed the entire process, taking
// down every concurrently-running test, not just this one. Now it's recorded as that spec's failure
// and every sibling spec (across all workers) still completes.
func TestBytecodeRunnerRunParallelRecoversPanicSiblingsStillRun(t *testing.T) {
	const n = 20
	const panicIndex = 5
	var completed [n]bool
	b := NewBCBuilder(n)
	for i := 0; i < n; i++ {
		i := i
		b.AddSpec(func(*Context) {
			if i == panicIndex {
				panic("boom")
			}
			completed[i] = true
		})
	}
	prog := b.BuildBC()
	runner := NewBytecodeRunner(prog)

	var reported string
	fake := &fakeReporter{fatalf: func(format string, args ...any) { reported = fmt.Sprintf(format, args...) }}
	runner.RunParallel(fake, 4)

	for i := 0; i < n; i++ {
		if i == panicIndex {
			continue
		}
		if !completed[i] {
			t.Errorf("expected spec[%d] to complete, it did not", i)
		}
	}
	if reported == "" || !strings.Contains(reported, "boom") {
		t.Fatalf("expected the panic to be reported mentioning the panic message, got %q", reported)
	}
	if !strings.Contains(reported, fmt.Sprintf("spec[%d]", panicIndex)) {
		t.Fatalf("expected the report to mention spec[%d], got %q", panicIndex, reported)
	}
}

// TestBytecodeRunnerRunParallelDoesNotDoubleReportExpectedAbort proves parallelAbort{} (the sentinel
// a fatal assertion panics with when abortOnFatal is set) is not double-reported as an unexpected
// panic — mirrors scheduler.go's runWorkerSpec test coverage.
func TestBytecodeRunnerRunParallelDoesNotDoubleReportExpectedAbort(t *testing.T) {
	results := make([]parallelFailure, 1)
	backend := &parallelBackend{results: &results, specIndex: 0, abortOnFatal: true}
	code := []instruction{{fn: func(c *Context) { c.backend.FailNow() }}}
	runBytecodeWorkerSpec(code, 0, 1, &Context{backend: backend}, &results, 0)

	if results[0].Message != "fail now" {
		t.Fatalf("expected results[0].Message to be %q (recorded by FailNow before the abort panic), got %q", "fail now", results[0].Message)
	}
}
