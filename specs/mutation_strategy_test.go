package specs

// mutation_strategy_test.go locks in the documented behavior of the two independently-reachable
// mutation strategies for property-based exploration (see #9, #41, and the doc comments on
// Mutator.Mutate and PathGenerator.mutate): both now mutate every dimension kind (bool, discrete,
// unranged int/int64, ranged int/int64), but via different mechanics — Mutator.Mutate (used by
// PathSpec.ExploreCoverage/ExploreSmart) re-picks discrete values from the dimension's value set and
// uses MutateInt's operators (+1/-1/×2/÷2/bit-flip/nearby) for ranged int/int64, while
// PathGenerator.mutate (used by PathSpec.Explore) has its own simpler int mutation set. Both
// unconditionally negate bool.

import "testing"

func TestMutationStrategies_AgreeOnBoolDimension(t *testing.T) {
	// exploreIterations > 0 puts the generator in ExplorationGuided mode, which is what allocates
	// g.rng — PathGenerator.mutate (unlike Mutator.Mutate, which carries its own rng) uses it.
	gen := newPathGenerator([]PathVar{{Name: "flag", Values: []any{true, false}}}, nil, 0, 1, true, 1, 0, 0)
	input := gen.PathValuesWith(map[string]any{"flag": true})

	out := NewMutator(1).Mutate(gen, input)
	if out.values[0] != false {
		t.Errorf("Mutator.Mutate: expected the bool dimension to be negated to false, got %v", out.values[0])
	}

	genMutated := gen.mutate(input)
	if genMutated.values[0] != false {
		t.Errorf("PathGenerator.mutate: expected the bool dimension to be negated to false, got %v", genMutated.values[0])
	}
}

// TestMutatorMutate_DiscreteDimension_CanChangeValue guards against #41's regression: before the
// fix, Mutator.Mutate left any non-int, non-bool dimension (and any unranged int/int64 dimension)
// completely unchanged. With a single discrete dimension holding several values, the dimension pick
// is deterministic, so across enough seeds the value must diverge from the input at least once.
func TestMutatorMutate_DiscreteDimension_CanChangeValue(t *testing.T) {
	gen := newPathGenerator([]PathVar{{Name: "color", Values: []any{"red", "green", "blue", "yellow"}}}, nil, 0, 1, true, 1, 0, 0)
	input := gen.PathValuesWith(map[string]any{"color": "red"})

	changed := false
	for seed := int64(1); seed <= 50 && !changed; seed++ {
		out := NewMutator(seed).Mutate(gen, input)
		if out.values[0] != "red" {
			changed = true
		}
	}
	if !changed {
		t.Fatal("Mutator.Mutate: expected the discrete dimension to change value across 50 seeds, stayed \"red\" every time")
	}
}

// TestMutatorMutate_UnrangedIntDimension_CanChangeValue mirrors the discrete-dimension case for an
// int dimension registered via .Int (explicit values, no range) instead of .IntRange — #41 also
// covered this case, since MutateInt/DimensionBounds only apply when hasRange is true.
func TestMutatorMutate_UnrangedIntDimension_CanChangeValue(t *testing.T) {
	gen := newPathGenerator([]PathVar{{Name: "n", Values: []any{1, 2, 3, 4}}}, nil, 0, 1, true, 1, 0, 0)
	input := gen.PathValuesWith(map[string]any{"n": 1})

	changed := false
	for seed := int64(1); seed <= 50 && !changed; seed++ {
		out := NewMutator(seed).Mutate(gen, input)
		if out.values[0] != 1 {
			changed = true
		}
	}
	if !changed {
		t.Fatal("Mutator.Mutate: expected the unranged int dimension to change value across 50 seeds, stayed 1 every time")
	}
}

func TestPathGeneratorDimensionValues(t *testing.T) {
	gen := newPathGenerator([]PathVar{
		{Name: "color", Values: []any{"red", "green", "blue"}},
		{Name: "n", rangeSpec: &intRange{min: 0, max: 9}},
	}, nil, 0, 0, false, 0, 0, 0)

	discrete := gen.DimensionValues(0)
	if len(discrete) != 3 || discrete[0] != "red" || discrete[1] != "green" || discrete[2] != "blue" {
		t.Fatalf("DimensionValues(0) = %v, want [red green blue]", discrete)
	}

	if ranged := gen.DimensionValues(1); ranged != nil {
		t.Fatalf("DimensionValues(1) = %v, want nil for a ranged dimension", ranged)
	}

	if out := gen.DimensionValues(-1); out != nil {
		t.Fatalf("DimensionValues(-1) = %v, want nil for an out-of-range index", out)
	}
	if out := gen.DimensionValues(2); out != nil {
		t.Fatalf("DimensionValues(2) = %v, want nil for an out-of-range index", out)
	}

	discrete[0] = "mutated"
	if gen.DimensionValues(0)[0] != "red" {
		t.Fatal("DimensionValues: returned slice must be a copy, mutating it corrupted generator state")
	}
}
