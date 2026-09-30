// error_matchers_test.go shows how to assert on errors: MatchError and MatchErrorAs, plus how
// Equal treats errors.
//
// Use MatchError when you care about the identity of an error ("is this ErrNotFound, even if it
// was wrapped on the way up?") and MatchErrorAs when you care about its type and want to read its
// fields ("is this a *ValidationError, and which field failed?").
//
// Semantics:
//   - MatchError(target) is errors.Is(actual, target). The comparison is oriented: an actual that
//     wraps the sentinel matches, but a bare sentinel does not match an expectation of some wrapped
//     error. Two different errors.New calls with the same text are different errors.
//   - MatchErrorAs(&target) is errors.As(actual, &target). On a match the target variable is
//     populated, so you can inspect it afterwards. The target must be a non-nil pointer to a type
//     implementing error, or to an interface; anything else fails the assertion with an
//     explanation instead of panicking.
//   - A nil error never matches either matcher. Assert success with ctx.Expect(err).To(specs.BeNil()).
//   - specs.Equal(err) applies the same errors.Is rule when both sides are errors.
package examples_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/getsyntegrity/go-specs/assert"
	"github.com/getsyntegrity/go-specs/specs"
)

var errNotFound = errors.New("not found")

type errValidation struct{ Field string }

func (e *errValidation) Error() string { return "invalid " + e.Field }

func findUser(id int) error {
	if id == 0 {
		return &errValidation{Field: "id"}
	}
	if id < 0 {
		return fmt.Errorf("find user %d: %w", id, errNotFound)
	}
	return nil
}

// In a spec: identity through wrapping, type with field access, and the no-error case.
func TestErrors_inASpec(t *testing.T) {
	specs.Describe(t, "findUser", func(s *specs.Spec) {
		s.It("wraps the sentinel", func(ctx *specs.Context) {
			ctx.Expect(findUser(-1)).To(specs.MatchError(errNotFound))
		})
		s.It("returns a typed validation error", func(ctx *specs.Context) {
			var target *errValidation
			ctx.Expect(findUser(0)).To(specs.MatchErrorAs(&target))
			ctx.Expect(target.Field).To(specs.Equal("id"))
		})
		s.It("returns no error for a valid id", func(ctx *specs.Context) {
			ctx.Expect(findUser(7)).To(specs.BeNil())
		})
	})
}

func Example_matchError() {
	wrapped := findUser(-1)
	fmt.Println(assert.MatchError(errNotFound).Match(errNotFound))
	fmt.Println(assert.MatchError(errNotFound).Match(wrapped))
	// Same text, different identity.
	fmt.Println(assert.MatchError(errNotFound).Match(errors.New("not found")))
	// Oriented: a bare sentinel does not satisfy an expectation of the wrapper.
	fmt.Println(assert.MatchError(wrapped).Match(errNotFound))
	// Output:
	// true
	// true
	// false
	// false
}

func Example_matchErrorFailureMessages() {
	_, failure := assert.Evaluate(assert.MatchError(errNotFound), errors.New("not found"))
	fmt.Println(failure)
	_, failure = assert.Evaluate(assert.MatchError(errNotFound), nil)
	fmt.Println(failure)
	_, failure = assert.Evaluate(assert.MatchError(errNotFound), "not found")
	fmt.Println(failure)
	// Output:
	// expected error not found (*errors.errorString) to match not found (*errors.errorString) — errors.Is(actual, expected) is false
	// expected an error matching not found (*errors.errorString), got a nil error — errors.Is(actual, expected) is false
	// expected an error matching not found (*errors.errorString), got not found (string) — errors.Is needs an error actual
}

func Example_matchErrorAs() {
	err := fmt.Errorf("create order: %w", &errValidation{Field: "email"})

	var target *errValidation
	fmt.Println(assert.MatchErrorAs(&target).Match(err))
	// On a match the target is populated, even through wrapping.
	fmt.Println(target.Field)

	// A target of an interface type works too.
	var iface interface{ Error() string }
	fmt.Println(assert.MatchErrorAs(&iface).Match(err))

	// A different type does not match.
	var other *errOther
	fmt.Println(assert.MatchErrorAs(&other).Match(err))
	// Output:
	// true
	// email
	// true
	// false
}

type errOther struct{}

func (*errOther) Error() string { return "other" }

func Example_matchErrorAsFailureMessages() {
	var other *errOther
	_, failure := assert.Evaluate(assert.MatchErrorAs(&other), findUser(0))
	fmt.Println(failure)
	_, failure = assert.Evaluate(assert.MatchErrorAs(&other), nil)
	fmt.Println(failure)
	// An unusable target is reported instead of panicking inside errors.As.
	_, failure = assert.Evaluate(assert.MatchErrorAs(errNotFound), findUser(0))
	fmt.Println(failure)
	// Output:
	// expected invalid id (*examples_test.errValidation) to unwrap to **examples_test.errOther — errors.As(actual, target) is false
	// expected an error assignable to **examples_test.errOther, got a nil error
	// MatchErrorAs needs a non-nil pointer to a type implementing error (or to an interface), got not found (*errors.errorString)
}

// specs.Equal on two errors uses errors.Is too, and tells same-looking errors apart in the message.
func Example_equalOnErrors() {
	fmt.Println(assert.Equal(errNotFound).Match(findUser(-1)))
	_, failure := assert.Evaluate(assert.Equal(errors.New("boom")), errors.New("boom"))
	fmt.Println(failure)
	// Output:
	// true
	// expected error boom (*errors.errorString) to match boom (*errors.errorString) — errors.Is(actual, expected) is false
}
