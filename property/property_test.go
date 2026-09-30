package property_test

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/getsyntegrity/go-specs/assert"
	"github.com/getsyntegrity/go-specs/property"
	"pgregory.net/rapid"
)

func sum(xs []int) int {
	s := 0
	for _, x := range xs {
		s += x
	}
	return s
}

// sumBelow100 is a deliberately false invariant: a sum of non-negative numbers is not always < 100.
func sumBelow100(p *property.T) {
	xs := property.Draw(p, rapid.SliceOf(rapid.IntRange(0, 1000)), "xs")
	p.Expect(sum(xs)).To(assert.BeLessThan(100))
}

func TestPassingPropertyRunsTheConfiguredNumberOfChecks(t *testing.T) {
	calls := 0
	res := property.Run(func(p *property.T) {
		calls++
		xs := property.Draw(p, rapid.SliceOf(rapid.Int()), "xs")
		rev := slices.Clone(xs)
		slices.Reverse(rev)
		slices.Reverse(rev)
		p.Expect(slices.Equal(rev, xs)).To(assert.BeTrue())
	}, property.WithChecks(37), property.WithSeed(7), property.WithoutFailFile())

	if res.Outcome != property.Passed {
		t.Fatalf("outcome = %v, want Passed\n%s", res.Outcome, res.Report)
	}
	if res.Checks != 37 || calls != 37 {
		t.Fatalf("Checks = %d, property calls = %d, want 37 and 37", res.Checks, calls)
	}
	if res.Rejected != 0 {
		t.Fatalf("Rejected = %d, want 0", res.Rejected)
	}
}

func TestFailingInvariantShrinksToASmallerCounterexample(t *testing.T) {
	res := property.Run(sumBelow100, property.WithSeed(42), property.WithoutFailFile())

	if res.Outcome != property.Failed {
		t.Fatalf("outcome = %v, want Failed\n%s", res.Outcome, res.Report)
	}
	if len(res.Counterexample) != 1 || res.Counterexample[0].Label != "xs" {
		t.Fatalf("counterexample = %+v, want one draw labelled xs", res.Counterexample)
	}
	if got, want := res.Counterexample[0].Value, "[]int{100}"; got != want {
		t.Fatalf("shrunk counterexample = %s, want %s", got, want)
	}
	if len(res.Original) != 1 || len(res.Original[0].Value) <= len(res.Counterexample[0].Value) {
		t.Fatalf("original %+v is not larger than the shrunk %+v", res.Original, res.Counterexample)
	}
	if len(res.Failures) != 1 || !strings.Contains(res.Failures[0], "100") {
		t.Fatalf("failures = %q, want exactly one assertion message naming the sum", res.Failures)
	}
	if res.Panic != nil {
		t.Fatalf("an assertion failure reported a panic: %+v", res.Panic)
	}
}

func TestReplayWithTheReportedSeedIsDeterministic(t *testing.T) {
	first := property.Run(sumBelow100, property.WithSeed(42), property.WithoutFailFile())
	if first.Outcome != property.Failed || first.Seed == 0 {
		t.Fatalf("first run: outcome = %v, seed = %d\n%s", first.Outcome, first.Seed, first.Report)
	}

	for i := 0; i < 3; i++ {
		again := property.Run(sumBelow100, property.WithSeed(first.Seed), property.WithoutFailFile())
		if again.Outcome != property.Failed {
			t.Fatalf("replay %d: outcome = %v, want Failed", i, again.Outcome)
		}
		if !reflect.DeepEqual(again.Original, first.Original) {
			t.Fatalf("replay %d: original %+v, want %+v", i, again.Original, first.Original)
		}
		if !reflect.DeepEqual(again.Counterexample, first.Counterexample) {
			t.Fatalf("replay %d: counterexample %+v, want %+v", i, again.Counterexample, first.Counterexample)
		}
		if again.Checks != 0 {
			t.Fatalf("replay %d: Checks = %d, want 0 (the reported seed fails on its first input)", i, again.Checks)
		}
	}
}

func TestSameSeedGivesTheSameResult(t *testing.T) {
	a := property.Run(sumBelow100, property.WithSeed(99), property.WithoutFailFile())
	b := property.Run(sumBelow100, property.WithSeed(99), property.WithoutFailFile())
	if a.Seed != b.Seed || a.Checks != b.Checks || !reflect.DeepEqual(a.Counterexample, b.Counterexample) {
		t.Fatalf("same seed, different result:\n%s\n---\n%s", a.Report, b.Report)
	}
}

func TestRejectionExhaustionIsNotAFailure(t *testing.T) {
	res := property.Run(func(p *property.T) {
		n := property.Draw(p, rapid.Int(), "n")
		p.Assume(n == n+1) // never true: every input is rejected
		p.Expect(true).To(assert.BeFalse())
	}, property.WithChecks(20), property.WithSeed(1), property.WithoutFailFile())

	if res.Outcome != property.Exhausted {
		t.Fatalf("outcome = %v, want Exhausted\n%s", res.Outcome, res.Report)
	}
	if res.Checks != 0 || res.Rejected == 0 {
		t.Fatalf("Checks = %d, Rejected = %d, want 0 valid and some rejected", res.Checks, res.Rejected)
	}
	if len(res.Failures) != 0 || res.Panic != nil || len(res.Counterexample) != 0 {
		t.Fatalf("exhaustion carried failure details: %+v", res)
	}
}

func TestRejectedInputsAreCountedAndDoNotFail(t *testing.T) {
	res := property.Run(func(p *property.T) {
		n := property.Draw(p, rapid.IntRange(0, 9), "n")
		if n%2 == 1 {
			p.Reject("odd input %d", n)
		}
		p.Expect(n % 2).To(assert.Equal(0))
	}, property.WithChecks(30), property.WithSeed(3), property.WithoutFailFile())

	if res.Outcome != property.Passed {
		t.Fatalf("outcome = %v, want Passed\n%s", res.Outcome, res.Report)
	}
	if res.Checks != 30 || res.Rejected == 0 {
		t.Fatalf("Checks = %d, Rejected = %d, want 30 valid and some rejected", res.Checks, res.Rejected)
	}
}

func TestPanicIsDistinguishedFromAnAssertionFailure(t *testing.T) {
	res := property.Run(func(p *property.T) {
		n := property.Draw(p, rapid.IntRange(0, 1000), "n")
		if n > 10 {
			panic(errors.New("boom"))
		}
	}, property.WithSeed(5), property.WithoutFailFile())

	if res.Outcome != property.Panicked {
		t.Fatalf("outcome = %v, want Panicked\n%s", res.Outcome, res.Report)
	}
	if res.Panic == nil || !strings.Contains(res.Panic.Value, "boom") || res.Panic.Stack == "" {
		t.Fatalf("panic info = %+v, want the value and a stack", res.Panic)
	}
	if len(res.Failures) != 0 {
		t.Fatalf("a panic was also reported as an assertion failure: %q", res.Failures)
	}
	if len(res.Counterexample) != 1 || res.Counterexample[0].Value != "11" {
		t.Fatalf("counterexample = %+v, want n = 11", res.Counterexample)
	}
}

func TestFailureIsIsolatedFromTheSurroundingTestAndLaterRuns(t *testing.T) {
	bad := property.Run(sumBelow100, property.WithSeed(42), property.WithoutFailFile())
	if bad.Outcome != property.Failed {
		t.Fatalf("setup: outcome = %v, want Failed", bad.Outcome)
	}
	if t.Failed() {
		t.Fatal("a falsified property marked the surrounding test failed")
	}

	good := property.Run(func(p *property.T) {
		n := property.Draw(p, rapid.IntRange(0, 5), "n")
		p.Expect(n).To(assert.BeLessThan(6))
	}, property.WithChecks(10), property.WithSeed(42), property.WithoutFailFile())
	if good.Outcome != property.Passed || len(good.Failures) != 0 {
		t.Fatalf("a later run inherited failure state: %+v", good)
	}
}

func TestPerRunStateDoesNotAccumulateAcrossShrinkingRuns(t *testing.T) {
	// Shrinking runs the property many times, each failing; only the counterexample's own run may
	// contribute failures and draws.
	res := property.Run(sumBelow100, property.WithSeed(42), property.WithoutFailFile())
	if len(res.Failures) != 1 || len(res.Counterexample) != 1 {
		t.Fatalf("failures = %d, draws = %d, want 1 and 1 (state leaked between runs)", len(res.Failures), len(res.Counterexample))
	}
}

func TestPersistedFailFileReplaysWithoutASeed(t *testing.T) {
	t.Chdir(t.TempDir())

	first := property.Run(sumBelow100, property.WithName("corpus"), property.WithSeed(42))
	if first.Outcome != property.Failed || first.FailFile == "" {
		t.Fatalf("first: outcome = %v, failfile = %q\n%s", first.Outcome, first.FailFile, first.Report)
	}
	if _, err := os.Stat(first.FailFile); err != nil {
		t.Fatalf("fail file not written: %v", err)
	}

	again := property.Run(sumBelow100, property.WithName("corpus"))
	if again.Outcome != property.Failed || again.FailFile != first.FailFile {
		t.Fatalf("replay: outcome = %v, failfile = %q, want Failed from %q", again.Outcome, again.FailFile, first.FailFile)
	}
	if !reflect.DeepEqual(again.Counterexample, first.Counterexample) {
		t.Fatalf("corpus replay counterexample %+v, want %+v", again.Counterexample, first.Counterexample)
	}

	explicit := property.Run(sumBelow100, property.WithName("other"), property.WithFailFile(first.FailFile))
	if explicit.Outcome != property.Failed || !reflect.DeepEqual(explicit.Counterexample, first.Counterexample) {
		t.Fatalf("explicit fail file: %+v", explicit)
	}
}

func TestReportPreservesCounterexampleAndReplayInstructions(t *testing.T) {
	res := property.Run(sumBelow100, property.WithName("sum-below-100"), property.WithSeed(42), property.WithoutFailFile())
	for _, want := range []string{
		"sum-below-100", "falsified", "xs = []int{100}", "seed", "property.WithSeed(", "-rapid.seed=", "expected",
	} {
		if !strings.Contains(res.Report, want) {
			t.Errorf("report lacks %q:\n%s", want, res.Report)
		}
	}
}

type recordingTB struct {
	name    string
	errors  []string
	helpers int
}

func (r *recordingTB) Helper()      { r.helpers++ }
func (r *recordingTB) Name() string { return r.name }
func (r *recordingTB) Errorf(format string, args ...any) {
	r.errors = append(r.errors, strings.TrimSpace(fmt.Sprintf(format, args...)))
}

func TestCheckReportsAFalsifiedPropertyOnTheTestAndStaysQuietOtherwise(t *testing.T) {
	bad := &recordingTB{name: "TestBad"}
	property.Check(bad, sumBelow100, property.WithSeed(42), property.WithoutFailFile())
	if len(bad.errors) != 1 || !strings.Contains(bad.errors[0], "xs = []int{100}") {
		t.Fatalf("errors = %q, want one report with the counterexample", bad.errors)
	}
	if !strings.Contains(bad.errors[0], "TestBad") {
		t.Fatalf("report does not default to the test name: %q", bad.errors[0])
	}

	ok := &recordingTB{name: "TestOK"}
	property.Check(ok, func(p *property.T) {
		p.Expect(property.Draw(p, rapid.Bool(), "b")).To(assert.BeOneOf(true, false))
	}, property.WithChecks(5), property.WithoutFailFile())
	if len(ok.errors) != 0 {
		t.Fatalf("a passing property reported %q", ok.errors)
	}
}

func TestExhaustionAndPanicAreReportedByCheck(t *testing.T) {
	ex := &recordingTB{name: "TestEx"}
	property.Check(ex, func(p *property.T) { p.Reject("never applicable") }, property.WithChecks(5), property.WithSeed(1), property.WithoutFailFile())
	if len(ex.errors) != 1 || !strings.Contains(ex.errors[0], "rejected") {
		t.Fatalf("errors = %q, want a rejection-exhaustion report", ex.errors)
	}

	pa := &recordingTB{name: "TestPanic"}
	property.Check(pa, func(p *property.T) { panic("kaboom") }, property.WithSeed(1), property.WithoutFailFile())
	if len(pa.errors) != 1 || !strings.Contains(pa.errors[0], "panic") || !strings.Contains(pa.errors[0], "kaboom") {
		t.Fatalf("errors = %q, want a panic report", pa.errors)
	}
}

func TestOptionsDoNotLeakIntoEngineFlags(t *testing.T) {
	read := func() string {
		return flag.Lookup("rapid.checks").Value.String() + "/" + flag.Lookup("rapid.seed").Value.String()
	}
	before := read()
	property.Run(func(p *property.T) {}, property.WithChecks(3), property.WithSeed(11), property.WithoutFailFile())
	if after := read(); after != before {
		t.Fatalf("engine flags changed from %s to %s", before, after)
	}
}

func TestGenerationLimitShrinkTimeAndStepsAreAccepted(t *testing.T) {
	res := property.Run(func(p *property.T) {
		property.Draw(p, rapid.IntRange(0, 3), "n")
	}, property.WithChecks(4), property.WithSteps(5), property.WithShrinkTime(1), property.WithSeed(2), property.WithoutFailFile())
	if res.Outcome != property.Passed || res.Checks != 4 {
		t.Fatalf("outcome = %v, checks = %d", res.Outcome, res.Checks)
	}
}
