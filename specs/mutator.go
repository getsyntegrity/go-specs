package specs

import "math/rand"

// Mutator applies fast, deterministic mutations to PathValues for exploration.
type Mutator struct {
	rng *rand.Rand
}

// NewMutator returns a mutator with a deterministic RNG (seed for reproducibility).
func NewMutator(seed int64) *Mutator {
	return &Mutator{rng: rand.New(rand.NewSource(seed))}
}

// clampInt returns v clamped to [min, max].
func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// MutateInt returns a mutation of v: +1, -1, ×2, ÷2, bit flip, or random nearby.
// Result is clamped to [min, max].
func (m *Mutator) MutateInt(v, min, max int) int {
	if m == nil || m.rng == nil {
		return clampInt(v, min, max)
	}
	const numOps = 6
	op := m.rng.Intn(numOps)
	var out int
	switch op {
	case 0:
		out = v + 1
	case 1:
		out = v - 1
	case 2:
		out = v * 2
	case 3:
		if v != 0 {
			out = v / 2
		} else {
			out = 0
		}
	case 4:
		// bit flip: flip one random bit
		bit := uint(m.rng.Intn(32))
		out = v ^ (1 << bit)
	case 5:
		// random nearby: ± up to 5
		delta := m.rng.Intn(11) - 5 // [-5, 5]
		out = v + delta
	default:
		out = v
	}
	return clampInt(out, min, max)
}

// Mutate returns a clone of p with one random dimension mutated.
// For int/int64 dimensions with a range, MutateInt is used and the result is clamped to generator
// bounds. bool dimensions are always negated (guaranteed to change). Discrete dimensions (unranged
// int/int64, or .Values) are re-picked from the dimension's own value set via DimensionValues —
// same as PathGenerator.mutate's discrete case, this may coincidentally land back on the same value.
//
// This is the mutation strategy behind PathSpec.ExploreCoverage/ExploreSmart (via CoverageExplorer/
// SmartExplorer). PathSpec.Explore uses a different, unexported strategy (PathGenerator.mutate in
// path_generator.go) that mutates bool/discrete dimensions the same way, at the cost of not sharing
// this type's int-mutation operators (bit-flip, ×2/÷2, etc. — see MutateInt). The two aren't meant
// to be interchangeable: each backs a distinct public exploration mode, evolved independently rather
// than consolidated, per the discussion on #9.
func (m *Mutator) Mutate(gen *PathGenerator, p PathValues) PathValues {
	if m == nil || m.rng == nil || gen == nil {
		return p.clone()
	}
	nd := gen.NumDims()
	if nd == 0 {
		return p.clone()
	}
	dim := m.rng.Intn(nd)
	idx := gen.ValueIndex(dim)
	if idx < 0 || idx >= len(p.values) {
		return p.clone()
	}
	out := p.clone()
	val := p.values[idx]
	min, max, hasRange := gen.DimensionBounds(dim)

	switch v := val.(type) {
	case bool:
		out.values[idx] = !v
		out.present[idx] = true
	case int:
		if hasRange {
			out.values[idx] = m.MutateInt(v, min, max)
		} else if values := gen.DimensionValues(dim); len(values) > 0 {
			out.values[idx] = values[m.rng.Intn(len(values))]
		}
		out.present[idx] = true
	case int64:
		if hasRange {
			mi, ma := int(min), int(max)
			out.values[idx] = int64(m.MutateInt(int(v), mi, ma))
		} else if values := gen.DimensionValues(dim); len(values) > 0 {
			out.values[idx] = values[m.rng.Intn(len(values))]
		}
		out.present[idx] = true
	default:
		if values := gen.DimensionValues(dim); len(values) > 0 {
			out.values[idx] = values[m.rng.Intn(len(values))]
		}
		out.present[idx] = true
	}
	return out
}
