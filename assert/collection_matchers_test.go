package assert

import (
	"errors"
	"fmt"
	"testing"
)

// collection_matchers_test.go pins ContainAllOf, ContainAnyOf, ContainTheSameElementsAs and
// BeOneOf: supported collections, element equality (ValuesEqual's), duplicates counting for the
// multiset matcher, exact failure wording, no panics, and composition.

func TestContainAllOfAndAnyOfMatch(t *testing.T) {
	sentinel := errors.New("boom")
	cases := []struct {
		name    string
		actual  any
		elems   []any
		wantAll bool
		wantAny bool
	}{
		{"[]int all present", []int{1, 2, 3}, []any{3, 1}, true, true},
		{"[]int some present", []int{1, 2, 3}, []any{3, 9}, false, true},
		{"[]int none present", []int{1, 2, 3}, []any{8, 9}, false, false},
		{"[]int wrong element type", []int{1, 2}, []any{"1"}, false, false},
		{"[]int int64 is not int", []int{1, 2}, []any{int64(1)}, false, false},
		{"[]string", []string{"a", "b"}, []any{"b", "a"}, true, true},
		{"[]float64", []float64{1.5, 2.5}, []any{2.5}, true, true},
		{"[]any mixed", []any{1, "a", nil}, []any{"a", nil, 1}, true, true},
		{"[]any structural", []any{[]int{1}}, []any{[]int{1}}, true, true},
		{"reflect slice", []uint8{1, 2}, []any{uint8(2)}, true, true},
		{"array", [3]int{1, 2, 3}, []any{2, 3}, true, true},
		{"errors by identity", []error{fmt.Errorf("w: %w", sentinel)}, []any{sentinel}, true, true},
		{"string substrings", "hello world", []any{"hello", "wor"}, true, true},
		{"string some substrings", "hello", []any{"he", "xyz"}, false, true},
		{"string with non-string element", "hello", []any{1}, false, false},
		{"duplicates in elems are fine", []int{1}, []any{1, 1}, true, true},
		{"nil slice", []int(nil), []any{1}, false, false},
		{"no elements: all is vacuous, any never holds", []int{1}, nil, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer mustNotPanic(t)
			if got := ContainAllOf(tc.elems...).Match(tc.actual); got != tc.wantAll {
				t.Fatalf("ContainAllOf(%v).Match(%v) = %v, want %v", tc.elems, tc.actual, got, tc.wantAll)
			}
			if got := ContainAnyOf(tc.elems...).Match(tc.actual); got != tc.wantAny {
				t.Fatalf("ContainAnyOf(%v).Match(%v) = %v, want %v", tc.elems, tc.actual, got, tc.wantAny)
			}
		})
	}
}

func TestContainTheSameElementsAsMatches(t *testing.T) {
	sentinel := errors.New("boom")
	cases := []struct {
		name     string
		actual   any
		expected any
		want     bool
	}{
		{"same order", []int{1, 2, 3}, []int{1, 2, 3}, true},
		{"different order", []int{3, 1, 2}, []int{1, 2, 3}, true},
		{"duplicates count", []int{1, 1, 2}, []int{1, 2, 2}, false},
		{"same duplicates, other order", []int{1, 2, 1}, []int{1, 1, 2}, true},
		{"extra element", []int{1, 2, 3}, []int{1, 2}, false},
		{"missing element", []int{1}, []int{1, 2}, false},
		{"both empty", []int{}, []int{}, true},
		{"nil and empty", []string(nil), []string{}, true},
		{"[]string", []string{"b", "a"}, []string{"a", "b"}, true},
		{"[]any", []any{1, "a", nil}, []any{nil, "a", 1}, true},
		{"[]int against []any", []int{2, 1}, []any{1, 2}, true},
		{"[]int against []any of another kind", []int{1}, []any{int64(1)}, false},
		{"array against slice", [2]int{2, 1}, []int{1, 2}, true},
		{"structs", []struct{ X int }{{1}, {2}}, []struct{ X int }{{2}, {1}}, true},
		{"errors by identity", []error{fmt.Errorf("w: %w", sentinel)}, []error{sentinel}, true},
		{"more than 64 elements", seq(100), reversed(seq(100)), true},
		{"more than 64 elements, one differs", seq(100), append(reversed(seq(99)), 1000), false},
		{"[]any more than 64 elements", anySlice(seq(70)), anySlice(reversed(seq(70))), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer mustNotPanic(t)
			if got := ContainTheSameElementsAs(tc.expected).Match(tc.actual); got != tc.want {
				t.Fatalf("ContainTheSameElementsAs(%v).Match(%v) = %v, want %v", tc.expected, tc.actual, got, tc.want)
			}
		})
	}
}

func seq(n int) []int {
	s := make([]int, n)
	for i := range s {
		s[i] = i
	}
	return s
}

func reversed(s []int) []int {
	r := make([]int, len(s))
	for i, v := range s {
		r[len(s)-1-i] = v
	}
	return r
}

func anySlice(s []int) []any {
	r := make([]any, len(s))
	for i, v := range s {
		r[i] = v
	}
	return r
}

func TestBeOneOfMatches(t *testing.T) {
	sentinel := errors.New("boom")
	cases := []struct {
		name   string
		actual any
		values []any
		want   bool
	}{
		{"int", 2, []any{1, 2, 3}, true},
		{"absent", 5, []any{1, 2, 3}, false},
		{"string", "b", []any{"a", "b"}, true},
		{"other kind of number", 1, []any{int64(1)}, false},
		{"nil", nil, []any{1, nil}, true},
		{"nil absent", nil, []any{1}, false},
		{"structural", []int{1}, []any{[]int{2}, []int{1}}, true},
		{"errors by identity", fmt.Errorf("w: %w", sentinel), []any{sentinel}, true},
		{"no values never matches", 1, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer mustNotPanic(t)
			if got := BeOneOf(tc.values...).Match(tc.actual); got != tc.want {
				t.Fatalf("BeOneOf(%v).Match(%v) = %v, want %v", tc.values, tc.actual, got, tc.want)
			}
		})
	}
}

func TestCollectionMatchersFailureMessages(t *testing.T) {
	cases := []struct {
		name    string
		matcher Matcher
		actual  any
		want    string
	}{
		{"ContainAllOf lists what is missing", ContainAllOf(1, 3, 4), []int{1, 2}, "expected [1 2] to contain all of [1 3 4], missing [3 4]"},
		{"ContainAllOf string", ContainAllOf("he", "xyz"), "hello", "expected hello to contain all of [he xyz], missing [xyz]"},
		{"ContainAllOf not a collection", ContainAllOf(1), 42, "ContainAllOf: int is not a string, slice or array"},
		{"ContainAllOf nil", ContainAllOf(1), nil, "ContainAllOf: <nil> is not a string, slice or array"},
		{"ContainAllOf map", ContainAllOf(1), map[string]int{"a": 1}, "ContainAllOf: map[string]int is not a string, slice or array"},
		{"ContainAnyOf", ContainAnyOf(3, 4), []int{1, 2}, "expected [1 2] to contain any of [3 4]"},
		{"ContainAnyOf no elements", ContainAnyOf(), []int{1}, "expected [1] to contain any of []"},
		{"ContainAnyOf not a collection", ContainAnyOf(1), true, "ContainAnyOf: bool is not a string, slice or array"},
		{"TheSame missing and unexpected", ContainTheSameElementsAs([]int{1, 1, 2}), []int{1, 2, 2}, "expected [1 2 2] to contain the same elements as [1 1 2] — missing [1], unexpected [2]"},
		{"TheSame only unexpected", ContainTheSameElementsAs([]int{1}), []int{1, 2}, "expected [1 2] to contain the same elements as [1] — unexpected [2]"},
		{"TheSame only missing", ContainTheSameElementsAs([]string{"a", "b"}), []string{"a"}, "expected [a] to contain the same elements as [a b] — missing [b]"},
		{"TheSame []any vs []int", ContainTheSameElementsAs([]any{1, "x"}), []int{1}, "expected [1] to contain the same elements as [1 x] — missing [x]"},
		{"TheSame actual not a collection", ContainTheSameElementsAs([]int{1}), 7, "ContainTheSameElementsAs: int is not a slice or array"},
		{"TheSame string actual is unsupported", ContainTheSameElementsAs([]string{"a"}), "a", "ContainTheSameElementsAs: string is not a slice or array"},
		{"TheSame expected not a collection", ContainTheSameElementsAs(5), []int{1}, "ContainTheSameElementsAs: expected value must be a slice or array, got int"},
		{"TheSame expected nil", ContainTheSameElementsAs(nil), []int{1}, "ContainTheSameElementsAs: expected value must be a slice or array, got <nil>"},
		{"BeOneOf", BeOneOf(1, 2, 3), 5, "expected 5 to be one of [1 2 3]"},
		{"BeOneOf no values", BeOneOf(), 5, "expected 5 to be one of []"},
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

func TestCollectionMatchersDoNotAllocateOnSuccess(t *testing.T) {
	// Boxed once up front: converting a slice to any at each call would itself allocate.
	var ints, strs, anys any = []int{1, 2, 3}, []string{"a", "b"}, []any{1, "a"}
	all, anyOf := ContainAllOf(1, 3), ContainAnyOf(9, 3)
	allStr, anyAny := ContainAllOf("a"), ContainAnyOf("a")
	same, sameStr := ContainTheSameElementsAs([]int{3, 2, 1}), ContainTheSameElementsAs([]string{"b", "a"})
	oneOf := BeOneOf(1, 2, 3)
	if allocs := testing.AllocsPerRun(100, func() {
		_ = all.Match(ints)
		_ = anyOf.Match(ints)
		_ = allStr.Match(strs)
		_ = anyAny.Match(anys)
		_ = same.Match(ints)
		_ = sameStr.Match(strs)
		_ = oneOf.Match(2)
	}); allocs != 0 {
		t.Fatalf("expected no allocations on the fast paths, got %v", allocs)
	}
}

func TestContainMatchersCopyTheirElements(t *testing.T) {
	elems := []any{1, 2}
	m := ContainAllOf(elems...)
	elems[0] = 99
	if !m.Match([]int{1, 2}) {
		t.Fatal("mutating the caller's slice after construction must not change the matcher")
	}
}

func TestCollectionMatchersDescribe(t *testing.T) {
	cases := []struct {
		m    Matcher
		want string
	}{
		{ContainAllOf(1, 2), "containing all of [1 2]"},
		{ContainAnyOf("a"), "containing any of [a]"},
		{ContainTheSameElementsAs([]int{1, 2}), "containing the same elements as [1 2]"},
		{BeOneOf(1, 2), "one of [1 2]"},
	}
	for _, tc := range cases {
		if got := tc.m.(Describer).Description(); got != tc.want {
			t.Fatalf("description = %q, want %q", got, tc.want)
		}
	}
}

func TestCollectionMatchersCompose(t *testing.T) {
	if !Not(ContainAllOf(9)).Match([]int{1}) || Not(ContainAllOf(1)).Match([]int{1}) {
		t.Fatal("Not(ContainAllOf) should negate")
	}
	if !All(ContainAllOf(1), Not(ContainAnyOf(9)), ContainTheSameElementsAs([]int{2, 1})).Match([]int{1, 2}) {
		t.Fatal("All should hold")
	}
	if !Any(BeOneOf(7), ContainAnyOf(2)).Match([]int{1, 2}) {
		t.Fatal("Any should hold")
	}
	if !BeOneOf("a", "b").Match("a") || Not(BeOneOf("a", "b")).Match("a") {
		t.Fatal("BeOneOf should compose")
	}
}
