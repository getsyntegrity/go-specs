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

// A sibling that would have matched must not be reported as an ordinary failure (its FailureMessage
// describes a comparison that succeeded) nor silently dropped (that would hide why the nil forced
// this Any to fail); it gets its own branch naming the suppression.
func TestAnyFailureMessageReportsSuppressedMatchWhenNilForcesFailure(t *testing.T) {
	got := Any(Equal(1), nil).FailureMessage(1)
	want := "Any: #1: would have matched (equal to 1), but a nil entry forces this Any to fail; #2: nil matcher (never matches)"
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
