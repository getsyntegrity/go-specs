package assert

import (
	"math"
	"testing"
	"time"
)

// general_matchers_test.go pins BeZero and Satisfy: what counts as a zero value, the exact failure
// wording, a nil predicate failing instead of panicking, and composition.

type zeroPoint struct{ X, Y int }

func TestBeZeroMatches(t *testing.T) {
	var nilPtr *int
	var nilSlice []int
	var nilMap map[string]int
	var nilFunc func()
	var nilChan chan int
	var nilErr error
	n := 1
	cases := []struct {
		name   string
		actual any
		want   bool
	}{
		{"nil interface", nil, true},
		{"nil error", nilErr, true},
		{"int", 0, true},
		{"non-zero int", 1, false},
		{"int64", int64(0), true},
		{"uint8", uint8(0), true},
		{"string", "", true},
		{"non-empty string", "a", false},
		{"false", false, true},
		{"true", true, false},
		{"float", 0.0, true},
		{"non-zero float", 0.5, false},
		{"negative zero is zero", math.Copysign(0, -1), true},
		{"complex", complex(0, 0), true},
		{"nil pointer", nilPtr, true},
		{"non-nil pointer", &n, false},
		{"pointer to a zero value is not zero", new(int), false},
		{"nil slice", nilSlice, true},
		{"empty non-nil slice is not zero", []int{}, false},
		{"nil map", nilMap, true},
		{"empty non-nil map is not zero", map[string]int{}, false},
		{"nil func", nilFunc, true},
		{"nil chan", nilChan, true},
		{"zero struct", zeroPoint{}, true},
		{"struct with a field", zeroPoint{X: 1}, false},
		{"zero array", [3]int{}, true},
		{"array with an element", [3]int{0, 1, 0}, false},
		{"zero time.Time", time.Time{}, true},
		{"time.Time", time.Unix(1, 0), false},
		{"zero duration", time.Duration(0), true},
		{"named string", namedText(""), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer mustNotPanic(t)
			if got := BeZero().Match(tc.actual); got != tc.want {
				t.Fatalf("BeZero().Match(%v (%T)) = %v, want %v", tc.actual, tc.actual, got, tc.want)
			}
		})
	}
}

func TestBeZeroFailureMessages(t *testing.T) {
	cases := []struct {
		name   string
		actual any
		want   string
	}{
		{"int", 5, "expected 5 to be the zero value of int"},
		{"string", "a", "expected a to be the zero value of string"},
		{"struct", zeroPoint{1, 2}, "expected {1 2} to be the zero value of assert.zeroPoint"},
		{"empty slice", []int{}, "expected [] to be the zero value of []int"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer mustNotPanic(t)
			m := BeZero()
			if m.Match(tc.actual) {
				t.Fatalf("expected no match for %v (%T)", tc.actual, tc.actual)
			}
			if got := m.FailureMessage(tc.actual); got != tc.want {
				t.Fatalf("FailureMessage:\n got  %q\n want %q", got, tc.want)
			}
		})
	}
}

func TestSatisfyMatches(t *testing.T) {
	even := func(v any) bool { n, ok := v.(int); return ok && n%2 == 0 }
	m := Satisfy("is even", even)
	if !m.Match(4) || m.Match(3) || m.Match("4") || m.Match(nil) {
		t.Fatal("Satisfy should follow its predicate, including for a nil or wrongly typed actual")
	}
	var got any = "unset"
	Satisfy("records", func(v any) bool { got = v; return true }).Match(nil)
	if got != nil {
		t.Fatalf("the predicate should receive the actual as is, got %v", got)
	}
}

func TestSatisfyFailureMessages(t *testing.T) {
	no := func(any) bool { return false }
	cases := []struct {
		name    string
		matcher Matcher
		actual  any
		want    string
	}{
		{"description", Satisfy("is even", no), 3, `expected 3 to satisfy "is even"`},
		{"empty description", Satisfy("", no), 3, "expected 3 to satisfy the given predicate"},
		{"nil actual", Satisfy("is set", no), nil, `expected <nil> to satisfy "is set"`},
		{"nil predicate", Satisfy("is even", nil), 3, `Satisfy: no predicate given for "is even"`},
		{"nil predicate, no description", Satisfy("", nil), 3, "Satisfy: no predicate given"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer mustNotPanic(t)
			if tc.matcher.Match(tc.actual) {
				t.Fatalf("expected no match for %v", tc.actual)
			}
			if got := tc.matcher.FailureMessage(tc.actual); got != tc.want {
				t.Fatalf("FailureMessage:\n got  %q\n want %q", got, tc.want)
			}
		})
	}
}

func TestGeneralMatchersDoNotAllocateOnSuccess(t *testing.T) {
	zero, sat := BeZero(), Satisfy("positive", func(v any) bool { return v.(int) > 0 })
	var z, i, s, st any = 0, 5, "", zeroPoint{}
	if allocs := testing.AllocsPerRun(100, func() {
		_ = zero.Match(z)
		_ = zero.Match(s)
		_ = zero.Match(st)
		_ = zero.Match(nil)
		_ = sat.Match(i)
	}); allocs != 0 {
		t.Fatalf("expected no allocations, got %v", allocs)
	}
}

func TestGeneralMatchersDescribe(t *testing.T) {
	cases := []struct {
		m    Matcher
		want string
	}{
		{BeZero(), "the zero value"},
		{Satisfy("is even", func(any) bool { return true }), `satisfying "is even"`},
		{Satisfy("", nil), "satisfying the given predicate"},
	}
	for _, tc := range cases {
		if got := tc.m.(Describer).Description(); got != tc.want {
			t.Fatalf("description = %q, want %q", got, tc.want)
		}
	}
}

func TestGeneralMatchersCompose(t *testing.T) {
	positive := Satisfy("positive", func(v any) bool { n, ok := v.(int); return ok && n > 0 })
	if !Not(BeZero()).Match(1) || Not(BeZero()).Match(0) {
		t.Fatal("Not(BeZero) should negate")
	}
	if !All(Not(BeZero()), positive).Match(3) {
		t.Fatal("All should hold")
	}
	if !Any(BeZero(), positive).Match(0) {
		t.Fatal("Any should hold")
	}
	if All(BeZero(), positive).Match(0) {
		t.Fatal("All should fail when one fails")
	}
}
