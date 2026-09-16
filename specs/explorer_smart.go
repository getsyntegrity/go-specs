package specs

import "math/rand"

// SmartExplorer combines boundary exploration, random sampling, coverage-guided mutation,
// and corpus replay to maximize the chance of finding bugs in a limited number of iterations.
type SmartExplorer struct {
	corpus  *Corpus
	mutator *Mutator
	rng     *rand.Rand
	seen    Coverage
}

// NewSmartExplorer returns an explorer with deterministic RNG (seed for reproducibility).
func NewSmartExplorer(seed int64) *SmartExplorer {
	return &SmartExplorer{
		corpus:  NewCorpus(seed),
		mutator: NewMutator(seed),
		rng:     rand.New(rand.NewSource(seed)),
	}
}

// NextInput returns the next input using a weighted strategy: 30% mutation from corpus,
// 30% random, 20% boundary values, 20% corpus replay. Falls back to random when corpus
// is empty for mutation/replay. Deterministic for a fixed seed.
func (e *SmartExplorer) NextInput(gen *PathGenerator) PathValues {
	if e == nil || e.rng == nil || gen == nil {
		return PathValues{}
	}
	// 30% mutation, 30% random, 20% boundary, 20% replay
	roll := e.rng.Float64()
	switch {
	case roll < 0.30:
		if e.corpus.Len() > 0 {
			return e.mutator.Mutate(gen, e.corpus.Random())
		}
		return gen.RandomInput(e.rng)
	case roll < 0.60:
		return gen.RandomInput(e.rng)
	case roll < 0.80:
		return e.boundaryInput(gen)
	default:
		if e.corpus.Len() > 0 {
			return e.corpus.Random()
		}
		return gen.RandomInput(e.rng)
	}
}

// boundaryInput returns a PathValues with boundary values per dimension: 0, 1, -1, min, max, half (clamped to dimension bounds).
func (e *SmartExplorer) boundaryInput(gen *PathGenerator) PathValues {
	pv := gen.RandomInput(e.rng)
	for d := 0; d < gen.NumDims(); d++ {
		min, max, hasRange := gen.DimensionBounds(d)
		if !hasRange {
			continue
		}
		idx := gen.ValueIndex(d)
		if idx < 0 || idx >= len(pv.values) {
			continue
		}
		half := (min + max) / 2
		choices := []int{0, 1, -1, min, max, half}
		v := choices[e.rng.Intn(len(choices))]
		if v < min {
			v = min
		}
		if v > max {
			v = max
		}
		pv.values[idx] = v
		pv.present[idx] = true
	}
	return pv
}

// Feedback records coverage for the run. If cov has unseen edges, the input is added to the corpus.
func (e *SmartExplorer) Feedback(p PathValues, cov *Coverage) {
	if e == nil {
		return
	}
	if cov == nil {
		return
	}
	if cov.HasNewCoverage(&e.seen) {
		e.corpus.Add(p)
		e.seen.MergeFrom(cov)
	}
}

// CorpusLen returns the number of inputs in the corpus.
func (e *SmartExplorer) CorpusLen() int {
	if e == nil {
		return 0
	}
	return e.corpus.Len()
}
