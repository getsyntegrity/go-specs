package specs

import "math/rand"

const maxCorpusSize = 1024

// Corpus stores inputs that produced new coverage during exploration.
// Evicts oldest entries when full. Random selection is O(1).
type Corpus struct {
	inputs []PathValues
	rng    *rand.Rand
}

// NewCorpus returns a corpus with a deterministic RNG (seed for reproducibility).
func NewCorpus(seed int64) *Corpus {
	return &Corpus{
		inputs: make([]PathValues, 0, maxCorpusSize),
		rng:    rand.New(rand.NewSource(seed)),
	}
}

// Add appends a clone of p. When at maxCorpusSize, the oldest entry is evicted.
func (c *Corpus) Add(p PathValues) {
	if c == nil {
		return
	}
	clone := p.clone()
	if len(c.inputs) >= maxCorpusSize {
		c.inputs = append(c.inputs[1:], clone)
		return
	}
	c.inputs = append(c.inputs, clone)
}

// Random returns a clone of a random corpus entry, or zero PathValues if empty.
func (c *Corpus) Random() PathValues {
	if c == nil || len(c.inputs) == 0 {
		return PathValues{}
	}
	return c.inputs[c.rng.Intn(len(c.inputs))].clone()
}

// Len returns the number of inputs in the corpus.
func (c *Corpus) Len() int {
	if c == nil {
		return 0
	}
	return len(c.inputs)
}
