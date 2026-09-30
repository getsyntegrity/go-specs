package assert

import (
	"fmt"
	"reflect"
)

// BeZero returns a matcher that expects actual to be the zero value of its type, by the same rule as
// reflect.Value.IsZero: 0, "", false, a nil pointer, slice, map, chan or func, a struct or array whose
// every field or element is zero. A nil interface (an untyped nil, or a nil error) is zero. Worth
// knowing: an empty but non-nil slice or map is not zero (use BeEmpty for that), and a float -0.0 is
// zero because it equals 0. time.Time{} is zero.
func BeZero() Matcher {
	return &beZeroMatcher{}
}

type beZeroMatcher struct{}

func (m *beZeroMatcher) Match(actual any) bool {
	switch a := actual.(type) {
	case nil:
		return true
	case int:
		return a == 0
	case string:
		return a == ""
	case bool:
		return !a
	}
	return reflect.ValueOf(actual).IsZero()
}

func (m *beZeroMatcher) FailureMessage(actual any) string {
	return fmt.Sprintf("expected %v to be the zero value of %T", actual, actual)
}

// Description implements Describer; see equalMatcher.Description.
func (m *beZeroMatcher) Description() string {
	return "the zero value"
}

// Satisfy returns a matcher that expects pred(actual) to be true, the escape hatch for a check no
// built-in matcher states. description names the expectation in failure messages ("expected 3 to
// satisfy \"is even\"") and in the messages of Not, All and Any; an empty one falls back to "the
// given predicate". The predicate receives the actual as is, including nil, and runs once per Match:
// a panic inside it propagates, since hiding it would hide the bug in the predicate. A nil pred is
// reported instead of panicking: the matcher never matches and its failure message says no predicate
// was given.
func Satisfy(description string, pred func(any) bool) Matcher {
	return &satisfyMatcher{description: description, pred: pred}
}

type satisfyMatcher struct {
	description string
	pred        func(any) bool
}

func (m *satisfyMatcher) Match(actual any) bool {
	return m.pred != nil && m.pred(actual)
}

func (m *satisfyMatcher) FailureMessage(actual any) string {
	if m.pred == nil {
		if m.description == "" {
			return "Satisfy: no predicate given"
		}
		return fmt.Sprintf("Satisfy: no predicate given for %q", m.description)
	}
	// The actual is arbitrary user data: renderBounded keeps %v for ordinary values and cannot
	// overflow the stack on a self-containing map or slice.
	return fmt.Sprintf("expected %s to satisfy %s", renderBounded(actual, "%v"), m.named())
}

// Description implements Describer; see equalMatcher.Description.
func (m *satisfyMatcher) Description() string {
	return "satisfying " + m.named()
}

func (m *satisfyMatcher) named() string {
	if m.description == "" {
		return "the given predicate"
	}
	return fmt.Sprintf("%q", m.description)
}
