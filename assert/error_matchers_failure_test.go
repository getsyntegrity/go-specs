package assert

import (
	"errors"
	"fmt"
	"testing"
)

// error_matchers_failure_test.go pins one FailureMessage per diagnostic branch of the error
// matchers (issue #200). The failure text is part of the matcher contract: MatchErrorAs in
// particular inherits errors.As's non-obvious target-validity rules, so the message is the
// documentation a user reads when they get it wrong. Each case asserts the exact wording, not
// merely that the matcher failed.

type failureMessageCase struct {
	name   string
	target any
	actual any
	want   string
}

func runFailureMessageCases(t *testing.T, build func(target any) Matcher, cases []failureMessageCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("expected a failure report rather than a panic, got panic: %v", r)
				}
			}()
			m := build(tc.target)
			if m.Match(tc.actual) {
				t.Fatalf("expected the matcher not to match %v (%T)", tc.actual, tc.actual)
			}
			if got := m.FailureMessage(tc.actual); got != tc.want {
				t.Fatalf("FailureMessage:\n got  %q\n want %q", got, tc.want)
			}
		})
	}
}

func TestMatchErrorAsFailureMessageCoversEveryBranch(t *testing.T) {
	var target *notFoundError
	var nilErr error
	notAnError := 42
	boom := errors.New("boom")

	runFailureMessageCases(t, func(target any) Matcher { return MatchErrorAs(target) }, []failureMessageCase{
		// Branch 1: invalid errors.As target. errors.As would panic on each of these.
		{
			name:   "invalid target: nil",
			target: nil,
			actual: boom,
			want:   "MatchErrorAs needs a non-nil pointer to a type implementing error (or to an interface), got <nil> (<nil>)",
		},
		{
			name:   "invalid target: not a pointer",
			target: notFoundError{},
			actual: boom,
			want:   "MatchErrorAs needs a non-nil pointer to a type implementing error (or to an interface), got {} (assert.notFoundError)",
		},
		{
			name:   "invalid target: nil pointer",
			target: (**notFoundError)(nil),
			actual: boom,
			want:   "MatchErrorAs needs a non-nil pointer to a type implementing error (or to an interface), got <nil> (**assert.notFoundError)",
		},
		{
			name:   "invalid target: pointer to a non-error type",
			target: &notAnError,
			actual: boom,
			want:   fmt.Sprintf("MatchErrorAs needs a non-nil pointer to a type implementing error (or to an interface), got %v (*int)", &notAnError),
		},
		// Branch 2: actual is not an error.
		{
			name:   "actual is not an error",
			target: &target,
			actual: "boom",
			want:   "expected an error assignable to **assert.notFoundError, got boom (string) — errors.As needs an error actual",
		},
		// Branch 3: actual is a nil error — the common case of a function that returned no error.
		{
			name:   "actual is an untyped nil",
			target: &target,
			actual: nil,
			want:   "expected an error assignable to **assert.notFoundError, got a nil error",
		},
		{
			name:   "actual is a nil error interface",
			target: &target,
			actual: nilErr,
			want:   "expected an error assignable to **assert.notFoundError, got a nil error",
		},
		// Branch 4: valid target, non-matching error.
		{
			name:   "error of an unrelated type",
			target: &target,
			actual: fmt.Errorf("layer: %w", boom),
			want:   "expected layer: boom (*fmt.wrapError) to unwrap to **assert.notFoundError — errors.As(actual, target) is false",
		},
	})
}

func TestMatchErrorFailureMessageCoversEveryBranch(t *testing.T) {
	sentinel := errors.New("boom")
	var nilErr error

	runFailureMessageCases(t, func(target any) Matcher { return MatchError(target.(error)) }, []failureMessageCase{
		{
			name:   "actual is not an error",
			target: sentinel,
			actual: "boom",
			want:   "expected an error matching boom (*errors.errorString), got boom (string) — errors.Is needs an error actual",
		},
		{
			name:   "actual is an untyped nil",
			target: sentinel,
			actual: nil,
			want:   "expected an error matching boom (*errors.errorString), got a nil error — errors.Is(actual, expected) is false",
		},
		{
			name:   "actual is a nil error interface",
			target: sentinel,
			actual: nilErr,
			want:   "expected an error matching boom (*errors.errorString), got a nil error — errors.Is(actual, expected) is false",
		},
		{
			name:   "error without the expected identity",
			target: sentinel,
			actual: errors.New("boom"),
			want:   "expected error boom (*errors.errorString) to match boom (*errors.errorString) — errors.Is(actual, expected) is false",
		},
	})
}
