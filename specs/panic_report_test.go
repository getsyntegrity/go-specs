package specs

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// releasedBackend returns a runnableBackend in exactly the state putTestBackend leaves it in before
// handing it back to runnableBackendPool: alive as an interface value, but with its testing.TB
// cleared. Every reporting method on it dereferences that nil tb, so it is the shape that turned a
// recovered spec panic into a process crash (#171).
func releasedBackend() testBackend { return &runnableBackend{} }

// panicRecoveryEngine is one execution engine's recovered-step entry point, reduced to "run this
// panicking function against this context". #171 is a cross-engine invariant, so every engine that
// recovers a panic and reports it to a backend is exercised through the same table.
type panicRecoveryEngine struct {
	name string
	run  func(ctx *Context, fn func(*Context))
}

func panicRecoveryEngines() []panicRecoveryEngine {
	return []panicRecoveryEngine{
		{"Runner", func(ctx *Context, fn func(*Context)) { runStepRecovered(ctx, fn, "spec") }},
		{"ExecutionPlan", func(ctx *Context, fn func(*Context)) {
			runProgram([]Instruction{{Code: OpBody, Fn: fn}}, ctx, nil)
		}},
		{"ExecutionPlanAfterHook", func(ctx *Context, fn func(*Context)) {
			runAfterInstructionRecovered(ctx, Instruction{Code: OpAfterHook, Fn: fn})
		}},
		{"Bytecode", func(ctx *Context, fn func(*Context)) {
			runBytecodeSpecRecovered(ctx, []instruction{{fn: fn}}, 0, 1)
		}},
		{"Block", runBlockSpecRecovered},
		{"Minimal", runMinimalSpecRecovered},
	}
}

// TestPanicRecoverySurvivesAnUnavailableBackendRealProcess is #171's regression. It drives every
// engine's recovery path with both an absent backend and a released (pooled, tb-cleared) one, in a
// subprocess, because the defect it guards against is a process crash: an in-process assertion
// cannot distinguish "recovery handled it" from "the test binary died before asserting". The
// subprocess must exit cleanly and print the final marker.
func TestPanicRecoverySurvivesAnUnavailableBackendRealProcess(t *testing.T) {
	const marker = "every engine survived an unavailable backend"

	if os.Getenv("GO_SPECS_PANIC_REPORT_HELPER") == "1" {
		for _, engine := range panicRecoveryEngines() {
			for _, backend := range []testBackend{nil, releasedBackend()} {
				ctx := &Context{backend: backend}
				engine.run(ctx, func(*Context) { panic("boom") })
				if !ctx.failed {
					t.Fatalf("%s: expected the recovered panic to be recorded as a failure", engine.name)
				}
			}
		}
		t.Log(marker)
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestPanicRecoverySurvivesAnUnavailableBackendRealProcess$", "-test.v")
	cmd.Env = append(os.Environ(), "GO_SPECS_PANIC_REPORT_HELPER=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected the subprocess to survive recovery against an unavailable backend, got %v: %s", err, output)
	}
	if !strings.Contains(string(output), marker) {
		t.Fatalf("expected the subprocess to reach the end of every engine, got: %s", output)
	}
}

// TestPanicRecoveryReportsAnUndeliverablePanicOnStderr pins the answer to "what is reported when the
// original panic cannot reach a backend": it goes to stderr, naming the panic and its stack, so the
// failure is loud rather than silently converted into a pass.
func TestPanicRecoveryReportsAnUndeliverablePanicOnStderr(t *testing.T) {
	if os.Getenv("GO_SPECS_PANIC_REPORT_STDERR_HELPER") == "1" {
		ctx := &Context{backend: releasedBackend()}
		runStepRecovered(ctx, func(*Context) { panic("undeliverable boom") }, "spec")
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestPanicRecoveryReportsAnUndeliverablePanicOnStderr$")
	cmd.Env = append(os.Environ(), "GO_SPECS_PANIC_REPORT_STDERR_HELPER=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected the subprocess to survive, got %v: %s", err, output)
	}
	got := string(output)
	if !strings.Contains(got, undeliverablePanicPrefix) {
		t.Fatalf("expected an undeliverable-panic notice naming why it could not be reported, got: %s", got)
	}
	if !strings.Contains(got, "undeliverable boom") {
		t.Fatalf("expected the original panic value in the notice, got: %s", got)
	}
}

// TestPanicRecoveryStillReportsThroughALiveBackend proves the guard did not cost the ordinary path:
// a live backend still receives the panic in the unchanged "message\noutput" wire format.
func TestPanicRecoveryStillReportsThroughALiveBackend(t *testing.T) {
	for _, engine := range panicRecoveryEngines() {
		backend := &controlledBackend{}
		ctx := &Context{backend: backend}
		engine.run(ctx, func(*Context) { panic("boom") })

		if len(backend.errors) != 1 {
			t.Fatalf("%s: expected exactly one reported failure, got %v", engine.name, backend.errors)
		}
		if !strings.Contains(backend.errors[0], "boom") {
			t.Fatalf("%s: expected the reported failure to name the panic value, got %q", engine.name, backend.errors[0])
		}
		if !ctx.failed {
			t.Fatalf("%s: expected the context to be recorded as failed", engine.name)
		}
	}
}

// TestPanicRecoverySurvivesABackendThatPanicsWhileReporting covers the residual case the released
// runnableBackend check cannot see: a backend that looks live and blows up inside Errorf. Recovery
// must absorb that too rather than let it escape the recovery defer.
func TestPanicRecoverySurvivesABackendThatPanicsWhileReporting(t *testing.T) {
	ctx := &Context{backend: panickingBackend{}}
	runStepRecovered(ctx, func(*Context) { panic("boom") }, "spec")

	if !ctx.failed {
		t.Fatal("expected the recovered panic to be recorded as a failure even though reporting it panicked")
	}
}

// panickingBackend is a testBackend whose reporting methods panic, standing in for a backend that is
// live by every check available yet unusable in practice.
type panickingBackend struct{}

func (panickingBackend) Helper()                      {}
func (panickingBackend) FailNow()                     { panic("FailNow on a dead backend") }
func (panickingBackend) Fatal(...any)                 { panic("Fatal on a dead backend") }
func (panickingBackend) Fatalf(string, ...any)        { panic("Fatalf on a dead backend") }
func (panickingBackend) Error(...any)                 { panic("Error on a dead backend") }
func (panickingBackend) Errorf(string, ...any)        { panic("Errorf on a dead backend") }
func (panickingBackend) Log(...any)                   {}
func (panickingBackend) Logf(string, ...any)          {}
func (panickingBackend) Name() string                 { return "panickingBackend" }
func (panickingBackend) Cleanup(func())               {}
func (panickingBackend) Run(string, func(testing.TB)) {}
