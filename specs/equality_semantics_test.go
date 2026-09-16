package specs

// equality_semantics_test.go locks in the documented equality-semantics divergence between
// EqualTo/ExpectT.ToEqual (always ==) and Expectation.ToEqual (== fast path, reflect.DeepEqual
// fallback for everything else). See "Equality semantics" in docs/DSL.md and issue #8. This is a
// deliberate design tradeoff (speed vs. value-based equality for non-primitive types), not a bug —
// these tests exist so a future change to either comparison can't silently make them agree (or
// disagree differently) without a test failing to call it out.

import "testing"

type equalityWithPtr struct{ N *int }

// TestEqualitySemantics_PointerStructDivergesByDesign verifies that EqualTo (==, pointer identity)
// and ctx.Expect(...).ToEqual (reflect.DeepEqual fallback, value equality) disagree for the exact
// same logical values when a struct holds a pointer field — by design, not a bug.
func TestEqualitySemantics_PointerStructDivergesByDesign(t *testing.T) {
	a, b := 5, 5
	x, y := equalityWithPtr{N: &a}, equalityWithPtr{N: &b}

	eqToBackend := &capturingBackend{}
	EqualTo(&Context{backend: eqToBackend}, x, y)
	if !eqToBackend.failed {
		t.Error("expected EqualTo to fail: == compares pointer identity, and x.N != y.N")
	}

	expectTBackend := &capturingBackend{}
	ExpectT(&Context{backend: expectTBackend}, x).ToEqual(y)
	if !expectTBackend.failed {
		t.Error("expected ExpectT(...).ToEqual to fail the same way as EqualTo — same == semantics")
	}

	expectBackend := &capturingBackend{}
	(&Context{backend: expectBackend}).Expect(x).ToEqual(y)
	if expectBackend.failed {
		t.Errorf("expected ctx.Expect(...).ToEqual to pass via reflect.DeepEqual, got failure: %q", expectBackend.message)
	}
}
