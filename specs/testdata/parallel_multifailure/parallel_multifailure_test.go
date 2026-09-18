// Package parallel_multifailure_test is a fixture executed by TestParallelGroupFailureVisibility in
// the specs package via a real `go test` subprocess. It is under testdata/ so `go test ./...` never
// runs it directly — every test in here fails on purpose.
//
// It exists because the behaviour under test (#173) is what `go test` actually prints and how far it
// gets: whether a parallel group with N failing specs surfaces all N, and whether reporting them
// silently ends the whole test function the way a Fatalf-based report used to. Neither question can
// be answered against a fake reporter, which never performs runtime.Goexit.
//
// Every assertion carries a unique MARKER_* string. The parent test greps the subprocess output for
// each marker, so the expectations survive any rewording of the failure format.
package parallel_multifailure_test

import (
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
)

// TestEveryFailingParallelSpecIsReported is the core regression: three independently failing
// ItParallel specs in one group must each reach the output. Before #173 only spec[0] did.
func TestEveryFailingParallelSpecIsReported(t *testing.T) {
	b := specs.NewBuilder()
	b.Describe("a parallel group where every spec fails", func() {
		b.ItParallel("first fails", func(ctx *specs.Context) {
			specs.EqualTo(ctx, "MARKER_ALPHA", "expected_alpha")
		})
		b.ItParallel("second fails", func(ctx *specs.Context) {
			specs.EqualTo(ctx, "MARKER_BETA", "expected_beta")
		})
		b.ItParallel("third fails", func(ctx *specs.Context) {
			specs.EqualTo(ctx, "MARKER_GAMMA", "expected_gamma")
		})
	})
	specs.NewRunner(b.Build()).Run(t)
}

// TestParallelFailureDoesNotImplicitlyFailFast pins the second half of #173: reporting a parallel
// group's failures must not end the test function. Without FailFast, the group declared after the
// failing one still runs, so MARKER_LATER_GROUP reaches the output. A Fatalf-based report ends in
// runtime.Goexit and that marker never appears.
func TestParallelFailureDoesNotImplicitlyFailFast(t *testing.T) {
	b := specs.NewBuilder()
	b.Describe("a failing parallel group", func() {
		b.ItParallel("fails", func(ctx *specs.Context) {
			specs.EqualTo(ctx, "MARKER_FIRST_GROUP", "expected_first_group")
		})
	})
	b.Describe("the group declared after it", func() {
		b.It("still runs", func(ctx *specs.Context) {
			specs.EqualTo(ctx, "MARKER_LATER_GROUP", "expected_later_group")
		})
	})
	specs.NewRunner(b.Build()).Run(t)
}

// TestRunParallelDoesNotEndTheTestFunction is the sharpest form of the no-implicit-fail-fast
// contract, because nothing isolates it: MinimalRunner.RunParallel reports straight onto this
// *testing.T. A Fatalf-based report ends in runtime.Goexit, so the statement after it never runs and
// MARKER_AFTER_RUNPARALLEL never reaches the output — the test simply stops, with no FailFast asked
// for anywhere.
func TestRunParallelDoesNotEndTheTestFunction(t *testing.T) {
	r := specs.NewMinimalRunner(4)
	r.Add("first fails", func(ctx *specs.Context) {
		specs.EqualTo(ctx, "MARKER_DIRECT_FIRST", "expected_direct_first")
	})
	r.Add("second fails", func(ctx *specs.Context) {
		specs.EqualTo(ctx, "MARKER_DIRECT_SECOND", "expected_direct_second")
	})
	r.RunParallel(t, 2)
	t.Log("MARKER_AFTER_RUNPARALLEL")
}

// TestFailFastStillStopsAfterAParallelGroup is the other side of that contract: fail-fast must stay
// something the user asks for. With FailFast set, the group after the failing parallel one is
// skipped, so MARKER_FAILFAST_LATER_GROUP must never reach the output.
func TestFailFastStillStopsAfterAParallelGroup(t *testing.T) {
	b := specs.NewBuilder()
	b.Describe("a failing parallel group", func() {
		b.ItParallel("fails", func(ctx *specs.Context) {
			specs.EqualTo(ctx, "MARKER_FAILFAST_FIRST_GROUP", "expected_failfast_first_group")
		})
	})
	b.Describe("the group declared after it", func() {
		b.It("must not run", func(ctx *specs.Context) {
			specs.EqualTo(ctx, "MARKER_FAILFAST_LATER_GROUP", "expected_failfast_later_group")
		})
	})
	r := specs.NewRunner(b.Build())
	r.FailFast = true
	r.Run(t)
}
