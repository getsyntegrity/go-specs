package assert

import (
	"errors"
	"testing"
)

// PR #225 review reopened a defect left behind by #209/#224: ctx.Expect(actual).To(m) calls
// m.Match(actual) to decide the result and, only on failure, calls m.FailureMessage(actual)
// separately to build the message. All and Any re-run every sub-matcher inside that second call to
// work out which entries are still responsible, so a failing composite assertion evaluated each
// sub-matcher twice. That is silently wrong for a matcher with a deliberate side effect — and this
// package already ships one: MatchErrorAs calls errors.As(actual, target), which populates target as
// part of matching. Documenting "matchers must be deterministic and side-effect-free" on the Matcher
// interface (the previous attempt at a fix) does not make that true; it only means the shipped
// contract-breaker was ignored.
//
// The real fix is a single-pass evaluation seam: Evaluate calls into a Matcher exactly once per
// assertion, and a composite that implements the optional Evaluator interface evaluates each of its
// own sub-matchers exactly once too, however deeply it nests. Match and FailureMessage stay exactly
// as they are today (third-party code may call either directly), but the DSL no longer drives a
// composite through that doubling pair — it goes through Evaluate.

// asCountingTarget is the MatchErrorAs target type for the regression test below. It must be a
// different concrete type from asCountingSource: errors.As only calls a source's custom As method
// when the target is not already directly assignable from the source's type, and it is that custom
// As call this test counts.
type asCountingTarget struct{ msg string }

func (t *asCountingTarget) Error() string { return t.msg }

// asCountingSource is an error whose As method counts every invocation. A production matcher never
// looks like this on purpose, but MatchErrorAs's whole point is to expose errors.As's own side
// effect (populating target), so this type stands in for any matcher that legitimately has one.
type asCountingSource struct {
	calls *int
}

func (e *asCountingSource) Error() string { return "as-counting source error" }

func (e *asCountingSource) As(target any) bool {
	*e.calls++
	tp, ok := target.(**asCountingTarget)
	if !ok {
		return false
	}
	*tp = &asCountingTarget{msg: "converted"}
	return true
}

// TestEvaluateCallsMatchErrorAsExactlyOnceInsideAll is the headline regression test for #225: inside
// a failing All(MatchErrorAs(&target), Equal("something else")), the As call that MatchErrorAs's
// Match triggers must fire exactly once, not twice.
func TestEvaluateCallsMatchErrorAsExactlyOnceInsideAll(t *testing.T) {
	calls := 0
	src := &asCountingSource{calls: &calls}
	var target *asCountingTarget

	m := All(MatchErrorAs(&target), Equal("something else"))

	matched, failure := Evaluate(m, src)
	if matched {
		t.Fatalf("expected the composite to fail (Equal does not match), got matched=true, failure=%q", failure)
	}
	if calls != 1 {
		t.Fatalf("MatchErrorAs's As method was called %d times through All, want exactly 1", calls)
	}
}

// TestEvaluateCallsMatchErrorAsExactlyOnceInsideAny mirrors the All regression for Any, whose
// FailureMessage also re-runs every sibling's Match on the old doubling path.
func TestEvaluateCallsMatchErrorAsExactlyOnceInsideAny(t *testing.T) {
	calls := 0
	src := &asCountingSource{calls: &calls}
	var target *asCountingTarget

	// A failing Equal alongside MatchErrorAs would let MatchErrorAs's match end Any early, so use two
	// deliberately-failing matchers: MatchErrorAs(&target) against a target it cannot satisfy would
	// still call As once (and fail to assign), so keep target failing not by construction but by
	// pairing with another failing entry and asserting the call count regardless of which one failed.
	m := Any(MatchErrorAs(&target), Equal("something else"))

	_, _ = Evaluate(m, src)
	if calls != 1 {
		t.Fatalf("MatchErrorAs's As method was called %d times through Any, want exactly 1", calls)
	}
}

// callCountingMatcher counts Match and FailureMessage calls separately, so a test can pin exactly
// how many times each was invoked through Evaluate — the property the old Match+FailureMessage
// doubling could not guarantee.
type callCountingMatcher struct {
	matches      bool
	matchCalls   int
	failMsgCalls int
}

func (c *callCountingMatcher) Match(any) bool {
	c.matchCalls++
	return c.matches
}

func (c *callCountingMatcher) FailureMessage(any) string {
	c.failMsgCalls++
	return "call-counting matcher failed"
}

// TestEvaluateOnOrdinaryMatcherCallsMatchOnceAndFailureMessageOnlyOnFailure pins requirement 5: a
// matcher that does not implement Evaluator is still evaluated exactly once by Evaluate, with
// FailureMessage built only when Match reported false.
func TestEvaluateOnOrdinaryMatcherCallsMatchOnceAndFailureMessageOnlyOnFailure(t *testing.T) {
	t.Run("passing", func(t *testing.T) {
		m := &callCountingMatcher{matches: true}
		matched, failure := Evaluate(m, "x")
		if !matched || failure != "" {
			t.Fatalf("got matched=%v failure=%q, want matched=true failure=\"\"", matched, failure)
		}
		if m.matchCalls != 1 {
			t.Errorf("Match called %d times, want 1", m.matchCalls)
		}
		if m.failMsgCalls != 0 {
			t.Errorf("FailureMessage called %d times, want 0", m.failMsgCalls)
		}
	})

	t.Run("failing", func(t *testing.T) {
		m := &callCountingMatcher{matches: false}
		matched, failure := Evaluate(m, "x")
		if matched || failure != "call-counting matcher failed" {
			t.Fatalf("got matched=%v failure=%q, want matched=false failure=%q", matched, failure, "call-counting matcher failed")
		}
		if m.matchCalls != 1 {
			t.Errorf("Match called %d times, want 1", m.matchCalls)
		}
		if m.failMsgCalls != 1 {
			t.Errorf("FailureMessage called %d times, want 1", m.failMsgCalls)
		}
	})
}

// TestAllEvaluateCallsEachChildExactlyOnce pins requirement 2 for All: every entry is asked for its
// verdict exactly once, whether the composite as a whole passes or fails.
func TestAllEvaluateCallsEachChildExactlyOnce(t *testing.T) {
	t.Run("passing", func(t *testing.T) {
		a := &callCountingMatcher{matches: true}
		b := &callCountingMatcher{matches: true}

		matched, failure := Evaluate(All(a, b), "x")
		if !matched || failure != "" {
			t.Fatalf("got matched=%v failure=%q, want matched=true failure=\"\"", matched, failure)
		}
		for name, m := range map[string]*callCountingMatcher{"a": a, "b": b} {
			if m.matchCalls != 1 {
				t.Errorf("%s.Match called %d times, want 1", name, m.matchCalls)
			}
			if m.failMsgCalls != 0 {
				t.Errorf("%s.FailureMessage called %d times, want 0", name, m.failMsgCalls)
			}
		}
	})

	t.Run("failing", func(t *testing.T) {
		a := &callCountingMatcher{matches: true}
		b := &callCountingMatcher{matches: false}

		matched, _ := Evaluate(All(a, b), "x")
		if matched {
			t.Fatal("expected the composite to fail")
		}
		if a.matchCalls != 1 || a.failMsgCalls != 0 {
			t.Errorf("a: matchCalls=%d failMsgCalls=%d, want 1/0", a.matchCalls, a.failMsgCalls)
		}
		if b.matchCalls != 1 || b.failMsgCalls != 1 {
			t.Errorf("b: matchCalls=%d failMsgCalls=%d, want 1/1", b.matchCalls, b.failMsgCalls)
		}
	})
}

// TestAnyEvaluateCallsEachChildExactlyOnce is the Any counterpart of the All test above.
func TestAnyEvaluateCallsEachChildExactlyOnce(t *testing.T) {
	t.Run("passing (first entry matches, second is never reached)", func(t *testing.T) {
		a := &callCountingMatcher{matches: true}
		b := &callCountingMatcher{matches: true}

		matched, failure := Evaluate(Any(a, b), "x")
		if !matched || failure != "" {
			t.Fatalf("got matched=%v failure=%q, want matched=true failure=\"\"", matched, failure)
		}
		if a.matchCalls != 1 {
			t.Errorf("a.Match called %d times, want 1", a.matchCalls)
		}
		// b is never consulted: Any returns as soon as the first entry matches.
		if b.matchCalls != 0 {
			t.Errorf("b.Match called %d times, want 0 (Any short-circuits on the first match)", b.matchCalls)
		}
	})

	t.Run("failing", func(t *testing.T) {
		a := &callCountingMatcher{matches: false}
		b := &callCountingMatcher{matches: false}

		matched, _ := Evaluate(Any(a, b), "x")
		if matched {
			t.Fatal("expected the composite to fail")
		}
		if a.matchCalls != 1 || a.failMsgCalls != 1 {
			t.Errorf("a: matchCalls=%d failMsgCalls=%d, want 1/1", a.matchCalls, a.failMsgCalls)
		}
		if b.matchCalls != 1 || b.failMsgCalls != 1 {
			t.Errorf("b: matchCalls=%d failMsgCalls=%d, want 1/1", b.matchCalls, b.failMsgCalls)
		}
	})
}

// TestNotEvaluateDoesNotBuildFailureMessageWhenSubFails pins requirement 3: when the sub-matcher
// fails (so Not succeeds), Not must not call the sub's FailureMessage at all — there is nothing
// wrong to describe, and calling it anyway could trigger a side effect for no reason.
func TestNotEvaluateDoesNotBuildFailureMessageWhenSubFails(t *testing.T) {
	sub := &callCountingMatcher{matches: false}

	matched, failure := Evaluate(Not(sub), "x")
	if !matched || failure != "" {
		t.Fatalf("got matched=%v failure=%q, want matched=true failure=\"\"", matched, failure)
	}
	if sub.matchCalls != 1 {
		t.Errorf("sub.Match called %d times, want 1", sub.matchCalls)
	}
	if sub.failMsgCalls != 0 {
		t.Errorf("sub.FailureMessage called %d times, want 0", sub.failMsgCalls)
	}
}

// TestNotEvaluateDoesNotCallSubFailureMessageWhenSubMatches covers the other branch: when the
// sub-matcher matches (so Not fails), Not still must not call the sub's FailureMessage — it would
// describe a comparison that succeeded. Not names the sub through Description() instead.
func TestNotEvaluateDoesNotCallSubFailureMessageWhenSubMatches(t *testing.T) {
	sub := &callCountingMatcher{matches: true}

	matched, failure := Evaluate(Not(sub), "x")
	if matched {
		t.Fatal("expected Not to fail when its sub-matcher matches")
	}
	want := "expected x not to be *assert.callCountingMatcher"
	if failure != want {
		t.Fatalf("got failure %q, want %q", failure, want)
	}
	if sub.matchCalls != 1 {
		t.Errorf("sub.Match called %d times, want 1", sub.matchCalls)
	}
	if sub.failMsgCalls != 0 {
		t.Errorf("sub.FailureMessage called %d times, want 0", sub.failMsgCalls)
	}
}

// TestEvaluateNestedCompositesEvaluateEachLeafExactlyOnce pins requirement 4: nesting Any and Not
// inside All must still reach every visited leaf exactly once, recursively, through the
// package-level Evaluate seam.
func TestEvaluateNestedCompositesEvaluateEachLeafExactlyOnce(t *testing.T) {
	anyFirst := &callCountingMatcher{matches: false} // visited, fails
	anySecond := &callCountingMatcher{matches: true} // visited, matches -> Any stops here
	notSub := &callCountingMatcher{matches: false}   // visited via Not, fails -> Not matches

	m := All(Any(anyFirst, anySecond), Not(notSub))

	matched, failure := Evaluate(m, "x")
	if !matched || failure != "" {
		t.Fatalf("got matched=%v failure=%q, want matched=true failure=\"\"", matched, failure)
	}

	if anyFirst.matchCalls != 1 || anyFirst.failMsgCalls != 1 {
		t.Errorf("anyFirst: matchCalls=%d failMsgCalls=%d, want 1/1", anyFirst.matchCalls, anyFirst.failMsgCalls)
	}
	if anySecond.matchCalls != 1 || anySecond.failMsgCalls != 0 {
		t.Errorf("anySecond: matchCalls=%d failMsgCalls=%d, want 1/0", anySecond.matchCalls, anySecond.failMsgCalls)
	}
	if notSub.matchCalls != 1 || notSub.failMsgCalls != 0 {
		t.Errorf("notSub: matchCalls=%d failMsgCalls=%d, want 1/0", notSub.matchCalls, notSub.failMsgCalls)
	}
}

// TestEvaluateHandlesTypedNil pins requirement 6 for a bare matcher and for each of the three
// combinators, reusing derefMatcher from composite_matchers_test.go — a non-nil Matcher interface
// wrapping a nil pointer, which a plain `== nil` check cannot see.
func TestEvaluateHandlesTypedNil(t *testing.T) {
	var typedNil *derefMatcher

	t.Run("bare matcher", func(t *testing.T) {
		matched, failure := Evaluate(typedNil, "x")
		if matched {
			t.Fatal("expected a typed-nil matcher never to match")
		}
		if failure != "nil matcher (never matches)" {
			t.Fatalf("got failure %q", failure)
		}
	})

	t.Run("Not", func(t *testing.T) {
		matched, failure := Evaluate(Not(typedNil), "x")
		if matched {
			t.Fatal("expected Not(typed nil) never to match")
		}
		if want := "Not: sub-matcher at position 1 is nil"; failure != want {
			t.Fatalf("got failure %q, want %q", failure, want)
		}
	})

	t.Run("All", func(t *testing.T) {
		matched, failure := Evaluate(All(typedNil), "x")
		if matched {
			t.Fatal("expected All(typed nil) never to match")
		}
		if want := "All: #1: nil matcher (never matches)"; failure != want {
			t.Fatalf("got failure %q, want %q", failure, want)
		}
	})

	t.Run("Any", func(t *testing.T) {
		matched, failure := Evaluate(Any(typedNil), "x")
		if matched {
			t.Fatal("expected Any(typed nil) never to match")
		}
		if want := "Any: #1: nil matcher (never matches)"; failure != want {
			t.Fatalf("got failure %q, want %q", failure, want)
		}
	})
}

// TestEvaluateHandlesUntypedNil covers the plain, unassigned-interface nil case alongside the typed
// one above -- Evaluate itself must treat both alike, even though the DSL's own m == nil guard in
// specs/context.go intercepts the untyped case earlier and never reaches Evaluate with it.
func TestEvaluateHandlesUntypedNil(t *testing.T) {
	matched, failure := Evaluate(nil, "x")
	if matched {
		t.Fatal("expected a nil matcher never to match")
	}
	if failure != "nil matcher (never matches)" {
		t.Fatalf("got failure %q", failure)
	}
}

// TestEvaluateMessagesMatchFailureMessageByteForByte pins requirement 7: for every case below, the
// message Evaluate returns must be byte-identical to what the existing, unchanged Match/FailureMessage
// pair already produces for the same matcher and actual -- Evaluate is a new seam, not a new wording.
func TestEvaluateMessagesMatchFailureMessageByteForByte(t *testing.T) {
	sentinel := errors.New("sentinel")
	cases := []struct {
		name   string
		m      Matcher
		actual any
	}{
		{"All: two failing entries", All(Equal(1), BeTrue()), 2},
		{"All: one failing entry", All(Equal(2), BeTrue()), 2},
		{"All: nil entry", All(Equal(1), nil), 1},
		{"All: empty", All(), "anything"},
		{"Any: two failing entries", Any(Equal(1), Equal(2)), 3},
		{"Any: nil entry with a describable sibling", Any(Equal(1), nil), 1},
		{"Any: empty", Any(), "anything"},
		{"Not: sub matches, has Describer", Not(Equal(43)), 43},
		{"Not: sub matches, no Describer", Not(fakeMatcher{matches: true, failMsg: "irrelevant"}), 5},
		{"Not: nil sub", Not(nil), "anything"},
		{"Nested: All(Not(...), ...)", All(Not(Equal(1)), BeTrue()), 1},
		{"MatchError", MatchError(sentinel), errors.New("unrelated")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wantMatched := tc.m.Match(tc.actual)
			wantFailure := ""
			if !wantMatched {
				wantFailure = tc.m.FailureMessage(tc.actual)
			}

			gotMatched, gotFailure := Evaluate(tc.m, tc.actual)

			if gotMatched != wantMatched {
				t.Fatalf("matched: got %v, want %v", gotMatched, wantMatched)
			}
			if gotFailure != wantFailure {
				t.Fatalf("failure: got %q, want %q", gotFailure, wantFailure)
			}
		})
	}
}
