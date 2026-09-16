package specs

import "math"

// Shrinker finds a minimal failing PathValues by deterministically shrinking each dimension.
type Shrinker struct {
	generator *PathGenerator
}

// NewShrinker returns a Shrinker that uses the given PathGenerator for dimensions and value shrinkers.
func NewShrinker(generator *PathGenerator) *Shrinker {
	if generator == nil {
		return nil
	}
	return &Shrinker{generator: generator}
}

// Shrink returns a minimal failing PathValues. test must return true when the property holds
// and false when it fails. The original failing is not modified. The result is deterministic
// and always a failing case.
//
// Algorithm: coordinate descent over dimensions. Each round processes dimensions in index
// order (0, 1, 2, ...). For each dimension, shrink that dimension only; if the result still
// fails and the value changed, accept it and restart from dimension 0 so we can minimize
// all dimensions toward a global minimum. No randomness is used.
func (s *Shrinker) Shrink(failing PathValues, test func(PathValues) bool) PathValues {
	if s == nil || s.generator == nil {
		return failing.clone()
	}
	pv := failing.clone()
	improved := true
	for improved {
		improved = false
		// Process dimensions in deterministic index order.
		for d := 0; d < s.generator.NumDims(); d++ {
			candidate := s.shrinkDimension(pv, d, test)
			if !test(candidate) {
				idx := s.generator.ValueIndex(d)
				if idx >= 0 && idx < len(candidate.values) && candidate.values[idx] != pv.values[idx] {
					pv = candidate
					improved = true
					// Restart from dimension 0 so we keep minimizing all dimensions.
					break
				}
			}
		}
	}
	return pv
}

// shrinkDimension returns a PathValues with dimension dim shrunk. For numeric dimensions
// it uses binary search toward zero (O(log n)); otherwise it uses the dimension's value shrinker.
// The original pv is not modified.
func (s *Shrinker) shrinkDimension(pv PathValues, dim int, test func(PathValues) bool) PathValues {
	if s == nil || s.generator == nil {
		return pv.clone()
	}
	idx := s.generator.ValueIndex(dim)
	if idx < 0 || idx >= len(pv.values) {
		return pv.clone()
	}
	val := pv.values[idx]
	var current int
	switch v := val.(type) {
	case int:
		current = v
	case int64:
		current = int(v)
	default:
		return s.shrinkDimensionValueShrinker(pv, dim, test)
	}
	if current <= 0 {
		return pv.clone()
	}
	// Binary search: find smallest value in [0, current] that still fails.
	low, high := 0, current
	lastFailing := current
	candidate := pv.clone()
	for high > low {
		mid := (high + low) / 2
		candidate.values[idx] = mid
		candidate.present[idx] = true
		if !s.generator.allow(candidate) {
			low = mid + 1
			continue
		}
		if !test(candidate) {
			lastFailing = mid
			high = mid
		} else {
			low = mid + 1
		}
	}
	candidate.values[idx] = lastFailing
	candidate.present[idx] = true
	return candidate
}

// shrinkDimensionValueShrinker uses ForEachShrinkCandidate for non-numeric or fallback shrinking.
func (s *Shrinker) shrinkDimensionValueShrinker(pv PathValues, dim int, test func(PathValues) bool) PathValues {
	var found PathValues
	var foundOk bool
	s.generator.ForEachShrinkCandidate(pv, dim, func(candidate PathValues) bool {
		if !test(candidate) {
			found = candidate
			foundOk = true
			return false
		}
		return true
	})
	if foundOk {
		return found
	}
	return pv.clone()
}

type intShrinker struct{}

func (intShrinker) Shrink(value any) []any {
	v, ok := value.(int)
	if !ok {
		if v64, ok := value.(int64); ok {
			v = int(v64)
		} else {
			return nil
		}
	}
	if v == 0 {
		return nil
	}
	candidates := []any{}
	halves := []int{}
	if v > 0 {
		halves = append(halves, v/2)
		if v > 1 {
			halves = append(halves, v-1)
		}
		if v > 10 {
			halves = append(halves, v-10)
		}
		if v > 100 {
			halves = append(halves, v-100)
		}
	}
	for _, h := range halves {
		if h >= 0 {
			candidates = append(candidates, h)
		}
	}
	if v > 0 {
		candidates = append(candidates, 0)
	}
	return candidates
}

type boolShrinker struct{}

func (boolShrinker) Shrink(value any) []any {
	_, ok := value.(bool)
	if !ok {
		return nil
	}
	return []any{false}
}

type floatShrinker struct{}

func (floatShrinker) Shrink(value any) []any {
	v, ok := value.(float64)
	if !ok {
		return nil
	}
	if v == 0 {
		return nil
	}
	candidates := []any{}
	if v > 0 {
		candidates = append(candidates, v/2)
		candidates = append(candidates, math.Trunc(v))
		candidates = append(candidates, 0.0)
	}
	return candidates
}

var (
	DefaultIntShrinker   ValueShrinker = intShrinker{}
	DefaultBoolShrinker  ValueShrinker = boolShrinker{}
	DefaultFloatShrinker ValueShrinker = floatShrinker{}
)
