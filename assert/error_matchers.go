package assert

import (
	"errors"
	"fmt"
	"reflect"
)

// Error equality in this package is oriented, not symmetric.
//
// A matcher is built around a value the caller declared as expected, and asked whether some actual
// value satisfies it. For errors the question that matches that shape is errors.Is(actual, expected):
// "does the error I got carry the identity I asked for?". An actual that wraps the expected sentinel
// satisfies it; a bare sentinel does not satisfy an expectation of some wrapped error that happens to
// contain it. Comparing in both directions would accept that second case too, inventing a relation
// errors.Is never claims.
//
// Structural comparison is wrong for errors in both directions: reflect.DeepEqual dereferences two
// *errorString pointers and compares the structs, so two unrelated errors carrying the same message
// compare as equal, while a wrapped error fails against the sentinel it wraps. See issue #183.

var errorInterfaceType = reflect.TypeOf((*error)(nil)).Elem()

// errorOperands reports whether both values carry error identity, returning them as errors.
// Both must be errors: comparing an error against a non-error is a type mismatch, which the
// structural path already answers correctly with false.
func errorOperands(expected, actual any) (expectedErr, actualErr error, ok bool) {
	expectedErr, expectedIsErr := expected.(error)
	actualErr, actualIsErr := actual.(error)
	if !expectedIsErr || !actualIsErr || expectedErr == nil || actualErr == nil {
		return nil, nil, false
	}
	return expectedErr, actualErr, true
}

// errorsMatch applies the oriented semantics: does actual carry expected's identity?
func errorsMatch(expected, actual error) bool {
	return errors.Is(actual, expected)
}

// describeError renders an error alongside its concrete type. Two distinct errors built from the
// same message are indistinguishable under %v — the reason this defect read as "expected boom to
// equal boom" — so the type is what makes a failure readable.
func describeError(err error) string {
	return fmt.Sprintf("%v (%T)", err, err)
}

// errorMismatchMessage is the single wording for a failed oriented error comparison, shared by
// every matcher so the semantics being applied are named in the failure itself.
func errorMismatchMessage(expected, actual error) string {
	return fmt.Sprintf("expected error %s to match %s — errors.Is(actual, expected) is false",
		describeError(actual), describeError(expected))
}

// MatchError returns a matcher that expects actual to be an error carrying target's identity,
// using errors.Is. This is the explicit spelling of the semantics Equal applies to errors.
func MatchError(target error) Matcher {
	return &matchErrorMatcher{target: target}
}

type matchErrorMatcher struct {
	target error
}

func (m *matchErrorMatcher) Match(actual any) bool {
	actualErr, ok := actual.(error)
	if !ok || actualErr == nil || m.target == nil {
		return false
	}
	return errorsMatch(m.target, actualErr)
}

func (m *matchErrorMatcher) FailureMessage(actual any) string {
	actualErr, ok := actual.(error)
	if !ok {
		return fmt.Sprintf("expected an error matching %s, got %v (%T) — errors.Is needs an error actual",
			describeError(m.target), actual, actual)
	}
	if actualErr == nil {
		return fmt.Sprintf("expected an error matching %s, got a nil error — errors.Is(actual, expected) is false",
			describeError(m.target))
	}
	return errorMismatchMessage(m.target, actualErr)
}

// Description implements Describer; see equalMatcher.Description in matcher.go.
func (m *matchErrorMatcher) Description() string {
	return fmt.Sprintf("an error matching %s", describeError(m.target))
}

// MatchErrorAs returns a matcher that expects actual to be an error assignable to target via
// errors.As. target must be a non-nil pointer to a type implementing error, or to an interface —
// the same contract errors.As requires. On a match, target is populated.
func MatchErrorAs(target any) Matcher {
	return &matchErrorAsMatcher{target: target}
}

type matchErrorAsMatcher struct {
	target any
}

func (m *matchErrorAsMatcher) Match(actual any) bool {
	actualErr, ok := actual.(error)
	// errors.As panics on an invalid target. A matcher's job is to report the truth about an
	// assertion, so an unusable target becomes a failed match with an explanatory message rather
	// than a panic that takes the suite down.
	if !ok || actualErr == nil || !isErrorAsTarget(m.target) {
		return false
	}
	return errors.As(actualErr, m.target)
}

func (m *matchErrorAsMatcher) FailureMessage(actual any) string {
	if !isErrorAsTarget(m.target) {
		return fmt.Sprintf("MatchErrorAs needs a non-nil pointer to a type implementing error (or to an interface), got %v (%T)",
			m.target, m.target)
	}
	actualErr, ok := actual.(error)
	if !ok {
		return fmt.Sprintf("expected an error assignable to %T, got %v (%T) — errors.As needs an error actual",
			m.target, actual, actual)
	}
	if actualErr == nil {
		return fmt.Sprintf("expected an error assignable to %T, got a nil error", m.target)
	}
	return fmt.Sprintf("expected %s to unwrap to %T — errors.As(actual, target) is false",
		describeError(actualErr), m.target)
}

// Description implements Describer; see equalMatcher.Description in matcher.go. It reuses %T on
// m.target directly, the same rendering FailureMessage above already uses for the same field.
func (m *matchErrorAsMatcher) Description() string {
	return fmt.Sprintf("an error assignable to %T", m.target)
}

// isErrorAsTarget mirrors errors.As's own target validity rules, so the matcher can reject an
// unusable target instead of letting errors.As panic on it.
func isErrorAsTarget(target any) bool {
	if target == nil {
		return false
	}
	val := reflect.ValueOf(target)
	typ := val.Type()
	if typ.Kind() != reflect.Pointer || val.IsNil() {
		return false
	}
	elem := typ.Elem()
	return elem.Kind() == reflect.Interface || elem.Implements(errorInterfaceType)
}
