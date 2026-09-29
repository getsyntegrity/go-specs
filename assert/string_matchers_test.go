package assert

import (
	"strings"
	"testing"
)

// string_matchers_test.go pins StartWith, EndWith and MatchRegex: which actuals are text, the exact
// failure wording, that an invalid pattern fails instead of panicking, and composition.

type namedText string

func mustNotPanic(t *testing.T) {
	t.Helper()
	if r := recover(); r != nil {
		t.Fatalf("expected a failure report rather than a panic, got panic: %v", r)
	}
}

func TestStringMatchersMatch(t *testing.T) {
	cases := []struct {
		name    string
		matcher Matcher
		actual  any
	}{
		{"StartWith string", StartWith("ab"), "abc"},
		{"StartWith empty prefix", StartWith(""), "abc"},
		{"StartWith equal", StartWith("abc"), "abc"},
		{"StartWith []byte", StartWith("ab"), []byte("abc")},
		{"StartWith named string", StartWith("ab"), namedText("abc")},
		{"EndWith string", EndWith("bc"), "abc"},
		{"EndWith empty suffix", EndWith(""), ""},
		{"EndWith []byte", EndWith("bc"), []byte("abc")},
		{"EndWith named string", EndWith("bc"), namedText("abc")},
		{"MatchRegex string", MatchRegex(`^a.c$`), "abc"},
		{"MatchRegex unanchored", MatchRegex(`b`), "abc"},
		{"MatchRegex []byte", MatchRegex(`^a+$`), []byte("aaa")},
		{"MatchRegex named string", MatchRegex(`c$`), namedText("abc")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !tc.matcher.Match(tc.actual) {
				t.Fatalf("expected %v (%T) to match", tc.actual, tc.actual)
			}
		})
	}
}

func TestStringMatchersMismatch(t *testing.T) {
	cases := []struct {
		name    string
		matcher Matcher
		actual  any
		want    string
	}{
		{"StartWith", StartWith("x"), "abc", `expected "abc" to start with "x"`},
		{"StartWith longer prefix", StartWith("abcd"), "abc", `expected "abc" to start with "abcd"`},
		{"StartWith []byte", StartWith("x"), []byte("abc"), `expected "abc" to start with "x"`},
		{"EndWith", EndWith("x"), "abc", `expected "abc" to end with "x"`},
		{"EndWith []byte", EndWith("x"), []byte("abc"), `expected "abc" to end with "x"`},
		{"MatchRegex", MatchRegex(`^b`), "abc", `expected "abc" to match regex "^b"`},
		{"MatchRegex []byte", MatchRegex(`^b`), []byte("abc"), `expected "abc" to match regex "^b"`},
		{"StartWith int", StartWith("a"), 42, "StartWith: int is not a string or []byte"},
		{"EndWith nil", EndWith("a"), nil, "EndWith: <nil> is not a string or []byte"},
		{"MatchRegex struct", MatchRegex("a"), struct{}{}, "MatchRegex: struct {} is not a string or []byte"},
		{"StartWith []string", StartWith("a"), []string{"a"}, "StartWith: []string is not a string or []byte"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer mustNotPanic(t)
			if tc.matcher.Match(tc.actual) {
				t.Fatalf("expected no match for %v (%T)", tc.actual, tc.actual)
			}
			if got := tc.matcher.FailureMessage(tc.actual); got != tc.want {
				t.Fatalf("FailureMessage:\n got  %q\n want %q", got, tc.want)
			}
		})
	}
}

func TestMatchRegexInvalidPatternAlwaysFails(t *testing.T) {
	defer mustNotPanic(t)
	m := MatchRegex("(")
	for _, actual := range []any{"(", "anything", []byte("x"), 1, nil} {
		if m.Match(actual) {
			t.Fatalf("an invalid pattern must never match %v", actual)
		}
		got := m.FailureMessage(actual)
		if !strings.HasPrefix(got, `MatchRegex: invalid pattern "(": `) || !strings.Contains(got, "missing closing )") {
			t.Fatalf("FailureMessage should carry the compile error, got %q", got)
		}
	}
	if Not(m).Match("x") != true {
		t.Fatal("Not of an always-failing matcher matches")
	}
}

func TestMatchRegexCompilesOnce(t *testing.T) {
	m := MatchRegex(`^a+$`)
	if allocs := testing.AllocsPerRun(100, func() { _ = m.Match("aaaa") }); allocs != 0 {
		t.Fatalf("expected no allocations on a string success path, got %v", allocs)
	}
}

func TestStartEndWithDoNotAllocateOnSuccess(t *testing.T) {
	s, e := StartWith("ab"), EndWith("bc")
	b := []byte("abc")
	if allocs := testing.AllocsPerRun(100, func() {
		_ = s.Match("abc")
		_ = e.Match("abc")
		_ = s.Match(b)
		_ = e.Match(b)
	}); allocs != 0 {
		t.Fatalf("expected no allocations, got %v", allocs)
	}
}

func TestStringMatchersDescribe(t *testing.T) {
	cases := []struct {
		m    Matcher
		want string
	}{
		{StartWith("ab"), `starting with "ab"`},
		{EndWith("bc"), `ending with "bc"`},
		{MatchRegex(`^a`), `matching regex "^a"`},
		{MatchRegex("("), `matching regex "(" (invalid pattern)`},
	}
	for _, tc := range cases {
		if got := tc.m.(Describer).Description(); got != tc.want {
			t.Fatalf("description = %q, want %q", got, tc.want)
		}
	}
}

func TestStringMatchersCompose(t *testing.T) {
	if !Not(StartWith("x")).Match("abc") || Not(StartWith("a")).Match("abc") {
		t.Fatal("Not(StartWith) should negate")
	}
	if !All(StartWith("a"), EndWith("c"), MatchRegex(`b`)).Match("abc") {
		t.Fatal("All should hold")
	}
	if !Any(StartWith("x"), EndWith("c")).Match("abc") {
		t.Fatal("Any should hold")
	}
	if All(StartWith("a"), EndWith("x")).Match("abc") {
		t.Fatal("All should fail when one fails")
	}
}
