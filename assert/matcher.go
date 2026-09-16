package assert

import (
	"fmt"
	"reflect"
	"strings"
)

// EqualComparable reports whether a and b are equal. Use for comparable types only; no reflection, inlineable.
func EqualComparable[T comparable](a, b T) bool {
	return a == b
}

// Matcher is the interface for assertion matchers used with Expect(...).To(m).
type Matcher interface {
	Match(actual any) bool
	FailureMessage(actual any) string
}

// Equal returns a matcher that expects actual to equal expected.
func Equal(expected any) Matcher {
	return &equalMatcher{expected: expected}
}

type equalMatcher struct {
	expected any
}

func (m *equalMatcher) Match(actual any) bool {
	switch a := actual.(type) {
	case int:
		if b, ok := m.expected.(int); ok {
			return a == b
		}
	case string:
		if b, ok := m.expected.(string); ok {
			return a == b
		}
	case bool:
		if b, ok := m.expected.(bool); ok {
			return a == b
		}
	case int64:
		if b, ok := m.expected.(int64); ok {
			return a == b
		}
	case float64:
		if b, ok := m.expected.(float64); ok {
			return a == b
		}
	}
	return ValuesEqual(m.expected, actual)
}

func (m *equalMatcher) FailureMessage(actual any) string {
	return fmt.Sprintf("expected %v to equal %v", actual, m.expected)
}

// NotEqual returns a matcher that expects actual not to equal expected.
func NotEqual(expected any) Matcher {
	return &notEqualMatcher{expected: expected}
}

type notEqualMatcher struct {
	expected any
}

func (m *notEqualMatcher) Match(actual any) bool {
	return !ValuesEqual(m.expected, actual)
}

func (m *notEqualMatcher) FailureMessage(actual any) string {
	return fmt.Sprintf("expected %v not to equal %v", actual, m.expected)
}

// BeNil returns a matcher that expects actual to be nil.
func BeNil() Matcher {
	return &beNilMatcher{}
}

type beNilMatcher struct{}

func (m *beNilMatcher) Match(actual any) bool {
	return IsNilValue(actual)
}

func (m *beNilMatcher) FailureMessage(actual any) string {
	return fmt.Sprintf("expected nil, got %v (%T)", actual, actual)
}

// BeTrue returns a matcher that expects actual to be the bool true.
func BeTrue() Matcher {
	return &beTrueMatcher{}
}

type beTrueMatcher struct{}

func (m *beTrueMatcher) Match(actual any) bool {
	v, ok := actual.(bool)
	return ok && v
}

func (m *beTrueMatcher) FailureMessage(actual any) string {
	return fmt.Sprintf("expected true, got %v (%T)", actual, actual)
}

// BeFalse returns a matcher that expects actual to be the bool false.
func BeFalse() Matcher {
	return &beFalseMatcher{}
}

type beFalseMatcher struct{}

func (m *beFalseMatcher) Match(actual any) bool {
	v, ok := actual.(bool)
	return ok && !v
}

func (m *beFalseMatcher) FailureMessage(actual any) string {
	return fmt.Sprintf("expected false, got %v (%T)", actual, actual)
}

// Contain returns a matcher that expects actual (string or slice) to contain expected.
func Contain(expected any) Matcher {
	return &containExpectedMatcher{expected: expected}
}

type containExpectedMatcher struct {
	expected any
}

func (m *containExpectedMatcher) Match(actual any) bool {
	switch a := actual.(type) {
	case string:
		needle, ok := m.expected.(string)
		if !ok {
			return false
		}
		return strings.Contains(a, needle)
	case []int:
		if e, ok := m.expected.(int); ok {
			for _, v := range a {
				if v == e {
					return true
				}
			}
			return false
		}
	case []string:
		if e, ok := m.expected.(string); ok {
			for _, v := range a {
				if v == e {
					return true
				}
			}
			return false
		}
	case []float64:
		if e, ok := m.expected.(float64); ok {
			for _, v := range a {
				if v == e {
					return true
				}
			}
			return false
		}
	}
	rv := reflect.ValueOf(actual)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		for i := 0; i < rv.Len(); i++ {
			if ValuesEqual(rv.Index(i).Interface(), m.expected) {
				return true
			}
		}
		return false
	}
	return false
}

func (m *containExpectedMatcher) FailureMessage(actual any) string {
	return fmt.Sprintf("expected %v to contain %v", actual, m.expected)
}

// ValuesEqual reports whether expected and actual are equal (for use by other packages).
func ValuesEqual(expected, actual any) bool {
	if expected == nil || actual == nil {
		return expected == actual
	}
	if eq, handled := fastEqualComparable(expected, actual); handled {
		return eq
	}
	return reflect.DeepEqual(expected, actual)
}

// IsNilValue reports whether value is nil or a nil pointer/slice/map/etc.
func IsNilValue(value any) bool {
	if value == nil {
		return true
	}
	rv := reflect.ValueOf(value)
	if isNilableKind(rv.Kind()) {
		return rv.IsNil()
	}
	return false
}

func isNilableKind(kind reflect.Kind) bool {
	switch kind {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Interface, reflect.Chan:
		return true
	default:
		return false
	}
}

func fastEqualComparable(expected, actual any) (bool, bool) {
	switch a := actual.(type) {
	case bool:
		if b, ok := expected.(bool); ok {
			return a == b, true
		}
	case string:
		if b, ok := expected.(string); ok {
			return a == b, true
		}
	case int:
		if b, ok := expected.(int); ok {
			return a == b, true
		}
	case int8:
		if b, ok := expected.(int8); ok {
			return a == b, true
		}
	case int16:
		if b, ok := expected.(int16); ok {
			return a == b, true
		}
	case int32:
		if b, ok := expected.(int32); ok {
			return a == b, true
		}
	case int64:
		if b, ok := expected.(int64); ok {
			return a == b, true
		}
	case uint:
		if b, ok := expected.(uint); ok {
			return a == b, true
		}
	case uint8:
		if b, ok := expected.(uint8); ok {
			return a == b, true
		}
	case uint16:
		if b, ok := expected.(uint16); ok {
			return a == b, true
		}
	case uint32:
		if b, ok := expected.(uint32); ok {
			return a == b, true
		}
	case uint64:
		if b, ok := expected.(uint64); ok {
			return a == b, true
		}
	case uintptr:
		if b, ok := expected.(uintptr); ok {
			return a == b, true
		}
	case float32:
		if b, ok := expected.(float32); ok {
			return a == b, true
		}
	case float64:
		if b, ok := expected.(float64); ok {
			return a == b, true
		}
	case complex64:
		if b, ok := expected.(complex64); ok {
			return a == b, true
		}
	case complex128:
		if b, ok := expected.(complex128); ok {
			return a == b, true
		}
	}
	return false, false
}
