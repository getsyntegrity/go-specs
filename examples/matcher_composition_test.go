// matcher_composition_test.go shows how to combine matchers: Not, All and Any, and how they nest
// inside the matchers that take a matcher (EveryElement, Project, ...).
//
// Use them instead of writing a new matcher for every combination. Each combinator names the
// sub-matchers that are responsible for a failure, so the message stays useful at any depth.
//
// Semantics:
//   - Not(m) succeeds when m does not. A nil m is a mistake, not a logical value: Not(nil) never matches.
//   - All(ms...) is AND. It reports every entry that failed, with its position (#1, #2, ...).
//     With no matchers it is vacuously true. A nil entry fails the whole All.
//   - Any(ms...) is OR. When nothing matched it lists why each entry failed. With no matchers it
//     never matches. A nil entry never matches.
//   - Each sub-matcher is evaluated once per assertion, however deeply composites nest, so a
//     matcher with a side effect (MatchErrorAs) runs once.
package examples_test

import (
	"fmt"
	"testing"

	"github.com/getsyntegrity/go-specs/assert"
	"github.com/getsyntegrity/go-specs/specs"
)

func TestComposition_inASpec(t *testing.T) {
	specs.Describe(t, "composition", func(s *specs.Spec) {
		s.It("combines matchers on one value", func(ctx *specs.Context) {
			ctx.Expect(42).To(specs.All(specs.BeGreaterThan(0), specs.BeLessThan(100)))
			ctx.Expect("pending").To(specs.Any(specs.Equal("pending"), specs.Equal("queued")))
			ctx.Expect("archived").To(specs.Not(specs.BeOneOf("pending", "queued")))
		})
	})
}

func Example_not() {
	fmt.Println(assert.Not(assert.Equal(1)).Match(2))
	_, failure := assert.Evaluate(assert.Not(assert.Equal(1)), 1)
	fmt.Println(failure)
	// Not(nil) is a mistake in the assertion and never matches.
	_, failure = assert.Evaluate(assert.Not(nil), 1)
	fmt.Println(failure)
	// Output:
	// true
	// expected 1 not to be equal to 1
	// Not: sub-matcher at position 1 is nil
}

func Example_all() {
	inRange := assert.All(assert.BeGreaterThan(0), assert.BeLessThan(100))
	fmt.Println(inRange.Match(42))
	// Every failing entry is reported, numbered from 1.
	_, failure := assert.Evaluate(assert.All(assert.BeGreaterThan(50), assert.BeLessThan(10), assert.BeZero()), 42)
	fmt.Println(failure)
	fmt.Println(assert.All().Match("anything"))
	// Output:
	// true
	// All: #1: "expected 42 to be greater than 50"; #2: "expected 42 to be less than 10"; #3: "expected 42 to be the zero value of int"
	// true
}

func Example_any() {
	status := assert.Any(assert.Equal("pending"), assert.Equal("queued"))
	fmt.Println(status.Match("queued"))
	_, failure := assert.Evaluate(status, "done")
	fmt.Println(failure)
	fmt.Println(assert.Any().Match("anything"))
	// Output:
	// true
	// Any: #1: "expected done to equal pending"; #2: "expected done to equal queued"
	// false
}

// Composites are matchers, so they nest: here Not inside Any inside All.
func Example_nestedComposites() {
	m := assert.All(
		assert.StartWith("user-"),
		assert.Any(assert.EndWith("-admin"), assert.Not(assert.Contain("temp"))),
	)
	fmt.Println(m.Match("user-42"))
	fmt.Println(m.Match("user-temp"))
	fmt.Println(m.Match("user-temp-admin"))
	_, failure := assert.Evaluate(m, "user-temp")
	fmt.Println(failure)
	// Output:
	// true
	// false
	// true
	// All: #2: "Any: #1: \"expected \\\"user-temp\\\" to end with \\\"-admin\\\"\"; #2: \"expected user-temp not to be containing temp\""
}

// Composites go anywhere a matcher is accepted: as the child of EveryElement, or of Project.
func Example_compositesInsideOtherMatchers() {
	fmt.Println(assert.EveryElement(assert.All(assert.BeGreaterThan(0), assert.BeLessThan(10))).Match([]int{1, 5, 9}))
	_, failure := assert.Evaluate(assert.EveryElement(assert.Any(assert.Equal(1), assert.Equal(2))), []int{1, 2, 3})
	fmt.Println(failure)
	// Output:
	// true
	// EveryElement: 1 of 3 elements failed — [2] 3: "Any: #1: \"expected 3 to equal 1\"; #2: \"expected 3 to equal 2\""
}
