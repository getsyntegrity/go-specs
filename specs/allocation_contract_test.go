package specs

import (
	"errors"
	"testing"
)

// Runner allocation contracts (issue #178).
//
// README.md and docs/ARCHITECTURE.md advertise "no per-spec allocation in the runner loop" and
// "zero allocations in the loop". Nothing enforced it: the benchmarks printed allocs/op, but a
// benchmark reporting 3 allocs/op instead of 0 still exits 0, so a regression could only be caught
// by a human reading a table.
//
// Scope. The *assertion* half of the allocation contract already lives in
// assertion_allocations_test.go, where TestAssertionAllocationsByValueShape pins the published
// per-form, per-value-shape table with exact counts in both directions (#177). That test is the
// authority on assertions and this file does not restate it -- an earlier draft of this file did,
// and got it wrong: it asserted zero for `ctx.Expect(str).ToEqual(str)`, which really costs two,
// and passed only because a string built from a literal inside the test function is folded away by
// the compiler before the assertion ever sees it. Duplicating a contract in weaker form is worse
// than not duplicating it, because the weaker copy is the one that goes green.
//
// What is left here is what that table does not reach: the runner loops, plus the two assertion
// shapes whose cost is structural rather than value-dependent (valueless matchers, and the error
// idiom the docs recommend).
//
// Wall-clock cost is never asserted anywhere: ns/op depends on the machine, and a shared CI runner
// is the worst place on earth to measure it. Allocation counts, by contrast, are a property of the
// generated code, so they are stable across machines and safe to gate on.

// allocContractRuns is the sample size handed to testing.AllocsPerRun. The counts pinned below are
// integers on every run, so this only needs to be large enough to amortise a stray one-off; it is
// not a statistical measurement.
const allocContractRuns = 100

// assertNoAllocations fails when f allocates at all. It reports the measured average rather than
// just "not zero", because the number tells you whether a regression added one allocation or a
// thousand.
func assertNoAllocations(t *testing.T, path string, f func()) {
	t.Helper()
	if got := testing.AllocsPerRun(allocContractRuns, f); got != 0 {
		t.Errorf("%s allocates %.1f times per call, want 0 -- this path is documented as allocation-free", path, got)
	}
}

// TestValuelessMatchersAllocateNothing covers the matchers that carry no expected value. They hold
// no state, so putting one behind the Matcher interface costs nothing, and the asserted value is a
// bool or a nil interface -- both of which the runtime converts for free at every width there is.
//
// That last point is why these rows are safe here while the value-shaped ones belong in
// TestAssertionAllocationsByValueShape: there is no wide variant of `true` for a compiler
// optimisation to hide. Matchers that *do* capture an expected value (Equal, NotEqual, Contain) are
// excluded, because their cost depends on the value and is pinned in that table instead.
func TestValuelessMatchersAllocateNothing(t *testing.T) {
	ctx, _ := newCapturedContext()
	var nilErr error

	assertNoAllocations(t, "ExpectT(ctx, true).To(BeTrue())", func() { ExpectT(ctx, true).To(BeTrue()) })
	assertNoAllocations(t, "ExpectT(ctx, false).To(BeFalse())", func() { ExpectT(ctx, false).To(BeFalse()) })
	assertNoAllocations(t, "ctx.Expect(nil error).To(BeNil())", func() { ctx.Expect(nilErr).To(BeNil()) })
}

// TestErrorClassificationAllocatesNothing pins the shape the docs recommend for errors: classify
// with errors.Is/errors.As and feed the boolean into a typed expectation. errors.Is is a real call
// the compiler cannot fold away, and what reaches the assertion is a bool, so this measures the
// idiom rather than a constant. If the recommended idiom ever stops being free, the recommendation
// is what needs revisiting.
func TestErrorClassificationAllocatesNothing(t *testing.T) {
	ctx, _ := newCapturedContext()
	sentinel := errors.New("boom")
	wrapped := errors.Join(sentinel)

	assertNoAllocations(t, "ExpectT(ctx, errors.Is(err, sentinel)).To(BeTrue())", func() {
		ExpectT(ctx, errors.Is(wrapped, sentinel)).To(BeTrue())
	})
}

// Suite sizes for the runner-loop contract. The contract is about *growth*, so what matters is the
// ratio between them, not either absolute number: 50x more specs must not buy a single extra
// allocation.
const (
	allocContractSmallSuite = 100
	allocContractLargeSuite = 5000
)

// assertRunnerLoopDoesNotAllocatePerSpec runs the same suite shape at two very different sizes and
// requires the allocation count not to grow. Pinning an absolute number instead would be brittle
// (one pooled buffer more or less is not a contract violation) and would miss the actual claim,
// which is that per-spec cost is zero: whatever fixed setup a run does, it must not scale with the
// number of specs.
func assertRunnerLoopDoesNotAllocatePerSpec(t *testing.T, runner string, run func(n int) func(testing.TB), tb testing.TB) {
	t.Helper()

	small, large := run(allocContractSmallSuite), run(allocContractLargeSuite)

	smallAllocs := testing.AllocsPerRun(allocContractRuns, func() { small(tb) })
	largeAllocs := testing.AllocsPerRun(allocContractRuns, func() { large(tb) })

	if largeAllocs > smallAllocs {
		perSpec := (largeAllocs - smallAllocs) / float64(allocContractLargeSuite-allocContractSmallSuite)
		t.Errorf("%s allocates per spec: %d specs cost %.1f allocations, %d specs cost %.1f (~%.4f per spec, want 0)",
			runner, allocContractSmallSuite, smallAllocs, allocContractLargeSuite, largeAllocs, perSpec)
	}
}

// TestMinimalRunnerLoopAllocatesNothingPerSpec pins the claim behind the runner benchmarks: the
// sequential loop calls each spec function directly, with a pooled Context, so a suite of 5000
// specs costs the same allocations as a suite of 100.
func TestMinimalRunnerLoopAllocatesNothingPerSpec(t *testing.T) {
	assertRunnerLoopDoesNotAllocatePerSpec(t, "MinimalRunner", func(n int) func(testing.TB) {
		r := NewMinimalRunner(n)
		for i := 0; i < n; i++ {
			r.Add("spec", func(ctx *Context) { EqualTo(ctx, 1, 1) })
		}
		return r.Run
	}, t)
}

// TestBlockRunnerLoopAllocatesNothingPerSpec is the same contract for the block-compiled runner,
// which groups specs into fixed-size blocks. Block size 16 is the middle of the three sizes the
// benchmarks measure.
func TestBlockRunnerLoopAllocatesNothingPerSpec(t *testing.T) {
	assertRunnerLoopDoesNotAllocatePerSpec(t, "BlockRunner", func(n int) func(testing.TB) {
		specs := make([]RunSpec, n)
		for i := range specs {
			specs[i] = RunSpec{Name: "spec", Fn: func(ctx *Context) { EqualTo(ctx, 1, 1) }}
		}
		fns, blocks := CompileBlocks(specs, 16)
		return NewBlockRunner(fns, blocks).Run
	}, t)
}

// flatTB is a testing.TB that reports failures to a real *testing.T but is not one itself.
//
// That distinction is the entire point. runnableBackend.Run only opens a real t.Run subtest when the
// TB it holds is a *testing.T; for any other TB it calls the spec function inline. The compiled
// Program runner is therefore allocation-free only on the flat path -- the subtest path allocates
// tens of times per spec inside the standard library, which is the documented price of per-spec
// test identity and not something this contract governs.
//
// testing.TB has an unexported private() method so external types cannot satisfy it; the interface
// is embedded rather than implemented, and the embedded value is the real *testing.T so that any
// method not overridden here still behaves. Only Helper is overridden, to keep the measured
// function free of bookkeeping the runners do not share with the other two.
type flatTB struct {
	testing.TB
}

func (flatTB) Helper() {}

// TestProgramRunnerLoopAllocatesNothingPerSpecOnTheFlatPath pins the same contract for the compiled
// execution plan, the runner the public Describe/It DSL builds. See flatTB for why the subtest path
// is excluded.
func TestProgramRunnerLoopAllocatesNothingPerSpecOnTheFlatPath(t *testing.T) {
	assertRunnerLoopDoesNotAllocatePerSpec(t, "compiled Program runner", func(n int) func(testing.TB) {
		b := NewBuilder()
		b.Describe("suite", func() {
			b.BeforeEach(func(*Context) {})
			for i := 0; i < n; i++ {
				b.It("spec", func(ctx *Context) { EqualTo(ctx, 1, 1) })
			}
		})
		return NewRunner(b.Build()).Run
	}, flatTB{TB: t})
}
