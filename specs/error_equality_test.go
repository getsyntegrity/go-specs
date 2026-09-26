package specs

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

// error_equality_test.go pins the DSL half of issue #183.
//
// Expectation.ToEqual carries its own inlined comparison that ended in reflect.DeepEqual, so it had
// the same silent false positive as the Equal matcher and had to be fixed alongside it — otherwise
// ctx.Expect(err).To(Equal(sentinel)) and ctx.Expect(err).ToEqual(sentinel) would disagree about the
// same two errors. The orientation is the same everywhere: errors.Is(actual, expected).

func expectToEqual(actual, expected any) *capturingBackend {
	backend := &capturingBackend{}
	(&Context{backend: backend}).Expect(actual).ToEqual(expected)
	return backend
}

func expectTo(actual any, m Matcher) *capturingBackend {
	backend := &capturingBackend{}
	(&Context{backend: backend}).Expect(actual).To(m)
	return backend
}

func TestExpectToEqualRejectsDistinctErrorsSharingAMessage(t *testing.T) {
	sentinel := errors.New("boom")
	impostor := errors.New("boom")

	if b := expectToEqual(impostor, sentinel); !b.failed {
		t.Fatal("expected ToEqual to fail for an unrelated error that merely shares the message")
	}
	if b := expectToEqual(errors.New("EOF"), io.EOF); !b.failed {
		t.Fatal("expected ToEqual to fail for an unrelated error whose message reads 'EOF'")
	}
}

func TestExpectToEqualAcceptsAnActualWrappingTheExpectedSentinel(t *testing.T) {
	sentinel := errors.New("boom")
	wrapped := fmt.Errorf("layer: %w", sentinel)

	if b := expectToEqual(wrapped, sentinel); b.failed {
		t.Fatalf("expected ToEqual to pass for an actual wrapping the sentinel, got failure: %q", b.message)
	}
}

func TestExpectToEqualRejectsABareSentinelAgainstAWrappedExpectation(t *testing.T) {
	sentinel := errors.New("boom")
	wrapped := fmt.Errorf("layer: %w", sentinel)

	if b := expectToEqual(sentinel, wrapped); !b.failed {
		t.Fatal("expected ToEqual to fail: errors.Is(sentinel, wrapped) is false")
	}
}

// The matcher route and the inlined ToEqual route must agree; a disagreement is exactly the class
// of bug #183 was.
func TestExpectToEqualAgreesWithTheEqualMatcherOnErrors(t *testing.T) {
	sentinel := errors.New("boom")
	impostor := errors.New("boom")
	wrapped := fmt.Errorf("layer: %w", sentinel)

	cases := []struct {
		name     string
		actual   error
		expected error
	}{
		{"unrelated errors sharing a message", impostor, sentinel},
		{"actual wraps expected", wrapped, sentinel},
		{"expected wraps actual", sentinel, wrapped},
		{"same error", sentinel, sentinel},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			viaToEqual := expectToEqual(tc.actual, tc.expected).failed
			viaMatcher := expectTo(tc.actual, Equal(tc.expected)).failed
			if viaToEqual != viaMatcher {
				t.Fatalf("ToEqual and Equal disagree: ToEqual failed=%v, Equal failed=%v", viaToEqual, viaMatcher)
			}
		})
	}
}

func TestExpectToEqualFailureMessageDistinguishesIdenticalRenderings(t *testing.T) {
	sentinel := errors.New("boom")
	impostor := errors.New("boom")

	b := expectToEqual(impostor, sentinel)
	if !b.failed {
		t.Fatal("expected a failure")
	}
	if strings.Contains(b.message, "expected boom to equal boom") {
		t.Fatalf("failure message does not distinguish the two errors: %q", b.message)
	}
	if !strings.Contains(b.message, "errors.Is") {
		t.Fatalf("expected the message to name the semantics being applied, got %q", b.message)
	}
}

func TestMatchErrorIsExposedThroughTheDSL(t *testing.T) {
	sentinel := errors.New("boom")
	wrapped := fmt.Errorf("layer: %w", sentinel)

	if b := expectTo(wrapped, MatchError(sentinel)); b.failed {
		t.Fatalf("expected specs.MatchError to accept a wrapping actual, got failure: %q", b.message)
	}
	if b := expectTo(errors.New("boom"), MatchError(sentinel)); !b.failed {
		t.Fatal("expected specs.MatchError to reject an unrelated error sharing the message")
	}
}

type dslNotFoundError struct{ Name string }

func (e *dslNotFoundError) Error() string { return "not found: " + e.Name }

func TestMatchErrorAsIsExposedThroughTheDSL(t *testing.T) {
	wrapped := fmt.Errorf("layer: %w", &dslNotFoundError{Name: "user"})

	var target *dslNotFoundError
	if b := expectTo(wrapped, MatchErrorAs(&target)); b.failed {
		t.Fatalf("expected specs.MatchErrorAs to unwrap the concrete type, got failure: %q", b.message)
	}
	if target == nil || target.Name != "user" {
		t.Fatalf("expected MatchErrorAs to populate the target, got %+v", target)
	}
}

// Issue #237: EqualTo and ExpectT(...).ToEqual used to compare errors with == only, so moving an
// error assertion from ctx.Expect to the typed path for speed silently rejected a wrapped sentinel.
// They now fall back to the same oriented errors.Is(actual, expected) — on the failure branch only,
// so the passing fast path is still a single == that allocates nothing.

func typedToEqual(actual, expected error) *capturingBackend {
	backend := &capturingBackend{}
	ExpectT(&Context{backend: backend}, actual).ToEqual(expected)
	return backend
}

func typedEqualTo(actual, expected error) *capturingBackend {
	backend := &capturingBackend{}
	EqualTo(&Context{backend: backend}, actual, expected)
	return backend
}

func TestTypedEqualityAcceptsAWrappedSentinel(t *testing.T) {
	sentinel := errors.New("not found")
	wrapped := fmt.Errorf("repo: %w", sentinel)

	if b := typedToEqual(wrapped, sentinel); b.failed {
		t.Errorf("expected ExpectT(ctx, wrapped).ToEqual(sentinel) to pass like ctx.Expect does, got %q", b.message)
	}
	if b := typedEqualTo(wrapped, sentinel); b.failed {
		t.Errorf("expected EqualTo(ctx, wrapped, sentinel) to pass like ctx.Expect does, got %q", b.message)
	}
}

func TestTypedEqualityRejectsUnrelatedErrorsSharingAMessage(t *testing.T) {
	sentinel := errors.New("boom")
	impostor := errors.New("boom")

	if b := typedToEqual(impostor, sentinel); !b.failed {
		t.Error("ExpectT: expected failure for an unrelated error that merely shares the message")
	}
	if b := typedEqualTo(impostor, sentinel); !b.failed {
		t.Error("EqualTo: expected failure for an unrelated error that merely shares the message")
	}
}

// The comparison is oriented exactly like ctx.Expect's: a bare sentinel does not satisfy an
// expectation of an error that wraps it.
func TestTypedEqualityIsOriented(t *testing.T) {
	sentinel := errors.New("boom")
	wrapped := fmt.Errorf("layer: %w", sentinel)

	if b := typedToEqual(sentinel, wrapped); !b.failed {
		t.Error("ExpectT: expected errors.Is(sentinel, wrapped) to be false")
	}
	if b := typedEqualTo(sentinel, wrapped); !b.failed {
		t.Error("EqualTo: expected errors.Is(sentinel, wrapped) to be false")
	}
}

func TestTypedEqualityRejectsANilErrorAgainstASentinel(t *testing.T) {
	sentinel := errors.New("boom")

	if b := typedToEqual(nil, sentinel); !b.failed {
		t.Error("ExpectT: expected a nil error not to equal a sentinel")
	}
	if b := typedEqualTo(nil, sentinel); !b.failed {
		t.Error("EqualTo: expected a nil error not to equal a sentinel")
	}
}

type dslCodeError struct{ Code int }

func (e dslCodeError) Error() string { return fmt.Sprintf("code %d", e.Code) }

// Is makes every dslCodeError with the same Code match, which == on the value cannot see once the
// values sit behind distinct wrappers.
func (e dslCodeError) Is(target error) bool {
	t, ok := target.(dslCodeError)
	return ok && t.Code == e.Code
}

// A concrete error type T takes the same fallback: its values are errors, so an Is method on the
// type is honoured exactly as ctx.Expect honours it.
func TestTypedEqualityHonoursAnIsMethodOnAConcreteErrorType(t *testing.T) {
	backend := &capturingBackend{}
	ExpectT(&Context{backend: backend}, &dslNotFoundError{Name: "a"}).ToEqual(&dslNotFoundError{Name: "a"})
	if !backend.failed {
		t.Error("expected two distinct *dslNotFoundError pointers without an Is method not to match")
	}

	if b := typedToEqual(fmt.Errorf("w: %w", dslCodeError{Code: 7}), dslCodeError{Code: 7}); b.failed {
		t.Errorf("expected errors.Is to honour dslCodeError.Is, got %q", b.message)
	}
}
