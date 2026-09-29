package assert

import (
	"fmt"
	"reflect"
)

// HaveLen returns a matcher that expects actual to have length n. Supported actuals are a string
// (its byte length), a slice, an array, a map and a chan (its buffered element count), with typed
// fast paths for the common ones and reflection only as the fallback. A nil slice, map or chan has
// length zero. An actual with no length (an int, a struct, nil, ...) never matches, and its
// FailureMessage says so instead of reporting a length mismatch. Pointers to arrays are not
// supported: dereferencing would make a nil pointer's length ambiguous.
func HaveLen(n int) Matcher {
	return &haveLenMatcher{n: n}
}

type haveLenMatcher struct {
	n int
}

func (m *haveLenMatcher) Match(actual any) bool {
	got, ok := lengthOf(actual)
	return ok && got == m.n
}

func (m *haveLenMatcher) FailureMessage(actual any) string {
	got, ok := lengthOf(actual)
	if !ok {
		return noLengthMessage("HaveLen", actual)
	}
	return fmt.Sprintf("expected %v to have length %d, got length %d", actual, m.n, got)
}

// Description implements Describer; see equalMatcher.Description.
func (m *haveLenMatcher) Description() string {
	return fmt.Sprintf("having length %d", m.n)
}

// BeEmpty returns a matcher that expects actual to have length zero, for the same kinds HaveLen
// supports. A nil slice, map or chan is empty; an actual with no length never matches.
func BeEmpty() Matcher {
	return &beEmptyMatcher{}
}

type beEmptyMatcher struct{}

func (m *beEmptyMatcher) Match(actual any) bool {
	got, ok := lengthOf(actual)
	return ok && got == 0
}

func (m *beEmptyMatcher) FailureMessage(actual any) string {
	got, ok := lengthOf(actual)
	if !ok {
		return noLengthMessage("BeEmpty", actual)
	}
	return fmt.Sprintf("expected %v to be empty, got length %d", actual, got)
}

// Description implements Describer; see equalMatcher.Description.
func (m *beEmptyMatcher) Description() string {
	return "empty"
}

func noLengthMessage(matcher string, actual any) string {
	return fmt.Sprintf("%s: %T has no length", matcher, actual)
}

// lengthOf reports actual's length and whether actual has one. The type switch covers the common
// concrete types without reflection; reflect handles named and less common slice, array, map and
// chan types.
func lengthOf(actual any) (int, bool) {
	switch a := actual.(type) {
	case string:
		return len(a), true
	case []any:
		return len(a), true
	case []string:
		return len(a), true
	case []int:
		return len(a), true
	case []byte:
		return len(a), true
	case map[string]any:
		return len(a), true
	}
	if actual == nil {
		return 0, false
	}
	rv := reflect.ValueOf(actual)
	switch rv.Kind() {
	case reflect.String, reflect.Slice, reflect.Array, reflect.Map, reflect.Chan:
		return rv.Len(), true
	default:
		return 0, false
	}
}
