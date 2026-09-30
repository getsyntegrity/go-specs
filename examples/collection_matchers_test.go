// collection_matchers_test.go shows the matchers that look at what a slice, array, string or map
// holds: Contain, ContainAllOf, ContainAnyOf, ContainTheSameElementsAs, BeOneOf, HaveLen, BeEmpty,
// HaveKey, HaveValue and HavePair.
//
// Use them when the test cares about membership, size or map contents but not about one exact,
// fully spelled-out value. To judge each element with another matcher ("every order has an item"),
// see quantified_collection_matchers_test.go.
//
// Semantics that surprise people:
//   - Elements compare with ValuesEqual, like Equal: an int 1 never matches an int64 1.
//   - Contain, ContainAllOf and ContainAnyOf accept a string too, where each expected value is a
//     substring. Contain does NOT look at map keys: use HaveKey, HaveValue or HavePair.
//   - ContainAllOf() with no elements matches any collection; ContainAnyOf() and BeOneOf() with no
//     values never match. Nothing is required of the first, nothing could satisfy the others.
//   - ContainTheSameElementsAs is multiset equality: order is ignored, duplicates count.
//   - A nil slice or map has length zero, so HaveLen(0) and BeEmpty accept it.
//   - An actual of the wrong kind (an int, nil, ...) never matches, and the failure message says
//     the kind is unsupported instead of reporting a missing element.
package examples_test

import (
	"fmt"

	"github.com/getsyntegrity/go-specs/assert"
)

func Example_contain() {
	fmt.Println(assert.Contain(3).Match([]int{1, 2, 3}))
	fmt.Println(assert.Contain("wor").Match("hello world"))
	fmt.Println(assert.Contain("b").Match([3]string{"a", "b", "c"}))
	fmt.Println(assert.Contain(4).Match([]int{1, 2, 3}))
	// Output:
	// true
	// true
	// true
	// false
}

// Contain says why it failed: the element is missing, the actual cannot be searched, or the
// expected value has the wrong type for the elements.
func Example_containFailureMessages() {
	fmt.Println(assert.Contain(4).FailureMessage([]int{1, 2, 3}))
	fmt.Println(assert.Contain("x").FailureMessage([]int{1, 2, 3}))
	fmt.Println(assert.Contain("a").FailureMessage(map[string]int{"a": 1}))
	fmt.Println(assert.Contain(1).FailureMessage(nil))
	// Output:
	// expected [1 2 3] to contain 4
	// expected [1 2 3] to contain x — []int actual needs an int expected value, got string
	// expected map[a:1] to contain a — map[string]int is not a supported Contain actual (want string, slice, or array)
	// expected <nil> to contain 1 — <nil> is not a supported Contain actual (want string, slice, or array)
}

func Example_containAllOf() {
	m := assert.ContainAllOf("red", "blue")
	fmt.Println(m.Match([]string{"green", "blue", "red"}))
	fmt.Println(m.Match("red and blue paint"))
	fmt.Println(m.Match([]string{"red"}))
	fmt.Println(assert.ContainAllOf().Match([]string{}))
	_, failure := assert.Evaluate(m, []string{"red"})
	fmt.Println(failure)
	// Output:
	// true
	// true
	// false
	// true
	// expected [red] to contain all of [red blue], missing [blue]
}

func Example_containAnyOf() {
	m := assert.ContainAnyOf(404, 500)
	fmt.Println(m.Match([]int{200, 500}))
	fmt.Println(m.Match([]int{200, 201}))
	fmt.Println(assert.ContainAnyOf().Match([]int{200}))
	_, failure := assert.Evaluate(m, []int{200, 201})
	fmt.Println(failure)
	// Output:
	// true
	// false
	// false
	// expected [200 201] to contain any of [404 500]
}

// Same elements, any order, duplicates count.
func Example_containTheSameElementsAs() {
	m := assert.ContainTheSameElementsAs([]int{3, 1, 2})
	fmt.Println(m.Match([]int{1, 2, 3}))
	fmt.Println(assert.ContainTheSameElementsAs([]int{1, 1, 2}).Match([]int{1, 2, 2}))
	// The two sides may have different slice types.
	fmt.Println(assert.ContainTheSameElementsAs([]any{1, "a"}).Match([]any{"a", 1}))
	_, failure := assert.Evaluate(m, []int{1, 2, 4})
	fmt.Println(failure)
	// Output:
	// true
	// false
	// true
	// expected [1 2 4] to contain the same elements as [3 1 2] — missing [3], unexpected [4]
}

func Example_beOneOf() {
	m := assert.BeOneOf("draft", "open", "closed")
	fmt.Println(m.Match("open"))
	fmt.Println(m.Match("deleted"))
	fmt.Println(assert.BeOneOf().Match("anything"))
	_, failure := assert.Evaluate(m, "deleted")
	fmt.Println(failure)
	// Output:
	// true
	// false
	// false
	// expected deleted to be one of [draft open closed]
}

func Example_haveLenAndBeEmpty() {
	var nilMap map[string]int
	fmt.Println(assert.HaveLen(3).Match([]int{1, 2, 3}))
	fmt.Println(assert.HaveLen(5).Match("hello"))
	fmt.Println(assert.HaveLen(1).Match(map[string]int{"a": 1}))
	fmt.Println(assert.HaveLen(0).Match(nilMap))
	fmt.Println(assert.BeEmpty().Match([]int(nil)))
	fmt.Println(assert.BeEmpty().Match([]int{}))
	fmt.Println(assert.BeEmpty().Match([]int{1}))
	// Output:
	// true
	// true
	// true
	// true
	// true
	// true
	// false
}

func Example_haveLenFailureMessages() {
	fmt.Println(assert.HaveLen(2).FailureMessage([]int{1, 2, 3}))
	fmt.Println(assert.BeEmpty().FailureMessage([]int{1}))
	fmt.Println(assert.HaveLen(1).FailureMessage(42))
	// Output:
	// expected [1 2 3] to have length 2, got length 3
	// expected [1] to be empty, got length 1
	// HaveLen: int has no length
}

func Example_mapMatchers() {
	prices := map[string]int{"tea": 3, "coffee": 4}
	fmt.Println(assert.HaveKey("tea").Match(prices))
	fmt.Println(assert.HaveKey("water").Match(prices))
	fmt.Println(assert.HaveValue(4).Match(prices))
	fmt.Println(assert.HavePair("tea", 3).Match(prices))
	// A value stored under a different key does not count for HavePair.
	fmt.Println(assert.HavePair("tea", 4).Match(prices))
	// Output:
	// true
	// false
	// true
	// true
	// false
}

func Example_mapMatchersFailureMessages() {
	prices := map[string]int{"tea": 3}
	fmt.Println(assert.HaveKey("water").FailureMessage(prices))
	fmt.Println(assert.HaveValue(9).FailureMessage(prices))
	fmt.Println(assert.HavePair("tea", 4).FailureMessage(prices))
	// A key of the wrong type is a plain non-match with its own message, never a panic.
	fmt.Println(assert.HaveKey(1).FailureMessage(prices))
	fmt.Println(assert.HaveKey("tea").FailureMessage([]string{"tea"}))
	// Output:
	// expected map[tea:3] to have key water
	// expected map[tea:3] to have value 9
	// expected map[tea:3] to have key tea with value 4 — key has value 3
	// expected map[tea:3] to have key 1 — map[string]int keys are string, got int
	// HaveKey: []string is not a map
}
