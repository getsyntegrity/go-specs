package assert

import (
	"errors"
	"testing"
)

// derefError reads its receiver in Unwrap, so errors.Is on a nil *derefError dereferences nil.
type derefError struct{ inner error }

func (e *derefError) Error() string { return "deref" }
func (e *derefError) Unwrap() error { return e.inner }

// A typed nil pointer stored in an error must fail an error comparison, not panic it. errorsMatch
// answers it with == instead of calling the nil receiver's methods through errors.Is.
func TestErrorComparisonsFailInsteadOfPanickingOnATypedNilPointer(t *testing.T) {
	var typedNil *derefError
	sentinel := errors.New("boom")

	checks := map[string]func() bool{
		"MatchError":  func() bool { return MatchError(sentinel).Match(error(typedNil)) },
		"Equal":       func() bool { return Equal(sentinel).Match(error(typedNil)) },
		"ValuesEqual": func() bool { return ValuesEqual(sentinel, error(typedNil)) },
		"EqualValues": func() bool { return equalValuesSymmetric(error(typedNil), sentinel) },
	}
	for name, check := range checks {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s panicked on a typed nil actual: %v", name, r)
				}
			}()
			if check() {
				t.Errorf("%s matched a typed nil actual against a sentinel", name)
			}
		}()
	}

	if !ValuesEqual(error(typedNil), error(typedNil)) {
		t.Error("expected a typed nil to equal itself")
	}
}

// nilSliceError is slice-backed, so its type is not comparable. Its methods have value receivers and
// are safe on a nil slice.
type nilSliceError []string

func (e nilSliceError) Error() string { return "slice error" }

// Codex review on #259: IsNilValue is true for a nil slice too, and == on two errors holding the same
// incomparable type panics. The typed-nil shortcut must not take that value into ==; errors.Is, which
// skips == for incomparable values, answers it instead.
func TestErrorComparisonsDoNotPanicOnNilSliceBackedErrors(t *testing.T) {
	nilSlice := error(nilSliceError(nil))
	checks := map[string]func() bool{
		"ValuesEqual nil vs nil":       func() bool { return ValuesEqual(nilSlice, error(nilSliceError(nil))) },
		"ValuesEqual nil vs non-nil":   func() bool { return ValuesEqual(error(nilSliceError{"a"}), nilSlice) },
		"Equal":                        func() bool { return Equal(nilSlice).Match(error(nilSliceError(nil))) },
		"NotEqual":                     func() bool { return !NotEqual(nilSlice).Match(error(nilSliceError(nil))) },
		"MatchError":                   func() bool { return MatchError(nilSlice).Match(error(nilSliceError(nil))) },
		"EqualValues (symmetric path)": func() bool { return equalValuesSymmetric(nilSlice, error(nilSliceError(nil))) },
	}
	for name, check := range checks {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s panicked on a nil slice-backed error: %v", name, r)
				}
			}()
			// errors.Is reports false for two incomparable values with no Is method, so none of these
			// relate the two errors.
			if check() {
				t.Errorf("%s related two slice-backed errors that errors.Is does not relate", name)
			}
		}()
	}
}
