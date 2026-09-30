// custom_matchers_test.go shows the two ways to check something no built-in matcher states:
// Satisfy for a one-off predicate, and your own type that implements assert.Matcher.
//
// Use Satisfy when the check is a short function used once or twice. Write a matcher type when the
// check is reused, takes parameters, or deserves a failure message that explains itself. For a
// domain object where you want the failure to name the field, prefer Project (projection_test.go).
//
// Satisfy semantics: the description appears in the failure message and in Not/All/Any messages
// ("expected 3 to satisfy \"is even\""); the predicate receives the actual as is, including nil; a
// panic inside it propagates; a nil predicate never matches and says so.
//
// Implementing Matcher needs two methods, Match(actual any) bool and FailureMessage(actual any)
// string. Adding Description() string (the assert.Describer interface) lets Not, All and Any name
// your matcher properly in their own messages instead of falling back to its Go type name.
package examples_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/getsyntegrity/go-specs/assert"
	"github.com/getsyntegrity/go-specs/specs"
)

func TestCustom_satisfyInASpec(t *testing.T) {
	specs.Describe(t, "Satisfy", func(s *specs.Spec) {
		s.It("checks an ad-hoc predicate", func(ctx *specs.Context) {
			isEven := specs.Satisfy("an even number", func(v any) bool {
				n, ok := v.(int)
				return ok && n%2 == 0
			})
			ctx.Expect(4).To(isEven)
			ctx.Expect(3).To(specs.Not(isEven))
		})
	})
}

func Example_satisfy() {
	isEven := assert.Satisfy("an even number", func(v any) bool {
		n, ok := v.(int)
		return ok && n%2 == 0
	})
	fmt.Println(isEven.Match(4))
	fmt.Println(isEven.Match(3))
	// The predicate gets the raw value, so check the type yourself.
	fmt.Println(isEven.Match("4"))
	_, failure := assert.Evaluate(isEven, 3)
	fmt.Println(failure)
	// The description names it inside Not too.
	_, failure = assert.Evaluate(assert.Not(isEven), 4)
	fmt.Println(failure)
	// Output:
	// true
	// false
	// false
	// expected 3 to satisfy "an even number"
	// expected 4 not to be satisfying "an even number"
}

func Example_satisfyMistakes() {
	// An empty description falls back to a generic name.
	_, failure := assert.Evaluate(assert.Satisfy("", func(any) bool { return false }), 1)
	fmt.Println(failure)
	// A nil predicate is reported, not a panic.
	_, failure = assert.Evaluate(assert.Satisfy("something", nil), 1)
	fmt.Println(failure)
	// Output:
	// expected 1 to satisfy the given predicate
	// Satisfy: no predicate given for "something"
}

// hasWordsMatcher is a matcher of your own: text with at least n whitespace-separated words.
type hasWordsMatcher struct{ n int }

func hasWords(n int) assert.Matcher { return hasWordsMatcher{n: n} }

func (m hasWordsMatcher) Match(actual any) bool {
	s, ok := actual.(string)
	return ok && len(strings.Fields(s)) >= m.n
}

func (m hasWordsMatcher) FailureMessage(actual any) string {
	s, ok := actual.(string)
	if !ok {
		return fmt.Sprintf("expected a string with at least %d words, got %T", m.n, actual)
	}
	return fmt.Sprintf("expected %q to have at least %d words, it has %d", s, m.n, len(strings.Fields(s)))
}

// Description makes hasWordsMatcher an assert.Describer, used by Not, All and Any.
func (m hasWordsMatcher) Description() string { return fmt.Sprintf("a text of at least %d words", m.n) }

func TestCustom_ownMatcherInASpec(t *testing.T) {
	specs.Describe(t, "custom matcher", func(s *specs.Spec) {
		s.It("plugs into Expect(...).To like any built-in", func(ctx *specs.Context) {
			ctx.Expect("the quick brown fox").To(hasWords(3))
			ctx.Expect("hi").To(specs.Not(hasWords(2)))
		})
	})
}

func Example_customMatcher() {
	fmt.Println(hasWords(3).Match("the quick brown fox"))
	_, failure := assert.Evaluate(hasWords(3), "too short")
	fmt.Println(failure)
	_, failure = assert.Evaluate(hasWords(3), 42)
	fmt.Println(failure)
	// Output:
	// true
	// expected "too short" to have at least 3 words, it has 2
	// expected a string with at least 3 words, got int
}

// Your matcher composes with the built-in ones; Description is what Not, All and Any print.
func Example_customMatcherInComposites() {
	m := assert.All(hasWords(2), assert.EndWith("."))
	fmt.Println(m.Match("hello there."))
	_, failure := assert.Evaluate(m, "hello")
	fmt.Println(failure)
	_, failure = assert.Evaluate(assert.Not(hasWords(2)), "hello there")
	fmt.Println(failure)
	// Output:
	// true
	// All: #1: "expected \"hello\" to have at least 2 words, it has 1"; #2: "expected \"hello\" to end with \".\""
	// expected hello there not to be a text of at least 2 words
}

// A matcher that does not implement Describer still works, but composites can only print its Go type.
type bareMatcher struct{}

func (bareMatcher) Match(actual any) bool            { return actual == "ok" }
func (bareMatcher) FailureMessage(actual any) string { return fmt.Sprintf("%v is not ok", actual) }

func Example_customMatcherWithoutDescriber() {
	_, failure := assert.Evaluate(assert.Not(bareMatcher{}), "ok")
	fmt.Println(failure)
	// Output:
	// expected ok not to be examples_test.bareMatcher
}
