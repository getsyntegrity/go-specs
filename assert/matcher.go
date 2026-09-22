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

// Matcher is the interface for assertion matchers used with Expect(...).To(m). The DSL evaluates a
// matcher exactly once per assertion: once to decide the result, and, only on failure, once more to
// build the failure message — the same shape Match/FailureMessage always had. Evaluate (see
// assert/evaluate.go) is that rule exported for direct callers; the two To methods in specs apply it
// inline so the zero-allocation typed path keeps calling Match without an extra hop, which costs
// nothing in behaviour and about 2ns per assertion if routed through the function instead.
// A composite (Not, All, Any; see assert/composite_matchers.go) implements Evaluate's
// optional Evaluator interface so it, too, evaluates each of its own sub-matchers exactly once per
// assertion, however deeply it nests, rather than once to decide and again, on its own failure, to
// build its message.
//
// That guarantee matters because a matcher may deliberately carry a side effect: MatchErrorAs, in
// this very package, calls errors.As(actual, target), which populates target as part of matching.
// Evaluating a matcher twice for one assertion — which the old Match-then-FailureMessage composite
// path did — would populate it twice; the single-pass Evaluate seam is what keeps that to once. Match
// and FailureMessage stay separately callable exactly as before, since both remain part of this
// interface and third-party code may call either directly; a Matcher implementation does not need to
// implement Evaluator to be evaluated correctly through Evaluate — the default path already calls
// Match once and, only on failure, FailureMessage once, which is one evaluation already.
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
	return EqualFailureMessage(m.expected, actual)
}

// Description implements Describer so composites (Not, All, Any) can name this matcher in their own
// failure messages without quoting a FailureMessage that may describe a comparison that succeeded.
func (m *equalMatcher) Description() string {
	return fmt.Sprintf("equal to %v", m.expected)
}

// EqualFailureMessage renders the failure for a mismatch under ValuesEqual's semantics. It is
// exported so the DSL's inlined comparison paths report identically to the matcher — a divergence
// between the two wordings is exactly as confusing as a divergence between the two comparisons.
func EqualFailureMessage(expected, actual any) string {
	if expectedErr, actualErr, ok := errorOperands(expected, actual); ok {
		return errorMismatchMessage(expectedErr, actualErr)
	}
	renderedActual, renderedExpected := describeMismatch(actual, expected)
	return fmt.Sprintf("expected %s to equal %s", renderedActual, renderedExpected)
}

// describeMismatch renders both sides, falling back to type-qualified forms when %v alone makes
// them indistinguishable. A failure reading "expected boom to equal boom" tells the reader nothing.
func describeMismatch(actual, expected any) (string, string) {
	renderedActual, renderedExpected := fmt.Sprintf("%v", actual), fmt.Sprintf("%v", expected)
	if renderedActual != renderedExpected {
		return renderedActual, renderedExpected
	}
	return fmt.Sprintf("%v (%T)", actual, actual), fmt.Sprintf("%v (%T)", expected, expected)
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
	if expectedErr, actualErr, ok := errorOperands(m.expected, actual); ok {
		return fmt.Sprintf("expected error %s not to match %s — errors.Is(actual, expected) is true",
			describeError(actualErr), describeError(expectedErr))
	}
	// No disambiguation here: a NotEqual failure means the two values matched, so rendering
	// identically is the expected outcome rather than the confusing one.
	return fmt.Sprintf("expected %v not to equal %v", actual, m.expected)
}

// Description implements Describer; see equalMatcher.Description.
func (m *notEqualMatcher) Description() string {
	return fmt.Sprintf("not equal to %v", m.expected)
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

// Description implements Describer; see equalMatcher.Description.
func (m *beNilMatcher) Description() string {
	return "nil"
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

// Description implements Describer; see equalMatcher.Description.
func (m *beTrueMatcher) Description() string {
	return "true"
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

// Description implements Describer; see equalMatcher.Description.
func (m *beFalseMatcher) Description() string {
	return "false"
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
			// The element is the actual and m.expected is the expected, so they go in that order.
			// This read reversed while everything compared structurally, because reflect.DeepEqual
			// is symmetric and hid it; ValuesEqual's error semantics are oriented and would not.
			if ValuesEqual(m.expected, rv.Index(i).Interface()) {
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

// Description implements Describer; see equalMatcher.Description.
func (m *containExpectedMatcher) Description() string {
	return fmt.Sprintf("containing %v", m.expected)
}

// ValuesEqual reports whether actual satisfies expected (for use by other packages).
//
// The argument order is the contract: this comparison is oriented. When both operands are errors it
// asks errors.Is(actual, expected), so an actual wrapping the expected sentinel satisfies it and an
// unrelated error carrying the same message does not. Everything else keeps the existing structural
// semantics — the comparable fast path, then reflect.DeepEqual.
//
// For a symmetric, unoriented error comparison see EqualValues.
func ValuesEqual(expected, actual any) bool {
	if expected == nil || actual == nil {
		return expected == actual
	}
	if eq, handled := fastEqualComparable(expected, actual); handled {
		return eq
	}
	if expectedErr, actualErr, ok := errorOperands(expected, actual); ok {
		return errorsMatch(expectedErr, actualErr)
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
