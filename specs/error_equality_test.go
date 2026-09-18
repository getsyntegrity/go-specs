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

// EqualTo and ExpectT keep their documented == semantics; this change is scoped to the reflective
// path, and docs/DSL.md records the divergence.
func TestEqualToKeepsPointerIdentitySemanticsForErrors(t *testing.T) {
	sentinel := errors.New("boom")
	wrapped := fmt.Errorf("layer: %w", sentinel)

	backend := &capturingBackend{}
	EqualTo(&Context{backend: backend}, error(wrapped), error(sentinel))
	if !backend.failed {
		t.Error("expected EqualTo to fail: == compares interface identity, not errors.Is")
	}
}
