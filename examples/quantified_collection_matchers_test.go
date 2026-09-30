// quantified_collection_matchers_test.go shows the matchers that judge the elements of a slice or
// array with a child matcher: EveryElement, AnyElement, NoElement, ExactlyNElements,
// AtLeastNElements, AtMostNElements, HaveElementsInOrder and ContainElementsInOrder.
//
// Use them for questions like "does every order line have a SKU?", "are exactly two lines
// shipped?" or "did the events arrive in this order?". They replace a hand-written loop and a
// boolean, and their failure message names the offending elements by index with the child's own
// explanation.
//
// Semantics:
//   - Only slices and arrays are supported (a nil slice counts as empty). Anything else, including
//     nil, a string or a map, never matches.
//   - Empty input follows logic. EveryElement and NoElement hold vacuously, AnyElement does not,
//     ExactlyNElements(0, m) and AtLeastNElements(0, m) hold, AtMostNElements(n, m) holds for
//     every n >= 0, and ContainElementsInOrder() with no matchers holds for any collection.
//   - A nil child matcher never matches, even on an empty collection, so the mistake cannot ship green.
//   - HaveElementsInOrder matches the whole collection position by position (same length).
//     ContainElementsInOrder allows other elements in between, like a subsequence.
//   - Each element is judged once per evaluation, so a child with side effects (MatchErrorAs) is
//     not driven twice.
package examples_test

import (
	"fmt"

	"github.com/getsyntegrity/go-specs/assert"
)

type quantLine struct {
	SKU     string
	Qty     int
	Shipped bool
}

func quantShipped() assert.Matcher {
	return assert.Satisfy("a shipped line", func(v any) bool {
		l, ok := v.(quantLine)
		return ok && l.Shipped
	})
}

func quantLines() []quantLine {
	return []quantLine{{"pen", 2, true}, {"ink", 1, false}, {"pad", 4, true}}
}

func Example_everyElement() {
	fmt.Println(assert.EveryElement(assert.BeGreaterThan(0)).Match([]int{1, 2, 3}))
	fmt.Println(assert.EveryElement(assert.BeGreaterThan(0)).Match([]int{1, 0, 3}))
	// The failure names each failing element by index.
	_, failure := assert.Evaluate(assert.EveryElement(quantShipped()), quantLines())
	fmt.Println(failure)
	// Output:
	// true
	// false
	// EveryElement: 1 of 3 elements failed — [1] {ink 1 false}: "expected {ink 1 false} to satisfy \"a shipped line\""
}

func Example_anyElement() {
	fmt.Println(assert.AnyElement(assert.Equal("b")).Match([]string{"a", "b"}))
	_, failure := assert.Evaluate(assert.AnyElement(assert.Equal("z")), []string{"a", "b"})
	fmt.Println(failure)
	// Output:
	// true
	// AnyElement: none of 2 elements matched — [0] a: "expected a to equal z"; [1] b: "expected b to equal z"
}

func Example_noElement() {
	fmt.Println(assert.NoElement(assert.BeZero()).Match([]int{1, 2}))
	_, failure := assert.Evaluate(assert.NoElement(quantShipped()), quantLines())
	fmt.Println(failure)
	// Output:
	// true
	// NoElement: 2 of 3 elements matched — [0] {pen 2 true}; [2] {pad 4 true}
}

func Example_countingMatchers() {
	lines := quantLines()
	fmt.Println(assert.ExactlyNElements(2, quantShipped()).Match(lines))
	fmt.Println(assert.AtLeastNElements(2, quantShipped()).Match(lines))
	fmt.Println(assert.AtMostNElements(1, quantShipped()).Match(lines))
	_, failure := assert.Evaluate(assert.ExactlyNElements(1, quantShipped()), lines)
	fmt.Println(failure)
	_, failure = assert.Evaluate(assert.AtLeastNElements(3, quantShipped()), lines)
	fmt.Println(failure)
	_, failure = assert.Evaluate(assert.AtMostNElements(1, quantShipped()), lines)
	fmt.Println(failure)
	// Output:
	// true
	// true
	// false
	// ExactlyNElements: expected exactly 1 of 3 elements to match, 2 did — matching indices [0 2]
	// AtLeastNElements: expected at least 3 of 3 elements to match, 2 did — matching indices [0 2]; [1] {ink 1 false}: "expected {ink 1 false} to satisfy \"a shipped line\""
	// AtMostNElements: expected at most 1 of 3 elements to match, 2 did — matching indices [0 2]
}

// What an empty or nil collection means for each matcher.
func Example_emptyCollections() {
	var nilLines []quantLine
	empty := []quantLine{}
	shipped := quantShipped()
	fmt.Println("EveryElement:", assert.EveryElement(shipped).Match(empty))
	fmt.Println("NoElement:", assert.NoElement(shipped).Match(nilLines))
	fmt.Println("AnyElement:", assert.AnyElement(shipped).Match(empty))
	fmt.Println("ExactlyNElements(0):", assert.ExactlyNElements(0, shipped).Match(empty))
	fmt.Println("AtLeastNElements(1):", assert.AtLeastNElements(1, shipped).Match(empty))
	fmt.Println("AtMostNElements(3):", assert.AtMostNElements(3, shipped).Match(empty))
	fmt.Println("ContainElementsInOrder():", assert.ContainElementsInOrder().Match(empty))
	// Output:
	// EveryElement: true
	// NoElement: true
	// AnyElement: false
	// ExactlyNElements(0): true
	// AtLeastNElements(1): false
	// AtMostNElements(3): true
	// ContainElementsInOrder(): true
}

// Things that are not collections, and mistakes in the assertion itself, never match.
func Example_unsupportedInputs() {
	fmt.Println(assert.EveryElement(assert.Equal(1)).Match(nil))
	fmt.Println(assert.EveryElement(assert.Equal("a")).Match("a"))
	fmt.Println(assert.EveryElement(nil).Match([]int{}))
	fmt.Println(assert.AtLeastNElements(-1, assert.Equal(1)).Match([]int{1}))
	// Not is therefore true on an unsupported value: it only says "the quantifier did not hold".
	fmt.Println(assert.Not(assert.EveryElement(assert.Equal(1))).Match(42))
	// Output:
	// false
	// false
	// false
	// false
	// true
}

func Example_haveElementsInOrder() {
	events := []string{"created", "paid", "shipped"}
	fmt.Println(assert.HaveElementsInOrder(assert.Equal("created"), assert.Equal("paid"), assert.Equal("shipped")).Match(events))
	// The matchers may be any matcher, not only Equal.
	fmt.Println(assert.HaveElementsInOrder(assert.StartWith("c"), assert.Contain("ai"), assert.EndWith("ed")).Match(events))
	// A different length or order fails, and the message names both problems.
	_, failure := assert.Evaluate(assert.HaveElementsInOrder(assert.Equal("created"), assert.Equal("shipped")), events)
	fmt.Println(failure)
	// Output:
	// true
	// true
	// HaveElementsInOrder: expected 2 elements, got 3 — unexpected [2] shipped; 1 of 2 positions failed — [1] paid: "expected paid to equal shipped"
}

func Example_containElementsInOrder() {
	events := []string{"created", "paid", "packed", "shipped"}
	// A subsequence: other events may sit in between.
	fmt.Println(assert.ContainElementsInOrder(assert.Equal("paid"), assert.Equal("shipped")).Match(events))
	// The order is what matters: shipped before paid is not there.
	_, failure := assert.Evaluate(assert.ContainElementsInOrder(assert.Equal("shipped"), assert.Equal("paid")), events)
	fmt.Println(failure)
	// Output:
	// true
	// ContainElementsInOrder: matched 1 of 2 matchers in order; matcher #2 (equal to paid) matched no element after index 3
}

// Each element is consumed at most once, so repeating a matcher needs that many elements.
func Example_containElementsInOrderConsumesElements() {
	fmt.Println(assert.ContainElementsInOrder(assert.Equal("a"), assert.Equal("a")).Match([]string{"a"}))
	fmt.Println(assert.ContainElementsInOrder(assert.Equal("a"), assert.Equal("a")).Match([]string{"a", "b", "a"}))
	// Output:
	// false
	// true
}
