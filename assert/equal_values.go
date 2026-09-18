package assert

import (
	"errors"
	"reflect"
	"testing"
)

// EqualValues reports whether a and b are related. For use as a test helper (accepts testing.TB).
//
// This helper is deliberately SYMMETRIC, and that is the difference between it and ValuesEqual.
// Its parameters are named a and b, not expected and actual: neither side is privileged, so for
// errors it asks whether either one wraps the other. ValuesEqual, which backs the Equal/NotEqual/
// Contain matchers, is ORIENTED — it asks only errors.Is(actual, expected), because a matcher is
// built around a value the caller declared as expected.
//
// Reach for EqualValues when you are asking "are these two errors related at all?" and for
// ValuesEqual (or the MatchError matcher) when you are asking "does this actual satisfy the
// identity I expected?". Non-error values behave identically in both.
func EqualValues(t testing.TB, a, b any) bool {
	t.Helper()
	return equalValuesSymmetric(a, b)
}

func equalValuesSymmetric(a, b any) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	errA, aIsErr := a.(error)
	errB, bIsErr := b.(error)
	if aIsErr && bIsErr {
		return errors.Is(errA, errB) || errors.Is(errB, errA)
	}
	return reflect.DeepEqual(a, b)
}
