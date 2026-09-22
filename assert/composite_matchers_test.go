package assert

import (
	"errors"
	"testing"
)

// Not/All/Any exist because #149/#200 taught this repo that matcher messages are the product: a
// composite that reports "combined matcher failed" throws away the only thing the user needs. These
// tests pin the Match truth tables, every message branch (which sub-matcher named, by which index,
// with which wording), and the nil/empty table from odd/tasks/matcher-composition.md. None of the
// nil/empty cases may panic — a testing framework reports, it does not take the suite down.

// fakeMatcher is a hand-rolled Matcher (not one of the built-ins) used to prove two things composite
// code must not conflate: (1) a matcher that does not implement Describer falls back to %T rather
// than panicking or printing something misleading, and (2) Not must read failMsg only when its
// sub-matcher did NOT match — reading it on a match would print a message describing a comparison
// that succeeded, which is exactly the bug Not exists to avoid.
type fakeMatcher struct {
	matches bool
	failMsg string
}

func (f fakeMatcher) Match(any) bool            { return f.matches }
func (f fakeMatcher) FailureMessage(any) string { return f.failMsg }

// describedMatcher is a fakeMatcher that also implements Describer, so tests can tell whether a
// composite used Description() or fell through to FailureMessage()/​%T.
type describedMatcher struct {
	fakeMatcher
	desc string
}

func (d describedMatcher) Description() string { return d.desc }

// derefMatcher has pointer-receiver methods that dereference the receiver. A nil *derefMatcher
// really panics on Match/FailureMessage if a composite's nil guard misses it — that is what proves
// the guard catches a *typed* nil (a declared-but-unassigned *derefMatcher passed as a Matcher, a
// non-nil interface value wrapping a nil pointer), not just an untyped nil interface.
type derefMatcher struct{ ok bool }

func (d *derefMatcher) Match(any) bool            { return d.ok }
func (d *derefMatcher) FailureMessage(any) string { return "derefMatcher failed" }

// flippingMatcher answers false the first time Match is called and true on every call after. It
// stands in for a sub-matcher whose second evaluation (a composite's FailureMessage re-running it
// to build a message) disagrees with its first (the Match call that decided the composite failed) —
// the exact non-determinism All/Any's FailureMessage must name rather than paper over.
type flippingMatcher struct {
	calls int
}

func (f *flippingMatcher) Match(any) bool {
	f.calls++
	return f.calls > 1
}
func (f *flippingMatcher) FailureMessage(any) string { return "flippingMatcher failed" }

// countingMatcher counts its Match calls and describes itself, so a test can prove Any's
// FailureMessage does not evaluate a sibling when a nil entry already dooms the composite.
type countingMatcher struct {
	calls int
}

func (c *countingMatcher) Match(any) bool            { c.calls++; return true }
func (c *countingMatcher) FailureMessage(any) string { return "countingMatcher failed" }
func (c *countingMatcher) Description() string       { return "counting" }

// --- Not -------------------------------------------------------------------------------------

func TestNotMatchTruthTable(t *testing.T) {
	cases := map[string]struct {
		sub  Matcher
		want bool
	}{
		"sub matches -> Not fails":     {fakeMatcher{matches: true}, false},
		"sub does not match -> Not ok": {fakeMatcher{matches: false}, true},
		"nil sub -> Not never matches": {nil, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := Not(tc.sub).Match("actual"); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// This is the test that would catch a Not implementation that calls sub.FailureMessage on a
// matching sub-matcher: failMsg is deliberately a string that must never appear in the output.
func TestNotFailureMessageUsesDescriptionNotSubFailureMessage(t *testing.T) {
	sub := describedMatcher{
		fakeMatcher: fakeMatcher{matches: true, failMsg: "THIS MUST NEVER APPEAR"},
		desc:        "the described thing",
	}
	got := Not(sub).FailureMessage(5)
	want := "expected 5 not to be the described thing"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestNotFailureMessageFallsBackToTypeNameWithoutDescriber(t *testing.T) {
	sub := fakeMatcher{matches: true, failMsg: "irrelevant"}
	got := Not(sub).FailureMessage(5)
	want := "expected 5 not to be assert.fakeMatcher"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestNotFailureMessageWithNilSubNamesItsPosition(t *testing.T) {
	got := Not(nil).FailureMessage("anything")
	want := "Not: sub-matcher at position 1 is nil"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestNotDescription(t *testing.T) {
	got := Not(Equal(43)).(Describer).Description()
	want := "not equal to 43"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestNotDescriptionWithNilSub(t *testing.T) {
	got := Not(nil).(Describer).Description()
	want := "not (nil matcher)"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// A typed nil — a declared-but-unassigned *derefMatcher passed as a Matcher — must be treated
// exactly like an untyped nil: same Match result, same FailureMessage and Description wording, no
// panic. derefMatcher's methods dereference the receiver, so a guard that only checks `== nil`
// (which is false for a non-nil interface wrapping a nil pointer) would panic here.
func TestNotHandlesTypedNilWithoutPanicking(t *testing.T) {
	var typedNil *derefMatcher
	m := Not(typedNil)

	if got := m.Match("actual"); got != false {
		t.Fatalf("Match: got %v, want false", got)
	}
	if got, want := m.FailureMessage("actual"), "Not: sub-matcher at position 1 is nil"; got != want {
		t.Fatalf("FailureMessage: got %q, want %q", got, want)
	}
	if got, want := m.(Describer).Description(), "not (nil matcher)"; got != want {
		t.Fatalf("Description: got %q, want %q", got, want)
	}
}

// --- All ---------------------------------------------------------------------------------------

func TestAllMatchTruthTable(t *testing.T) {
	cases := map[string]struct {
		ms   []Matcher
		want bool
	}{
		"no matchers is the AND identity": {nil, true},
		"all match":                       {[]Matcher{fakeMatcher{matches: true}, fakeMatcher{matches: true}}, true},
		"one fails":                       {[]Matcher{fakeMatcher{matches: true}, fakeMatcher{matches: false}}, false},
		"nil entry never matches":         {[]Matcher{fakeMatcher{matches: true}, nil}, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := All(tc.ms...).Match("actual"); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAllFailureMessageListsEveryFailingSubMatcherByIndexAndQuotesItsMessage(t *testing.T) {
	got := All(Equal(1), BeTrue()).FailureMessage(2)
	want := `All: #1: "expected 2 to equal 1"; #2: "expected true, got 2 (int)"`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestAllFailureMessageOmitsSubMatchersThatMatched(t *testing.T) {
	got := All(Equal(2), BeTrue()).FailureMessage(2)
	want := `All: #2: "expected true, got 2 (int)"`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestAllFailureMessageNamesNilEntryPosition(t *testing.T) {
	got := All(Equal(1), nil).FailureMessage(1)
	want := "All: #2: nil matcher (never matches)"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// All() with no matchers matches everything, so FailureMessage is never invoked through ordinary
// use. It still must not panic if called directly — a testing framework reports, it does not crash.
func TestAllEmptyFailureMessageDoesNotPanic(t *testing.T) {
	got := All().FailureMessage("anything")
	want := "All: no sub-matcher explains the failure"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestAllDescriptionNestsSubDescriptions(t *testing.T) {
	got := All(Equal(true), BeTrue()).(Describer).Description()
	want := "all of [equal to true, true]"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestAllDescriptionWithNilEntry(t *testing.T) {
	got := All(Equal(1), nil).(Describer).Description()
	want := "all of [equal to 1, <nil>]"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestAllEmptyDescription(t *testing.T) {
	got := All().(Describer).Description()
	want := "all of []"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// A typed nil entry — a declared-but-unassigned *derefMatcher passed as a Matcher — must fail the
// composite exactly like an untyped nil entry, without panicking: derefMatcher's methods dereference
// the receiver, so an `== nil` guard alone (false for a non-nil interface wrapping a nil pointer)
// would panic here.
func TestAllHandlesTypedNilEntryWithoutPanicking(t *testing.T) {
	var typedNil *derefMatcher
	m := All(Equal(1), typedNil)

	if got := m.Match(1); got != false {
		t.Fatalf("Match: got %v, want false", got)
	}
	if got, want := m.FailureMessage(1), "All: #2: nil matcher (never matches)"; got != want {
		t.Fatalf("FailureMessage: got %q, want %q", got, want)
	}
}

// FailureMessage re-runs every non-nil sub-matcher's Match to decide which are still responsible for
// the failure (see the Matcher doc comment in matcher.go). Reaching this point means Match reported
// false but the second pass says every entry now matches — a sub-matcher answered differently on its
// second call — so the message must name that instead of the old, misleading
// "All: no sub-matcher explains the failure" placeholder.
func TestAllFailureMessageNamesNonDeterministicSubMatcherInsteadOfThePlaceholder(t *testing.T) {
	sub := &flippingMatcher{}
	m := All(sub)

	if got := m.Match("x"); got != false {
		t.Fatalf("Match: got %v, want false", got)
	}
	got := m.FailureMessage("x")
	want := "All: Match reported a failure but every sub-matcher now reports a match — a sub-matcher is not deterministic"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// --- Any ---------------------------------------------------------------------------------------

func TestAnyMatchTruthTable(t *testing.T) {
	cases := map[string]struct {
		ms   []Matcher
		want bool
	}{
		"no matchers is the OR identity": {nil, false},
		"one matches":                    {[]Matcher{fakeMatcher{matches: false}, fakeMatcher{matches: true}}, true},
		"all fail":                       {[]Matcher{fakeMatcher{matches: false}, fakeMatcher{matches: false}}, false},
		"nil entry forces false despite a sibling match": {[]Matcher{fakeMatcher{matches: true}, nil}, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := Any(tc.ms...).Match("actual"); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAnyFailureMessageListsEveryFailingSubMatcherByIndexAndQuotesItsMessage(t *testing.T) {
	got := Any(Equal(1), Equal(2)).FailureMessage(3)
	want := `Any: #1: "expected 3 to equal 1"; #2: "expected 3 to equal 2"`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestAnyFailureMessageNamesNilEntryPosition(t *testing.T) {
	got := Any(nil).FailureMessage("anything")
	want := "Any: #1: nil matcher (never matches)"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// A non-nil sibling must not be evaluated at all once a nil entry has already doomed the composite
// (that would re-run Match for zero diagnostic benefit and risk a matcher with a side effect — see
// TestAnyFailureMessageDoesNotEvaluateSiblingsWhenANilEntryForcesFailure), and it must not be
// silently dropped either (that would hide why the nil forced this Any to fail). It gets its own
// branch naming both facts: what it is, and that it was not evaluated.
func TestAnyFailureMessageReportsSuppressedMatchWhenNilForcesFailure(t *testing.T) {
	got := Any(Equal(1), nil).FailureMessage(1)
	want := "Any: #1: equal to 1 — not evaluated, the nil entry at #2 forces this Any to fail; #2: nil matcher (never matches)"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestAnyEmptyFailureMessage(t *testing.T) {
	got := Any().FailureMessage("anything")
	want := "Any: no matchers were given, so nothing could match"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// A typed nil entry — a declared-but-unassigned *derefMatcher passed as a Matcher — must fail the
// composite exactly like an untyped nil entry, without panicking: derefMatcher's methods dereference
// the receiver, so an `== nil` guard alone (false for a non-nil interface wrapping a nil pointer)
// would panic here.
func TestAnyHandlesTypedNilEntryWithoutPanicking(t *testing.T) {
	var typedNil *derefMatcher
	m := Any(typedNil)

	if got := m.Match("anything"); got != false {
		t.Fatalf("Match: got %v, want false", got)
	}
	if got, want := m.FailureMessage("anything"), "Any: #1: nil matcher (never matches)"; got != want {
		t.Fatalf("FailureMessage: got %q, want %q", got, want)
	}
}

// Once a nil entry has already doomed the composite, FailureMessage must not evaluate a non-nil
// sibling at all: Match never reached it either (the nil scan short-circuits before the match loop),
// so evaluating it only in FailureMessage would ask a question Match itself never asked, for a
// composite that was already going to fail regardless of the answer.
func TestAnyFailureMessageDoesNotEvaluateSiblingsWhenANilEntryForcesFailure(t *testing.T) {
	sibling := &countingMatcher{}
	m := Any(sibling, nil)

	if got := m.Match("x"); got != false {
		t.Fatalf("Match: got %v, want false", got)
	}
	before := sibling.calls

	got := m.FailureMessage("x")
	want := "Any: #1: counting — not evaluated, the nil entry at #2 forces this Any to fail; #2: nil matcher (never matches)"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if sibling.calls != before {
		t.Fatalf("sibling.Match was called during FailureMessage: before=%d, after=%d", before, sibling.calls)
	}
}

// FailureMessage re-runs every entry's Match to decide which are still responsible for the failure
// (see the Matcher doc comment in matcher.go). Reaching this point means Match reported false but
// the second pass says every entry now matches — a sub-matcher answered differently on its second
// call — so the message must name that rather than emit an empty "Any: " list.
func TestAnyFailureMessageNamesNonDeterministicSubMatcherInsteadOfAnEmptyList(t *testing.T) {
	sub := &flippingMatcher{}
	m := Any(sub)

	if got := m.Match("x"); got != false {
		t.Fatalf("Match: got %v, want false", got)
	}
	got := m.FailureMessage("x")
	want := "Any: Match reported a failure but every sub-matcher now reports a match — a sub-matcher is not deterministic"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestAnyDescriptionNestsSubDescriptions(t *testing.T) {
	got := Any(Equal(1), BeNil()).(Describer).Description()
	want := "any of [equal to 1, nil]"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestAnyEmptyDescription(t *testing.T) {
	got := Any().(Describer).Description()
	want := "any of []"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// --- nesting -------------------------------------------------------------------------------------

func TestNotAllFailureMessageNestsDescriptions(t *testing.T) {
	got := Not(All(Equal(true), BeTrue())).FailureMessage(true)
	want := "expected true not to be all of [equal to true, true]"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestAllOfNotFailureMessage(t *testing.T) {
	got := All(Not(Equal(1)), BeTrue()).FailureMessage(1)
	want := `All: #1: "expected 1 not to be equal to 1"; #2: "expected true, got 1 (int)"`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// --- Description() on the existing built-in matchers ----------------------------------------------

func TestBuiltinMatchersImplementDescriber(t *testing.T) {
	sentinel := errors.New("boom")
	var target *customErr

	cases := map[string]struct {
		matcher Matcher
		want    string
	}{
		"Equal":        {Equal(43), "equal to 43"},
		"NotEqual":     {NotEqual(43), "not equal to 43"},
		"BeNil":        {BeNil(), "nil"},
		"BeTrue":       {BeTrue(), "true"},
		"BeFalse":      {BeFalse(), "false"},
		"Contain":      {Contain("x"), "containing x"},
		"MatchError":   {MatchError(sentinel), "an error matching boom (*errors.errorString)"},
		"MatchErrorAs": {MatchErrorAs(&target), "an error assignable to **assert.customErr"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			d, ok := tc.matcher.(Describer)
			if !ok {
				t.Fatalf("%T does not implement Describer", tc.matcher)
			}
			if got := d.Description(); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

type customErr struct{ msg string }

func (e *customErr) Error() string { return e.msg }
