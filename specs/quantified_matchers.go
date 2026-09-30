package specs

import "github.com/getsyntegrity/go-specs/assert"

// Quantified and ordered collection matchers, re-exported from assert. They judge the elements of a
// slice or array with a child matcher; see assert/quantified_matchers.go for the exact semantics.

// EveryElement expects every element of actual (a slice or array) to match m; an empty collection
// matches vacuously.
func EveryElement(m Matcher) Matcher { return assert.EveryElement(m) }

// AnyElement expects at least one element of actual to match m; an empty collection never matches.
func AnyElement(m Matcher) Matcher { return assert.AnyElement(m) }

// NoElement expects no element of actual to match m; an empty collection matches vacuously.
func NoElement(m Matcher) Matcher { return assert.NoElement(m) }

// ExactlyNElements expects exactly n elements of actual to match m.
func ExactlyNElements(n int, m Matcher) Matcher { return assert.ExactlyNElements(n, m) }

// AtLeastNElements expects at least n elements of actual to match m.
func AtLeastNElements(n int, m Matcher) Matcher { return assert.AtLeastNElements(n, m) }

// AtMostNElements expects at most n elements of actual to match m.
func AtMostNElements(n int, m Matcher) Matcher { return assert.AtMostNElements(n, m) }

// HaveElementsInOrder expects actual to be exactly the sequence ms: same length, element i matching
// ms[i].
func HaveElementsInOrder(ms ...Matcher) Matcher { return assert.HaveElementsInOrder(ms...) }

// ContainElementsInOrder expects ms to match a subsequence of actual's elements, in order, each
// element used at most once.
func ContainElementsInOrder(ms ...Matcher) Matcher { return assert.ContainElementsInOrder(ms...) }
