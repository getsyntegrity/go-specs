package assert

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

// error_equality_test.go pins the oriented error semantics of the equality matchers (issue #183).
//
// Before this, Equal compared errors structurally through reflect.DeepEqual, so two unrelated
// errors carrying the same message compared as equal — a silent false positive — while a wrapped
// error failed against its own sentinel. The orientation is deliberate and is the contract these
// tests exist to lock: for Equal(expected).Match(actual) the question asked is
// errors.Is(actual, expected), never the reverse and never both directions. An actual that wraps
// the expected sentinel satisfies it; a bare sentinel does not satisfy an expectation of some
// wrapped error that happens to contain it.

type notFoundError struct{ Name string }

func (e *notFoundError) Error() string { return "not found: " + e.Name }

func TestEqualRejectsDistinctErrorsSharingAMessage(t *testing.T) {
	sentinel := errors.New("boom")
	impostor := errors.New("boom")

	if Equal(sentinel).Match(impostor) {
		t.Fatal("expected Equal to reject an unrelated error that merely shares the message 'boom'")
	}
	if Equal(io.EOF).Match(errors.New("EOF")) {
		t.Fatal("expected Equal(io.EOF) to reject an unrelated error whose message reads 'EOF'")
	}
}

func TestEqualAcceptsAnActualThatWrapsTheExpectedSentinel(t *testing.T) {
	sentinel := errors.New("boom")
	wrapped := fmt.Errorf("layer: %w", sentinel)

	if !Equal(sentinel).Match(wrapped) {
		t.Fatal("expected Equal(sentinel) to accept an actual error wrapping it — errors.Is(actual, expected) is true")
	}
	if !Equal(sentinel).Match(errors.Join(sentinel)) {
		t.Fatal("expected Equal(sentinel) to accept errors.Join(sentinel)")
	}
}

// The orientation is not symmetric: expecting a wrapped error is a stricter claim than expecting
// the sentinel it wraps, so a bare sentinel must not satisfy it.
func TestEqualRejectsABareSentinelAgainstAWrappedExpectation(t *testing.T) {
	sentinel := errors.New("boom")
	wrapped := fmt.Errorf("layer: %w", sentinel)

	if Equal(wrapped).Match(sentinel) {
		t.Fatal("expected Equal(wrapped) to reject the bare sentinel — errors.Is(sentinel, wrapped) is false")
	}
}

func TestEqualAcceptsAnErrorAgainstItself(t *testing.T) {
	sentinel := errors.New("boom")
	if !Equal(sentinel).Match(sentinel) {
		t.Fatal("expected Equal to accept an error compared against itself")
	}
}

// A nil actual carries no error identity, so it satisfies no error expectation.
func TestEqualRejectsNilAgainstAnErrorExpectation(t *testing.T) {
	sentinel := errors.New("boom")
	if Equal(sentinel).Match(nil) {
		t.Fatal("expected Equal(sentinel) to reject a nil actual")
	}
	if Equal(nil).Match(sentinel) {
		t.Fatal("expected Equal(nil) to reject a non-nil error actual")
	}
}

// NotEqual is the exact negation of Equal over the same oriented semantics — it must not fall back
// to structural comparison, or it would silently pass for an error compared against itself.
func TestNotEqualIsTheExactInverseOfEqualForErrors(t *testing.T) {
	sentinel := errors.New("boom")
	impostor := errors.New("boom")
	wrapped := fmt.Errorf("layer: %w", sentinel)

	cases := []struct {
		name     string
		expected error
		actual   error
	}{
		{"unrelated errors sharing a message", sentinel, impostor},
		{"actual wraps expected", sentinel, wrapped},
		{"expected wraps actual", wrapped, sentinel},
		{"same error", sentinel, sentinel},
		{"io.EOF against a lookalike", io.EOF, errors.New("EOF")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			equal := Equal(tc.expected).Match(tc.actual)
			notEqual := NotEqual(tc.expected).Match(tc.actual)
			if equal == notEqual {
				t.Fatalf("NotEqual must be the exact inverse of Equal: Equal=%v NotEqual=%v", equal, notEqual)
			}
		})
	}
}

// Contain passes each element as the actual and the matcher's own value as the expected, so the
// same orientation has to hold element -> expected.
func TestContainUsesTheSameOrientationForErrorElements(t *testing.T) {
	sentinel := errors.New("boom")
	impostor := errors.New("boom")
	wrapped := fmt.Errorf("layer: %w", sentinel)

	if !Contain(sentinel).Match([]error{io.EOF, wrapped}) {
		t.Fatal("expected Contain(sentinel) to find an element wrapping it")
	}
	if Contain(sentinel).Match([]error{impostor}) {
		t.Fatal("expected Contain(sentinel) to reject an unrelated element that merely shares the message")
	}
}

func TestMatchErrorUsesErrorsIs(t *testing.T) {
	sentinel := errors.New("boom")
	impostor := errors.New("boom")
	wrapped := fmt.Errorf("layer: %w", sentinel)

	if !MatchError(sentinel).Match(wrapped) {
		t.Fatal("expected MatchError(sentinel) to accept a wrapping actual")
	}
	if MatchError(sentinel).Match(impostor) {
		t.Fatal("expected MatchError(sentinel) to reject an unrelated error sharing the message")
	}
	if MatchError(sentinel).Match(nil) {
		t.Fatal("expected MatchError(sentinel) to reject a nil actual")
	}
	if MatchError(sentinel).Match("boom") {
		t.Fatal("expected MatchError(sentinel) to reject a non-error actual")
	}
}

func TestMatchErrorAsUsesErrorsAs(t *testing.T) {
	wrapped := fmt.Errorf("layer: %w", &notFoundError{Name: "user"})

	var target *notFoundError
	if !MatchErrorAs(&target).Match(wrapped) {
		t.Fatal("expected MatchErrorAs to unwrap to *notFoundError")
	}
	if target == nil || target.Name != "user" {
		t.Fatalf("expected MatchErrorAs to populate the target, got %+v", target)
	}

	var other *notFoundError
	if MatchErrorAs(&other).Match(errors.New("boom")) {
		t.Fatal("expected MatchErrorAs to reject an error of an unrelated type")
	}
}

// errors.As panics on an invalid target; the matcher must report a failure instead of taking the
// suite down, because a matcher's job is to tell the truth about the assertion, not to crash.
func TestMatchErrorAsRejectsAnInvalidTargetWithoutPanicking(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("expected MatchErrorAs to report a failure rather than panic, got panic: %v", r)
		}
	}()

	m := MatchErrorAs(nil)
	if m.Match(errors.New("boom")) {
		t.Fatal("expected MatchErrorAs(nil) not to match")
	}
	if msg := m.FailureMessage(errors.New("boom")); msg == "" {
		t.Fatal("expected a non-empty failure message for an invalid target")
	}
}

// The reason this defect stayed invisible: expected and actual rendered identically under %v, so
// the failure read "expected boom to equal boom". The message has to distinguish them.
func TestFailureMessageDistinguishesErrorsThatRenderIdentically(t *testing.T) {
	sentinel := errors.New("boom")
	impostor := errors.New("boom")
	joined := errors.Join(sentinel)

	for _, tc := range []struct {
		name    string
		message string
	}{
		{"Equal against an impostor", Equal(sentinel).FailureMessage(impostor)},
		{"Equal against a join", Equal(impostor).FailureMessage(joined)},
		{"MatchError against an impostor", MatchError(sentinel).FailureMessage(impostor)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if strings.Contains(tc.message, "expected boom to equal boom") {
				t.Fatalf("failure message does not distinguish the two errors: %q", tc.message)
			}
			if !strings.Contains(tc.message, "errors.Is") {
				t.Fatalf("expected the message to name the semantics being applied, got %q", tc.message)
			}
			if !strings.Contains(tc.message, "errors.errorString") && !strings.Contains(tc.message, "errors.joinError") {
				t.Fatalf("expected the message to carry concrete types, got %q", tc.message)
			}
		})
	}
}

// Non-error values keep their existing structural semantics; this fix is scoped to errors.
func TestNonErrorEqualityIsUnchanged(t *testing.T) {
	type sample struct {
		ID   int
		Data map[string]int
	}
	left := sample{ID: 1, Data: map[string]int{"a": 1}}
	right := sample{ID: 1, Data: map[string]int{"a": 1}}

	if !Equal(left).Match(right) {
		t.Fatal("expected struct equality to keep using reflect.DeepEqual")
	}
	if !Equal([]byte("abc")).Match([]byte("abc")) {
		t.Fatal("expected byte slice equality to keep using reflect.DeepEqual")
	}
	if Equal(1).Match(int64(1)) {
		t.Fatal("expected int and int64 to stay unequal")
	}
}
