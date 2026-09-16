package assert

import (
	"errors"
	"reflect"
	"testing"
)

// EqualValues reports whether a and b are equal. For use as a test helper (accepts testing.TB).
// For error values, uses errors.Is for wrapped error comparison; otherwise uses reflect.DeepEqual.
// Moved from matchers package; assert is the single source of truth for equality helpers.
func EqualValues(t testing.TB, a, b any) bool {
	t.Helper()
	return equalValues(a, b)
}

func equalValues(a, b any) bool {
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
