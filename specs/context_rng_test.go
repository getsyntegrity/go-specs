package specs

import (
	"testing"
)

func TestContextRNGDeterminism(t *testing.T) {
	s := newSpec(t, false, nil)
	seed := int64(12345)
	first := generateRNGSequence(t, s, seed)
	second := generateRNGSequence(t, s, seed)
	if len(first) != len(second) {
		t.Fatalf("sequence length mismatch: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("rng sequence mismatch at %d: %d vs %d", i, first[i], second[i])
		}
	}
}

func generateRNGSequence(t *testing.T, s *Spec, seed int64) []int64 {
	s.RandomSeed(seed)
	values := make([]int64, 10)
	s.Describe("seq", func(spec *Spec) {
		spec.It("reads", func(ctx *Context) {
			for i := range values {
				values[i] = ctx.randomInt64()
			}
		})
	})
	return append([]int64(nil), values...)
}
