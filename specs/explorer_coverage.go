package specs

// CoverageExplorer learns from execution-path coverage (branch sampling via assertions).
// seenCoverageHashes is maintained in the Coverage bitmap; when Feedback reports new edges,
// the input is stored in the corpus. NextInput returns random input when corpus is empty,
// otherwise a mutation of a random corpus entry via Mutator.Mutate: +1/-1/×2/÷2/bit-flip/nearby
// for a ranged int or int64 dimension (see Mutator.MutateInt); bool is negated; discrete dimensions
// (unranged int/int64, .Values) are re-picked from their own value set — see Mutator.Mutate's doc
// comment.
//
// This is a different mutation strategy from the one PathSpec.Explore(n) uses (PathGenerator's own
// unexported mutate, which mutates bool/discrete dimensions the same way but doesn't share this
// type's int operators) — see Mutator.Mutate's doc comment for why the two exist and aren't meant
// to be interchangeable.
type CoverageExplorer struct {
	corpus  *Corpus
	mutator *Mutator
	seen    Coverage
}

// NewCoverageExplorer returns an explorer with deterministic corpus and mutator (same seed).
func NewCoverageExplorer(seed int64) *CoverageExplorer {
	return &CoverageExplorer{
		corpus:  NewCorpus(seed),
		mutator: NewMutator(seed),
	}
}

// NextInput returns the next input to try: random if corpus is empty, else a mutation of a random corpus entry.
func (e *CoverageExplorer) NextInput(gen *PathGenerator) PathValues {
	if e == nil {
		return PathValues{}
	}
	if gen == nil {
		return PathValues{}
	}
	if e.corpus.Len() == 0 {
		return gen.RandomInput(e.mutator.rng)
	}
	return e.mutator.Mutate(gen, e.corpus.Random())
}

// Feedback records coverage for the run with input p. If cov has unseen edges, p is added to the corpus
// and cov is merged into seen. Returns true if the input was added to the corpus (new coverage).
func (e *CoverageExplorer) Feedback(p PathValues, cov *Coverage) bool {
	if e == nil {
		return false
	}
	if cov == nil {
		return false
	}
	if cov.HasNewCoverage(&e.seen) {
		e.corpus.Add(p)
		e.seen.MergeFrom(cov)
		return true
	}
	return false
}

// Seen returns a copy of the accumulated coverage (for external inspection).
func (e *CoverageExplorer) Seen() Coverage {
	if e == nil {
		return Coverage{}
	}
	return e.seen
}

// CorpusLen returns the number of inputs in the corpus.
func (e *CoverageExplorer) CorpusLen() int {
	if e == nil {
		return 0
	}
	return e.corpus.Len()
}
