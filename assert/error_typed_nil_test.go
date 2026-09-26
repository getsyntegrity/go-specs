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
