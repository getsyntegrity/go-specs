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

// EqualFailureMessage renders the failure for a mismatch under ValuesEqual's semantics. When the
// values are composites (struct, map, slice, array, pointer) a "differences:" section follows the
// first line, listing the path, expected value and actual value of each difference; see
// structural_diff.go for the bounds. Errors and scalars keep their single-line messages. It is
// exported so the DSL's inlined comparison paths report identically to the matcher — a divergence
// between the two wordings is exactly as confusing as a divergence between the two comparisons.
func EqualFailureMessage(expected, actual any) string {
	if expectedErr, actualErr, ok := errorOperands(expected, actual); ok {
		return errorMismatchMessage(expectedErr, actualErr)
	}
	var renderedActual, renderedExpected string
	if !safeForFmt(reflect.ValueOf(actual)) || !safeForFmt(reflect.ValueOf(expected)) {
		// %v would recurse forever through a self-containing map or slice and orders NaN keys by iteration; the diff renderer is bounded and deterministic.
		renderedActual = renderDiffValue(reflect.ValueOf(actual), 0)
		renderedExpected = renderDiffValue(reflect.ValueOf(expected), 0)
	} else {
		renderedActual, renderedExpected = describeMismatch(actual, expected)
	}
	msg := fmt.Sprintf("expected %s to equal %s", renderedActual, renderedExpected)
	if diff := structuralDiff(expected, actual); diff != "" {
		msg += "\n" + diff
	}
	return msg
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

// Contain returns a matcher that expects actual (a string, or a slice/array via the []int, []string
// and []float64 fast paths or the reflect fallback) to contain expected. Two failure modes are not
// "does not contain": an actual Contain cannot support at all (a scalar, a map, nil, a struct, ...)
// and an expected value whose type could never match the actual's element type (a non-string needle
// against a string, or an element type mismatch against a slice/array). FailureMessage names both
// explicitly instead of reporting them as an indistinguishable missing element (issue #277). Map-key
// containment is not supported; an unsupported-actual diagnosis is what a map actual gets today.
func Contain(expected any) Matcher {
	return &containExpectedMatcher{expected: expected}
}

type containExpectedMatcher struct {
	expected any
}

func (m *containExpectedMatcher) Match(actual any) bool {
	matched, _ := m.diagnose(actual, false)
	return matched
}

func (m *containExpectedMatcher) FailureMessage(actual any) string {
	_, reason := m.diagnose(actual, true)
	if reason == "" {
		return fmt.Sprintf("expected %v to contain %v", actual, m.expected)
	}
	return fmt.Sprintf("expected %v to contain %v — %s", actual, m.expected, reason)
}

// diagnose decides the match and, only when explain is true, also classifies a failure: reason
// stays empty for a match or for a genuine missing element (the container is one Contain supports
// and the expected value has a type that could match it, it just is not present), and is set to an
// actionable explanation for an unsupported actual type or an expected value whose type rules out
// any match. explain is false from Match, which does not need the string, so the fast, allocation-
// free comparisons stay the only cost on that hot path; FailureMessage passes true and pays for the
// message only on the failure path that already builds one.
func (m *containExpectedMatcher) diagnose(actual any, explain bool) (matched bool, reason string) {
	switch a := actual.(type) {
	case string:
		needle, ok := m.expected.(string)
		if !ok {
			if explain {
				reason = incompatibleExpectedReason("string", "string", m.expected)
			}
			return false, reason
		}
		return strings.Contains(a, needle), ""
	case []int:
		return containTypedSlice(a, "int", m.expected, explain)
	case []string:
		return containTypedSlice(a, "string", m.expected, explain)
	case []float64:
		return containTypedSlice(a, "float64", m.expected, explain)
	}

	rv := reflect.ValueOf(actual)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		elemType := rv.Type().Elem()
		if elemType.Kind() != reflect.Interface {
			expectedType := reflect.TypeOf(m.expected)
			if expectedType == nil || !expectedType.AssignableTo(elemType) {
				if explain {
					reason = incompatibleExpectedReason(rv.Type().String(), elemType.String(), m.expected)
				}
				return false, reason
			}
		}
		for i := 0; i < rv.Len(); i++ {
			// The element is the actual and m.expected is the expected, so they go in that order.
			// This read reversed while everything compared structurally, because reflect.DeepEqual
			// is symmetric and hid it; ValuesEqual's error semantics are oriented and would not.
			if ValuesEqual(m.expected, rv.Index(i).Interface()) {
				return true, ""
			}
		}
		return false, ""
	default:
		if explain {
			reason = unsupportedActualReason(actual)
		}
		return false, reason
	}
}

// containTypedSlice backs Contain's []int/[]string/[]float64 fast paths: same lookup either way, so
// the diagnosis for a wrong-typed needle is written once instead of three times.
func containTypedSlice[T comparable](a []T, elemTypeName string, expected any, explain bool) (matched bool, reason string) {
	e, ok := expected.(T)
	if !ok {
		if explain {
			reason = incompatibleExpectedReason(fmt.Sprintf("%T", a), elemTypeName, expected)
		}
		return false, reason
	}
	for _, v := range a {
		if v == e {
			return true, ""
		}
	}
	return false, ""
}

// incompatibleExpectedReason explains that expected's type can never match actualTypeDesc's
// elements (or its own characters, for a string actual), so a miss should not be read as "not
// present" but as "type mismatch".
func incompatibleExpectedReason(actualTypeDesc, elemTypeName string, expected any) string {
	return fmt.Sprintf("%s actual needs %s %s expected value, got %T", actualTypeDesc, article(elemTypeName), elemTypeName, expected)
}

// unsupportedActualReason explains that Contain has no matching strategy for actual's type at all —
// distinct from incompatibleExpectedReason, where a strategy exists but expected's type rules it out.
func unsupportedActualReason(actual any) string {
	return fmt.Sprintf("%T is not a supported Contain actual (want string, slice, or array)", actual)
}

// article picks "a" or "an" for the type name that follows it in a diagnosis (e.g. "a string",
// "an int64"). It is a plain vowel-letter check, not a pronunciation rule, which is the same
// simplification English style guides make for this kind of generated text.
func article(typeName string) string {
	if typeName == "" {
		return "a"
	}
	switch typeName[0] {
	case 'a', 'e', 'i', 'o', 'u', 'A', 'E', 'I', 'O', 'U':
		return "an"
	default:
		return "a"
	}
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
