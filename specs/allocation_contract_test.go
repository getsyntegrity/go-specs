package specs

import (
	"errors"
	"testing"
)

// Allocation contracts (issue #178).
//
// README.md, docs/ARCHITECTURE.md and docs/BENCHMARKS.md all advertise "zero allocations on the
// assertion fast path" and "no per-spec allocation in the runner loop". Until now nothing enforced
// either claim: the benchmarks printed allocs/op, but a benchmark that prints 3 allocs/op instead
// of 0 still exits 0, so a regression could only be caught by a human reading a table.
//
// These tests turn the subset of those claims that is a *supported guarantee* into a gate. They are
// deliberately narrow. A path appears here only when allocating nothing is an intentional design
// commitment we would treat a regression in as a bug, not merely something that happens to measure
// zero today. BENCHMARKS.md records which claims are contractual (pinned here) and which are
// observational (measured, never asserted).
//
// Wall-clock cost is never asserted anywhere: ns/op depends on the machine, and a shared CI runner
// is the worst place on earth to measure it. Allocation counts, by contrast, are a property of the
// generated code, so they are stable across machines and safe to gate on.
//
// TestPassingAssertionsAllocateNothingOnTheFastPath in expectation_reuse_test.go is the older,
// narrower sibling of the assertion contracts below: it guards the specific regression that
// removing the Expectation pool could have introduced (#170), on int values only. It stays where
// its story is. This file widens the surface to other value shapes, the matchers and the runners.

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

// allocContractPoint is a comparable struct wider than a machine word. Width is the point: it is
// the shape that used to cost an allocation on the typed path, and pinning it on both EqualTo and
// ExpectT is what makes "for a T of any size" a gate rather than a sentence in a README.
type allocContractPoint struct {
	X int
	Y string
}

// TestEqualToAllocatesNothingForAnyComparable pins the strongest form of the headline claim.
// EqualTo is a free generic function: it compares its two T values with == and never stores them
// anywhere, so nothing is boxed and nothing can escape, whatever T is. That holds for a comparable
// struct just as it does for an int, which is what makes this the path to reach for in a hot loop.
func TestEqualToAllocatesNothingForAnyComparable(t *testing.T) {
	ctx := NewContext(t)
	text := "syntegrity"
	value := allocContractPoint{X: 7, Y: "seven"}

	assertNoAllocations(t, "EqualTo(ctx, int, int)", func() { EqualTo(ctx, 42, 42) })
	assertNoAllocations(t, "EqualTo(ctx, string, string)", func() { EqualTo(ctx, text, "syntegrity") })
	assertNoAllocations(t, "EqualTo(ctx, struct, struct)", func() {
		EqualTo(ctx, value, allocContractPoint{X: 7, Y: "seven"})
	})
}

// TestExpectTAllocatesNothingForAnyComparable pins the fluent typed form at the same strength as
// EqualTo above: zero allocations for a comparable T of any size, structs included.
//
// The guarantee has two independent sources, and both must hold. ExpectT holds the value in
// typedExpectation[T].actual, which is a T and not an `any`, so there is no interface conversion to
// heap-allocate a value the runtime does not hand out for free (#177). The handle behind it does
// not escape the assertion that consumes it, so escape analysis stack-allocates it. Neither source
// depends on the width of T, which is why the wide struct belongs here rather than in an
// observational footnote.
//
// A wide comparable struct used to be excluded from this contract, back when the typed handle held
// a *Expectation whose `actual` field was an `any` and a struct too wide to stay on the stack was
// boxed on the heap. That is no longer how the code works, and the old exception is gone with it.
//
// To(Matcher) is the one typed path still outside this contract, and for a reason no rewrite here
// can remove: Matcher is Match(any), so the value must become an interface before a matcher can see
// it. That cost is measured by TestAssertionAllocationsByValueShape and recorded in BENCHMARKS.md
// as an observation.
func TestExpectTAllocatesNothingForAnyComparable(t *testing.T) {
	ctx := NewContext(t)
	text := "syntegrity"
	value := allocContractPoint{X: 7, Y: "seven"}

	assertNoAllocations(t, "ExpectT(ctx, int).ToEqual(int)", func() { ExpectT(ctx, 42).ToEqual(42) })
	assertNoAllocations(t, "ExpectT(ctx, string).ToEqual(string)", func() { ExpectT(ctx, text).ToEqual("syntegrity") })
	assertNoAllocations(t, "ExpectT(ctx, bool).ToEqual(bool)", func() { ExpectT(ctx, true).ToEqual(true) })
	assertNoAllocations(t, "ExpectT(ctx, struct).ToEqual(struct)", func() {
		ExpectT(ctx, value).ToEqual(allocContractPoint{X: 7, Y: "seven"})
	})
}

// TestValuelessMatchersAllocateNothing covers the matchers that carry no expected value. They hold
// no state, so putting one behind the Matcher interface costs nothing.
//
// Matchers that *do* capture an expected value (Equal, NotEqual, Contain) are excluded on purpose:
// each one costs a single allocation for the matcher value itself. That is inherent to the design,
// not a regression, so it is documented as observational rather than pinned here.
func TestValuelessMatchersAllocateNothing(t *testing.T) {
	ctx := NewContext(t)
	var nilErr error

	assertNoAllocations(t, "ExpectT(ctx, true).To(BeTrue())", func() { ExpectT(ctx, true).To(BeTrue()) })
	assertNoAllocations(t, "ExpectT(ctx, false).To(BeFalse())", func() { ExpectT(ctx, false).To(BeFalse()) })
	assertNoAllocations(t, "ctx.Expect(nil error).To(BeNil())", func() { ctx.Expect(nilErr).To(BeNil()) })
}

// TestErrorClassificationAllocatesNothing pins the shape the docs recommend for errors: classify
// with errors.Is/errors.As and feed the boolean into a typed expectation. The recommended idiom
// stays on the fast path; if it ever stops doing so, the recommendation is what needs revisiting.
func TestErrorClassificationAllocatesNothing(t *testing.T) {
	ctx := NewContext(t)
	sentinel := errors.New("boom")
	wrapped := errors.Join(sentinel)

	assertNoAllocations(t, "ExpectT(ctx, errors.Is(err, sentinel)).To(BeTrue())", func() {
		ExpectT(ctx, errors.Is(wrapped, sentinel)).To(BeTrue())
	})
}

// TestUntypedExpectStaysAllocationFreeOnThePassingPath covers ctx.Expect, which takes `any`. It is
// listed separately from the typed path because the guarantee has a different source: the value is
// already an `any` at the call site, and the Expectation holding it does not escape the assertion
// that consumes it, so escape analysis keeps both off the heap for the shapes below.
// Only the passing path is covered -- a *failing* assertion formats a message and is expected to
// allocate, which is fine, because a failing suite is not a hot path.
func TestUntypedExpectStaysAllocationFreeOnThePassingPath(t *testing.T) {
	ctx := NewContext(t)
	text := "syntegrity"

	assertNoAllocations(t, "ctx.Expect(int).ToEqual(int)", func() { ctx.Expect(42).ToEqual(42) })
	assertNoAllocations(t, "ctx.Expect(string).ToEqual(string)", func() { ctx.Expect(text).ToEqual("syntegrity") })
	assertNoAllocations(t, "ctx.Expect(bool).To(BeTrue())", func() { ctx.Expect(true).To(BeTrue()) })
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
func assertRunnerLoopDoesNotAllocatePerSpec(t *testing.T, runner string, build func(n int) func(testing.TB)) {
	t.Helper()

	small := build(allocContractSmallSuite)
	large := build(allocContractLargeSuite)

	smallAllocs := testing.AllocsPerRun(allocContractRuns, func() { small(t) })
	largeAllocs := testing.AllocsPerRun(allocContractRuns, func() { large(t) })

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
	})
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
	})
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
// method not overridden here still behaves. Only Run is overridden, and only to keep the runner off
// the subtest path.
type flatTB struct {
	testing.TB
}

func (flatTB) Helper() {}

// TestProgramRunnerLoopAllocatesNothingPerSpecOnTheFlatPath pins the same contract for the compiled
// execution plan, the runner the public Describe/It DSL builds. See flatTB for why the subtest path
// is excluded.
func TestProgramRunnerLoopAllocatesNothingPerSpecOnTheFlatPath(t *testing.T) {
	flat := flatTB{TB: t}

	build := func(n int) func(testing.TB) {
		b := NewBuilder()
		b.Describe("suite", func() {
			b.BeforeEach(func(*Context) {})
			for i := 0; i < n; i++ {
				b.It("spec", func(ctx *Context) { EqualTo(ctx, 1, 1) })
			}
		})
		return NewRunner(b.Build()).Run
	}

	small := build(allocContractSmallSuite)
	large := build(allocContractLargeSuite)

	smallAllocs := testing.AllocsPerRun(allocContractRuns, func() { small(flat) })
	largeAllocs := testing.AllocsPerRun(allocContractRuns, func() { large(flat) })

	if largeAllocs > smallAllocs {
		perSpec := (largeAllocs - smallAllocs) / float64(allocContractLargeSuite-allocContractSmallSuite)
		t.Errorf("compiled Program runner allocates per spec: %d specs cost %.1f allocations, %d specs cost %.1f (~%.4f per spec, want 0)",
			allocContractSmallSuite, smallAllocs, allocContractLargeSuite, largeAllocs, perSpec)
	}
}
