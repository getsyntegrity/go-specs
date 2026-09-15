package specs

import (
	"strings"
	"testing"
)

// TestPathValuesHashDeterministicAcrossClones proves Hash depends only on content and position, not
// on identity: a clone must hash identically to its source, which is what lets candidateSubtestName
// (#103) use the hash as a stable fingerprint across separate PathValues copies of the same candidate.
func TestPathValuesHashDeterministicAcrossClones(t *testing.T) {
	pv := PathValues{
		index:   map[string]int{"a": 0, "b": 1},
		values:  []any{"first", 5},
		present: []bool{true, true},
	}
	clone := pv.clone()
	if pv.Hash() != clone.Hash() {
		t.Fatalf("expected a clone to hash identically to its source, got %d and %d", pv.Hash(), clone.Hash())
	}
}

// TestPathValuesHashDistinguishesStringContent pins the fix for the bug this method carried
// unnoticed before #103 gave it a caller: every non-numeric, non-bool type — including string, the
// single most common PathVar kind — fell into the position-only fallback, so two candidates that
// differed only in a string value hashed identically. Now a string is hashed by its own bytes.
func TestPathValuesHashDistinguishesStringContent(t *testing.T) {
	index := map[string]int{"name": 0}
	a := PathValues{index: index, values: []any{"alpha"}, present: []bool{true}}
	b := PathValues{index: index, values: []any{"bravo"}, present: []bool{true}}
	if a.Hash() == b.Hash() {
		t.Fatalf("expected distinct string values to hash differently, both hashed to %d", a.Hash())
	}
}

// TestPathValuesHashDistinguishesLongStringsSharingAPrefix proves the byte-by-byte string hashing
// added for #103 does not truncate: two long strings that agree on a long shared prefix and differ
// only in their last byte must still hash differently. candidateSubtestName's actual identity
// guarantee (collision-free names) depends solely on AttemptIndex, never on this hash — but a hash
// that quietly truncated would make the Fingerprint segment a poor diagnostic aid, silently equating
// two candidates whose values genuinely differ.
func TestPathValuesHashDistinguishesLongStringsSharingAPrefix(t *testing.T) {
	prefix := strings.Repeat("the quick brown fox jumps over the lazy dog. ", 20)
	index := map[string]int{"name": 0}
	a := PathValues{index: index, values: []any{prefix + "A"}, present: []bool{true}}
	b := PathValues{index: index, values: []any{prefix + "B"}, present: []bool{true}}
	if a.Hash() == b.Hash() {
		t.Fatalf("expected two long strings sharing a prefix to hash differently")
	}
}

// TestPathValuesHashDistinguishesFloatContent extends the same fix to float32/float64, the other
// common PathVar kind that used to fall into the position-only fallback.
func TestPathValuesHashDistinguishesFloatContent(t *testing.T) {
	index := map[string]int{"x": 0}
	a := PathValues{index: index, values: []any{1.5}, present: []bool{true}}
	b := PathValues{index: index, values: []any{2.5}, present: []bool{true}}
	if a.Hash() == b.Hash() {
		t.Fatalf("expected distinct float64 values to hash differently")
	}
	af := PathValues{index: index, values: []any{float32(1.5)}, present: []bool{true}}
	bf := PathValues{index: index, values: []any{float32(2.5)}, present: []bool{true}}
	if af.Hash() == bf.Hash() {
		t.Fatalf("expected distinct float32 values to hash differently")
	}
}

// TestPathValuesHashPointerFallsBackToPositionOnly pins the accepted limitation Hash's doc comment
// documents: a pointer's target is never read, because its formatted form (an address) is unstable
// across processes and runs, exactly what #103 requires a candidate-identity hash to avoid. Two
// distinct pointers at the same position hash identically — this is deliberate, not a gap the fix
// left open.
func TestPathValuesHashPointerFallsBackToPositionOnly(t *testing.T) {
	index := map[string]int{"p": 0}
	x, y := 1, 2
	a := PathValues{index: index, values: []any{&x}, present: []bool{true}}
	b := PathValues{index: index, values: []any{&y}, present: []bool{true}}
	if a.Hash() != b.Hash() {
		t.Fatalf("expected two distinct pointers at the same position to hash identically, got %d and %d", a.Hash(), b.Hash())
	}
}

// BenchmarkPathValuesHash measures the per-candidate cost #103 adds to every generated case: Hash is
// now called once per executed candidate, from candidateSubtestName, to build its name. A five-var
// mix (two strings, two ints, one bool) stands in for a realistic PathVar set.
func BenchmarkPathValuesHash(b *testing.B) {
	pv := PathValues{
		index: map[string]int{"sku": 0, "region": 1, "qty": 2, "price": 3, "vip": 4},
		values: []any{
			"SKU-0042-DELUXE", "us-east", 7, 199, true,
		},
		present: []bool{true, true, true, true, true},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = pv.Hash()
	}
}
