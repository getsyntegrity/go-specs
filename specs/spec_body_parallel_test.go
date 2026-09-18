package specs

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// This file pins the semantics of calling the Go testing primitive ctx.T.Parallel() from inside a
// sequential spec body (#172).
//
// Sequential execution hands every spec the same mutable *Context, re-pointing its backend/T/tb at
// the current subtest for the duration of the body and restoring them on the way out. t.Parallel()
// breaks the single assumption that makes that safe: it pauses the subtest goroutine and lets
// t.Run return *before* the body finished, so the restore never happens at the moment the runner
// believes it did. Two failure shapes follow, and the second is the dangerous one:
//
//   - the next spec is created under the previous spec's still-installed subtest, producing nested
//     identities such as "outer/spec-0/spec-1" that no reporter or -run pattern can address; and
//   - the runner finishes and releases the shared Context back to the pool while the paused bodies
//     have not run yet, so every assertion they later make sees a nil backend, returns silently,
//     and the suite stays green while proving nothing.
//
// The supported way to run specs concurrently is ItParallel / RunParallel, which give each spec its
// own Context and a parallelBackend that never exposes a live *testing.T. ctx.T.Parallel() is
// therefore unsupported, and must fail loudly the moment it is detected rather than corrupt the run.

// parallelHelperEnv gates the re-exec helper mode used by the real-process tests below.
const parallelHelperEnv = "GO_SPECS_SPEC_BODY_PARALLEL_HELPER"

// runParallelHelper re-execs this test binary with -test.v, running only the named test with the
// helper mode enabled, and returns the transcript plus whether the child exited non-zero.
//
// A subprocess is required, not incidental: the guarantee under test is that the *run* fails and
// says why, which can only be observed from outside the failing test's own process.
func runParallelHelper(t *testing.T, testName string) (output string, failed bool) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.v", "-test.run=^"+testName+"$")
	cmd.Env = append(os.Environ(), parallelHelperEnv+"=1")
	raw, err := cmd.CombinedOutput()
	return string(raw), err != nil
}

// assertParallelRejected checks the transcript reports the unsupported operation by name, points at
// the supported alternative, and does not leave the run green.
func assertParallelRejected(t *testing.T, output string, failed bool) {
	t.Helper()
	if !failed {
		t.Fatalf("expected the run to fail once a spec body called ctx.T.Parallel(), got a passing run:\n%s", output)
	}
	if !strings.Contains(output, "ctx.T.Parallel()") {
		t.Fatalf("expected the diagnostic to name ctx.T.Parallel() as the unsupported operation, got:\n%s", output)
	}
	if !strings.Contains(output, "ItParallel") {
		t.Fatalf("expected the diagnostic to point at ItParallel as the supported alternative, got:\n%s", output)
	}
}

// assertNoNestedSpecIdentity checks no spec was opened underneath another spec's subtest. Every
// spec in the helper suites is a sibling, so any "<name>/<name>" pair in the transcript is the
// accidental nesting #172 reproduces.
func assertNoNestedSpecIdentity(t *testing.T, output string) {
	t.Helper()
	for _, nested := range []string{"alpha/bravo", "bravo/charlie", "alpha/charlie"} {
		if strings.Contains(output, nested) {
			t.Fatalf("expected sibling specs to stay siblings, found nested identity %q in:\n%s", nested, output)
		}
	}
}

// parallelHelperProgram is the Builder/Program spec tree whose middle spec calls the unsupported
// ctx.T.Parallel(). The bodies print markers so the transcript shows how far execution got.
func parallelHelperProgram() *Program {
	b := NewBuilder()
	b.Describe("suite", func() {
		b.It("alpha", func(*Context) { fmt.Println("RAN alpha") })
		b.It("bravo", func(ctx *Context) {
			fmt.Println("RAN bravo")
			ctx.T.Parallel()
			fmt.Println("RESUMED bravo")
			ctx.Expect(1).ToEqual(2)
		})
		b.It("charlie", func(*Context) { fmt.Println("RAN charlie") })
	})
	return b.Build()
}

// parallelHelperSuite is parallelHelperProgram's Spec/ExecutionPlan equivalent, so both sequential
// execution models are held to the same contract.
func parallelHelperSuite(s *Spec) {
	s.It("alpha", func(*Context) { fmt.Println("RAN alpha") })
	s.It("bravo", func(ctx *Context) {
		fmt.Println("RAN bravo")
		ctx.T.Parallel()
		fmt.Println("RESUMED bravo")
		ctx.Expect(1).ToEqual(2)
	})
	s.It("charlie", func(*Context) { fmt.Println("RAN charlie") })
}

// TestRunnerRejectsSpecBodyParallelRealProcess proves the Builder/Runner sequential model refuses
// ctx.T.Parallel() instead of corrupting the shared Context (#172).
func TestRunnerRejectsSpecBodyParallelRealProcess(t *testing.T) {
	if os.Getenv(parallelHelperEnv) == "1" {
		NewRunner(parallelHelperProgram()).Run(t)
		return
	}
	output, failed := runParallelHelper(t, "TestRunnerRejectsSpecBodyParallelRealProcess")
	assertParallelRejected(t, output, failed)
	assertNoNestedSpecIdentity(t, output)
}

// TestSpecRejectsSpecBodyParallelRealProcess proves the Describe/ExecutionPlan sequential model
// refuses ctx.T.Parallel() on the same terms (#172).
func TestSpecRejectsSpecBodyParallelRealProcess(t *testing.T) {
	if os.Getenv(parallelHelperEnv) == "1" {
		Describe(t, "suite", parallelHelperSuite)
		return
	}
	output, failed := runParallelHelper(t, "TestSpecRejectsSpecBodyParallelRealProcess")
	assertParallelRejected(t, output, failed)
	assertNoNestedSpecIdentity(t, output)
}

// TestSpecBodyParallelNeverSilencesAssertions pins the acceptance criterion that matters most: a
// paused body's assertions must never become no-ops. The helper's bravo spec asserts 1 == 2 after
// it resumes, so a transcript that reports failure only from the guard — and never from the
// resumed assertion or the guard's own abort — would mean the Context was recycled underneath it.
//
// The resumed body keeps the subtest it was bound to, so its failure is attributed there rather
// than swallowed.
func TestSpecBodyParallelNeverSilencesAssertions(t *testing.T) {
	if os.Getenv(parallelHelperEnv) == "1" {
		NewRunner(parallelHelperProgram()).Run(t)
		return
	}
	output, failed := runParallelHelper(t, "TestSpecBodyParallelNeverSilencesAssertions")
	if !failed {
		t.Fatalf("expected a failing run, got:\n%s", output)
	}
	if strings.Contains(output, "RESUMED bravo") && !strings.Contains(output, "expected 1 to equal 2") {
		t.Fatalf("the paused body resumed but its assertion was silently dropped; got:\n%s", output)
	}
}

// TestPoisonedContextIsNotRecycled pins the lifecycle half of the fix: releasing a poisoned Context
// must not reset it or return it to contextPool, because the parked body still points at it. If
// release blanked the backend, that body's assertions would hit the c.backend == nil guards and
// vanish — which is the silent-green failure #172 is really about.
func TestPoisonedContextIsNotRecycled(t *testing.T) {
	backend := asTestBackend(t)
	defer putTestBackend(backend)

	ctx, release := acquireContext(backend)
	ctx.poison()
	release()

	if ctx.backend == nil {
		t.Fatal("expected a poisoned Context to keep its backend after release so a parked body still reports, got nil")
	}
	if ctx.T == nil {
		t.Fatal("expected a poisoned Context to keep its *testing.T after release, got nil")
	}
}

// TestResetClearsPoison guards the one way a poisoned Context could still leak into a later spec:
// poison must not survive a Reset, so a Context that somehow re-enters the pool is handed out clean.
func TestResetClearsPoison(t *testing.T) {
	ctx := &Context{}
	ctx.poison()
	if !ctx.poisoned {
		t.Fatal("expected poison() to mark the Context")
	}
	ctx.Reset(nil)
	if ctx.poisoned {
		t.Fatal("expected Reset to clear the poison flag")
	}
}

// TestUnsupportedSpecBodyParallelMessage pins the diagnostic's contract: it names the unsupported
// operation, the spec it happened in, and the supported alternative.
func TestUnsupportedSpecBodyParallelMessage(t *testing.T) {
	msg := unsupportedSpecBodyParallelMessage("suite/bravo")
	for _, want := range []string{"ctx.T.Parallel()", "suite/bravo", "ItParallel", "RunParallel"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("expected the diagnostic to mention %q, got:\n%s", want, msg)
		}
	}
	if unnamed := unsupportedSpecBodyParallelMessage(""); !strings.Contains(unnamed, "a sequential spec body") {
		t.Fatalf("expected a readable fallback when the spec has no name, got:\n%s", unnamed)
	}
}
