package specs

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

// This file pins #173: a parallel group with N independently failing specs surfaces all N, and
// surfacing them does not implicitly end the enclosing test the way the old Fatalf-based report did.
//
// It has two halves. The unit half drives reportFailures and the three parallel engines against
// collectingReporter, which records every call instead of failing; the end-to-end half runs the
// fixture in testdata/parallel_multifailure as a real `go test` subprocess, for the same reason
// parallel_attribution_test.go does: only a real *testing.T performs the runtime.Goexit that the
// "no implicit fail-fast" half of the contract is about, and only real output proves what a plain
// `go test` run shows a user.
//
// Both halves run under CI's `go test -race ./...`, where the worker goroutines writing
// results[specIndex] and the reporting goroutine reading them are exactly the shared state the
// detector is there to watch.

const parallelMultifailureFixtureDir = "testdata/parallel_multifailure"

// collectingReporter is a failureReporter that keeps every reported message instead of failing, so a
// test can assert on how many failures were surfaced and in which order. It also implements Fatalf —
// which failureReporter no longer requires — precisely so a test can assert it stays untouched:
// Fatalf on a real *testing.T ends in runtime.Goexit, and reaching for it here is the exact defect
// #173 describes.
type collectingReporter struct {
	messages []string
	fatalfs  int
}

func (c *collectingReporter) Helper() {}

func (c *collectingReporter) Errorf(format string, args ...any) {
	c.messages = append(c.messages, fmt.Sprintf(format, args...))
}

func (c *collectingReporter) Fatalf(format string, args ...any) {
	c.fatalfs++
	c.messages = append(c.messages, fmt.Sprintf(format, args...))
}

// collectingBackend is the testBackend counterpart of collectingReporter: it keeps every reported
// message instead of failing, so parallelStep's reporting onto an enclosing Context can be asserted
// without a real *testing.T. capturingBackend (program_test.go) keeps only the last message, which
// is exactly the thing this file exists to stop assuming.
type collectingBackend struct {
	messages []string
}

func (b *collectingBackend) Helper()           {}
func (b *collectingBackend) FailNow()          { b.messages = append(b.messages, "fail now") }
func (b *collectingBackend) Fatal(args ...any) { b.messages = append(b.messages, fmt.Sprint(args...)) }
func (b *collectingBackend) Fatalf(format string, args ...any) {
	b.messages = append(b.messages, fmt.Sprintf(format, args...))
}
func (b *collectingBackend) Error(args ...any) { b.Fatal(args...) }
func (b *collectingBackend) Errorf(format string, args ...any) {
	b.Fatalf(format, args...)
}
func (b *collectingBackend) Log(args ...any)              {}
func (b *collectingBackend) Logf(string, ...any)          {}
func (b *collectingBackend) Name() string                 { return "" }
func (b *collectingBackend) Cleanup(func())               {}
func (b *collectingBackend) Run(string, func(testing.TB)) {}

// TestReportFailuresReportsEveryFailedRecordInSpecOrder is the contract at its narrowest: every
// record whose Failed bit is set is reported, in spec index order, and passing specs are skipped.
func TestReportFailuresReportsEveryFailedRecordInSpecOrder(t *testing.T) {
	results := []failureRecord{
		{Failed: true, Message: "first"},
		{},
		{Failed: true, Message: "third"},
		{Failed: true, Message: "fourth"},
	}

	rep := &collectingReporter{}
	reportFailures(rep, results)

	if len(rep.messages) != 3 {
		t.Fatalf("expected all three failures to be reported, got %d: %q", len(rep.messages), rep.messages)
	}
	for i, want := range []string{"spec[0]: first", "spec[2]: third", "spec[3]: fourth"} {
		if rep.messages[i] != want {
			t.Errorf("report %d: got %q, want %q", i, rep.messages[i], want)
		}
	}
}

// TestReportFailuresNeverReportsThroughFatalf pins the no-implicit-fail-fast half at the unit level.
// Fatalf is how the collapse used to happen: it reported one failure and then took the calling
// goroutine down with it, skipping every later group whether or not FailFast was asked for.
func TestReportFailuresNeverReportsThroughFatalf(t *testing.T) {
	rep := &collectingReporter{}

	reportFailures(rep, []failureRecord{{Failed: true, Message: "boom"}, {Failed: true, Message: "bang"}})

	if rep.fatalfs != 0 {
		t.Errorf("expected reportFailures to report through Errorf only, but Fatalf was called %d time(s)", rep.fatalfs)
	}
	if len(rep.messages) != 2 {
		t.Errorf("expected both failures reported, got %q", rep.messages)
	}
}

// TestReportFailuresReportsAFailureWithAnEmptyMessage keeps #175's invariant wired into the loop
// this change rewrites: the Failed bit is the failure, not a non-empty message.
func TestReportFailuresReportsAFailureWithAnEmptyMessage(t *testing.T) {
	rep := &collectingReporter{}

	reportFailures(rep, []failureRecord{{}, {Failed: true}, {Failed: true, Message: "loud"}})

	if len(rep.messages) != 2 {
		t.Fatalf("expected the silent failure and the loud one to both be reported, got %q", rep.messages)
	}
	if rep.messages[0] != "spec[1]: " {
		t.Errorf("expected the empty-message failure to be reported as spec[1], got %q", rep.messages[0])
	}
}

// TestParallelBackendKeepsTheFirstFailureOfASpec pins the first-write-wins half of #173's third
// acceptance criterion. Two non-fatal failures in one spec body (Error/Errorf never abort) used to
// leave the second one in the slot; the first is the one that explains the spec, and it is what the
// sequential path keeps too — Context.failure.Failed is sticky, and recoverParallelSpecFailure
// already refuses to overwrite an existing record.
func TestParallelBackendKeepsTheFirstFailureOfASpec(t *testing.T) {
	results := make([]failureRecord, 1)
	backend := &parallelBackend{specIndex: 0, results: &results}

	backend.Error("first failure")
	backend.Errorf("second %s", "failure")

	if !results[0].Failed {
		t.Fatal("expected the spec to be recorded as failed")
	}
	if results[0].Message != "first failure" {
		t.Errorf("expected the first failure to win, got %q", results[0].Message)
	}
}

// TestParallelBackendDoesNotLetALaterFailureOverwriteASilentOne is the same rule at the boundary
// #175 had to fix elsewhere: a failure with no message is still a recorded failure, so a later one
// must not be treated as filling an empty slot.
func TestParallelBackendDoesNotLetALaterFailureOverwriteASilentOne(t *testing.T) {
	results := make([]failureRecord, 1)
	backend := &parallelBackend{specIndex: 0, results: &results}

	backend.Error()
	backend.Error("second failure")

	if results[0].Message != "" {
		t.Errorf("expected the silent first failure to be kept, got %q", results[0].Message)
	}
}

// TestRunParallelReportsEveryFailingSpec covers MinimalRunner.RunParallel, and
// TestRunParallelBatchedReportsEveryFailingSpec its batched sibling: #173 asks for every parallel
// engine to hold the same contract, and all three reach it through reportFailures.
func TestRunParallelReportsEveryFailingSpec(t *testing.T) {
	rep := runParallelWithThreeFailingSpecs(t, func(r *MinimalRunner, tb failureReporter) {
		r.RunParallel(tb, 3)
	})
	assertReportedSpecIndexes(t, rep, "spec[0]", "spec[2]", "spec[3]")
}

func TestRunParallelBatchedReportsEveryFailingSpec(t *testing.T) {
	rep := runParallelWithThreeFailingSpecs(t, func(r *MinimalRunner, tb failureReporter) {
		r.RunParallelBatched(tb, 3, 2)
	})
	assertReportedSpecIndexes(t, rep, "spec[0]", "spec[2]", "spec[3]")
}

// runParallelWithThreeFailingSpecs builds one four-spec runner whose specs 0, 2 and 3 fail
// independently, runs it through run, and returns the reporter. The passing spec at index 1 keeps
// the assertions honest about which indexes are reported.
func runParallelWithThreeFailingSpecs(t *testing.T, run func(*MinimalRunner, failureReporter)) *collectingReporter {
	t.Helper()

	r := NewMinimalRunner(8)
	r.Add("s0", func(ctx *Context) { ctx.backend.Errorf("alpha") })
	r.Add("s1", func(*Context) {})
	r.Add("s2", func(ctx *Context) { ctx.backend.Errorf("beta") })
	r.Add("s3", func(ctx *Context) { ctx.backend.Errorf("gamma") })

	rep := &collectingReporter{}
	run(r, rep)
	return rep
}

// TestBytecodeRunParallelReportsEveryFailingSpec covers the third engine, the bytecode worker pool.
func TestBytecodeRunParallelReportsEveryFailingSpec(t *testing.T) {
	b := NewBCBuilder(8)
	b.AddSpec(func(ctx *Context) { ctx.backend.Errorf("alpha") })
	b.AddSpec(func(*Context) {})
	b.AddSpec(func(ctx *Context) { ctx.backend.Errorf("beta") })

	rep := &collectingReporter{}
	NewBytecodeRunner(b.BuildBC()).RunParallel(rep, 3)

	assertReportedSpecIndexes(t, rep, "spec[0]", "spec[2]")
}

// TestParallelStepReportsEveryFailingSpec covers ItParallel's own group step, which reports onto the
// enclosing Context's backend rather than a testing.TB.
func TestParallelStepReportsEveryFailingSpec(t *testing.T) {
	backend := &collectingBackend{}
	ctx := &Context{backend: backend}

	run := parallelStep([]step{
		runAll([]step{func(ctx *Context) { ctx.backend.Error("alpha") }}),
		runAll([]step{func(*Context) {}}),
		runAll([]step{func(ctx *Context) { ctx.backend.Error("beta") }}),
	}, []string{"p1", "p2", "p3"}, nil)
	run(ctx)

	if len(backend.messages) != 2 {
		t.Fatalf("expected both failing ItParallel specs to be reported, got %q", backend.messages)
	}
	if !strings.Contains(backend.messages[0], "spec[0]") || !strings.Contains(backend.messages[1], "spec[2]") {
		t.Errorf("expected spec[0] then spec[2], got %q", backend.messages)
	}
	if !ctx.hasFailed() {
		t.Error("expected the group's failure to be folded back onto the enclosing Context")
	}
}

// assertReportedSpecIndexes checks that exactly the given spec indexes were reported, in order.
func assertReportedSpecIndexes(t *testing.T, rep *collectingReporter, want ...string) {
	t.Helper()

	if rep.fatalfs != 0 {
		t.Errorf("expected reporting through Errorf only, but Fatalf was called %d time(s)", rep.fatalfs)
	}
	if len(rep.messages) != len(want) {
		t.Fatalf("expected %d failures reported, got %d: %q", len(want), len(rep.messages), rep.messages)
	}
	for i, prefix := range want {
		if !strings.HasPrefix(rep.messages[i], prefix) {
			t.Errorf("report %d: got %q, want it to start with %q", i, rep.messages[i], prefix)
		}
	}
}

// TestParallelGroupFailureVisibility is the end-to-end half: it runs the fixture package with a real
// `go test` and asserts on what a user actually sees.
func TestParallelGroupFailureVisibility(t *testing.T) {
	output := runParallelMultifailureFixture(t)

	t.Run("every failing spec in a parallel group reaches the output", func(t *testing.T) {
		for _, marker := range []string{"MARKER_ALPHA", "MARKER_BETA", "MARKER_GAMMA"} {
			if !strings.Contains(output, marker) {
				t.Errorf("%s is missing from the output; a parallel group collapsed its failures\nfull output:\n%s",
					marker, output)
			}
		}
		for _, index := range []string{"spec[0]", "spec[1]", "spec[2]"} {
			if !strings.Contains(output, index) {
				t.Errorf("%s is missing from the output\nfull output:\n%s", index, output)
			}
		}
	})

	t.Run("reporting them does not skip the groups declared after", func(t *testing.T) {
		if !strings.Contains(output, "MARKER_LATER_GROUP") {
			t.Errorf("the group after the failing parallel one never ran: reporting a parallel failure "+
				"implicitly ended the test\nfull output:\n%s", output)
		}
	})

	t.Run("reporting them does not end the test function itself", func(t *testing.T) {
		for _, marker := range []string{"MARKER_DIRECT_FIRST", "MARKER_DIRECT_SECOND"} {
			if !strings.Contains(output, marker) {
				t.Errorf("%s is missing: RunParallel collapsed its failures\nfull output:\n%s", marker, output)
			}
		}
		if !strings.Contains(output, "MARKER_AFTER_RUNPARALLEL") {
			t.Errorf("the statement after RunParallel never ran: reporting ended the test function "+
				"with runtime.Goexit, without FailFast being asked for\nfull output:\n%s", output)
		}
	})

	t.Run("FailFast still stops after the failing parallel group", func(t *testing.T) {
		if strings.Contains(output, "MARKER_FAILFAST_LATER_GROUP") {
			t.Errorf("FailFast did not stop before the next group\nfull output:\n%s", output)
		}
		if !strings.Contains(output, "MARKER_FAILFAST_FIRST_GROUP") {
			t.Errorf("the FailFast fixture's own parallel failure was never reported\nfull output:\n%s", output)
		}
	})
}

// runParallelMultifailureFixture runs `go test` over the fixture package and returns its combined
// output. The fixture is expected to fail, so a non-zero exit status is the success case here.
func runParallelMultifailureFixture(t *testing.T) string {
	t.Helper()

	goBin := findGoBinary(t)

	cmd := exec.Command(goBin, "test", "-count=1", "-v", "./"+parallelMultifailureFixtureDir)
	out, err := cmd.CombinedOutput()
	output := string(out)
	if err == nil {
		t.Fatalf("expected the parallel multifailure fixture to fail, but it passed:\n%s", output)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("running go test on the parallel multifailure fixture: %v\n%s", err, output)
	}
	return output
}
