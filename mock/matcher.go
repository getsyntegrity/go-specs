package mock

import "github.com/getsyntegrity/go-specs/assert"

// ArgMatcher matches a single argument in call verification.
type ArgMatcher interface {
	Match(v any) bool
}

// Any returns a matcher that matches any value.
func Any() ArgMatcher {
	return anyMatcher{}
}

type anyMatcher struct{}

func (anyMatcher) Match(v any) bool {
	return true
}

// Equal returns a matcher that matches values equal to expected, with the same semantics as the
// assert.Equal matcher (assert.ValuesEqual): when both values are errors it asks
// errors.Is(actual, expected), so an argument wrapping the expected sentinel matches and an
// unrelated error that merely carries the same message does not; everything else compares
// structurally (reflect.DeepEqual). See issue #183 for why errors are not compared structurally.
func Equal(expected any) ArgMatcher {
	return &equalMatcher{expected: expected}
}

type equalMatcher struct {
	expected any
}

func (m *equalMatcher) Match(v any) bool {
	return assert.ValuesEqual(m.expected, v)
}
