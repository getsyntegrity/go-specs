package specs

import (
	"strings"
	"testing"
)

// Issue #177: the allocation guarantee has to be stated over values a real spec asserts on.
//
// TestPassingAssertionsAllocateNothingOnTheFastPath asserts 42 against 42. That case cannot
// allocate whatever the assertion path does, because the Go runtime serves interface conversions of
// integers below 256 out of runtime.staticuint64s and both operands are untyped constants. It
// therefore proves only that the handle itself is stack-allocated — not that the value reaches the
// comparison without being boxed, which is the part the docs were read as promising and did not
// deliver for strings, large integers or structs.
//
// The table below is the whole allocation contract, per assertion form and per value shape, and it
// is the same table the README publishes. Pinning it here rather than in a benchmark is deliberate:
// `go test ./...` runs this, so a regression fails the build instead of quietly making a published
// number wrong.

type allocProbe struct {
	X, Y int64
	Name string
}

// Built in init rather than written as literals: as constants the compiler could fold them, hoist
// their interface conversions out of the measured function, or serve them from the static table —
// each of which would let a boxing regression pass this test.
var (
	probeSmallInt int
	probeLargeInt int
	probeString   string
	probeStruct   allocProbe
)

func init() {
	probeSmallInt = len("42") + 40
	probeLargeInt = 1<<40 + len("x")
	probeString = strings.Repeat("assertion", 4)
	probeStruct = allocProbe{X: int64(probeLargeInt), Y: 7, Name: probeString}
}

// TestAssertionAllocationsByValueShape pins the published allocation table.
//
// The zeros on the two typed rows are the point of #177: those paths hold the value at its own type
// and never convert it to an interface, so they cost nothing for a T of any size.
//
// The non-zero cells are pinned just as tightly, and for the same reason. ExpectT(...).To(m) must
// convert the value to an any because Matcher is Match(any), and ctx.Expect takes an any and boxes
// both operands. Neither cost can be removed by anything ExpectT does — only a generic Matcher[T]
// would remove the first — so they are measured and published rather than glossed over. Asserting
// the exact count, not an upper bound, is what makes this test fail if a cost silently grows *or*
// disappears, which is what keeps the README honest in both directions.
func TestAssertionAllocationsByValueShape(t *testing.T) {
	ctx, _ := newCapturedContext()
	// Matchers are built once, outside the measured function: constructing a matcher boxes it into
	// the Matcher interface, and that allocation belongs to the matcher, not to the assertion path.
	mSmall, mLarge := Equal(probeSmallInt), Equal(probeLargeInt)
	mString, mStruct := Equal(probeString), Equal(probeStruct)

	cases := []struct {
		name string
		want float64
		fn   func()
	}{
		// EqualTo — fully generic, no interface anywhere.
		{"EqualTo/small int", 0, func() { EqualTo(ctx, probeSmallInt, probeSmallInt) }},
		{"EqualTo/large int", 0, func() { EqualTo(ctx, probeLargeInt, probeLargeInt) }},
		{"EqualTo/string", 0, func() { EqualTo(ctx, probeString, probeString) }},
		{"EqualTo/struct", 0, func() { EqualTo(ctx, probeStruct, probeStruct) }},

		// ExpectT(...).ToEqual — the path #177 unboxed.
		{"ExpectT.ToEqual/small int", 0, func() { ExpectT(ctx, probeSmallInt).ToEqual(probeSmallInt) }},
		{"ExpectT.ToEqual/large int", 0, func() { ExpectT(ctx, probeLargeInt).ToEqual(probeLargeInt) }},
		{"ExpectT.ToEqual/string", 0, func() { ExpectT(ctx, probeString).ToEqual(probeString) }},
		{"ExpectT.ToEqual/struct", 0, func() { ExpectT(ctx, probeStruct).ToEqual(probeStruct) }},

		// ExpectT(...).To(matcher) — one conversion, forced by Matcher.Match(any). A small integer
		// needs none because the runtime serves it from its static table, which is exactly why the
		// matcher path must not be described as "one allocation" unconditionally either.
		{"ExpectT.To/small int", 0, func() { ExpectT(ctx, probeSmallInt).To(mSmall) }},
		{"ExpectT.To/large int", 1, func() { ExpectT(ctx, probeLargeInt).To(mLarge) }},
		{"ExpectT.To/string", 1, func() { ExpectT(ctx, probeString).To(mString) }},
		{"ExpectT.To/struct", 1, func() { ExpectT(ctx, probeStruct).To(mStruct) }},

		// ctx.Expect(...).ToEqual — two conversions, one per operand, because the API is untyped.
		{"Expect.ToEqual/small int", 0, func() { ctx.Expect(probeSmallInt).ToEqual(probeSmallInt) }},
		{"Expect.ToEqual/large int", 2, func() { ctx.Expect(probeLargeInt).ToEqual(probeLargeInt) }},
		{"Expect.ToEqual/string", 2, func() { ctx.Expect(probeString).ToEqual(probeString) }},
		{"Expect.ToEqual/struct", 2, func() { ctx.Expect(probeStruct).ToEqual(probeStruct) }},
	}
	for _, c := range cases {
		if got := testing.AllocsPerRun(1000, c.fn); got != c.want {
			t.Errorf("%s allocated %v times per run on the passing path, want exactly %v", c.name, got, c.want)
		}
	}
}
