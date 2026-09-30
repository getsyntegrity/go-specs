package assert

import (
	"errors"
	"fmt"
	"testing"
)

// map_matchers_test.go pins HaveKey, HaveValue and HavePair: the supported map shapes, key type
// mismatch as a plain non-match, exact failure wording, no panics, and composition.

type namedKeyMap map[namedText]int

func TestHaveKeyMatches(t *testing.T) {
	var nilInterfaceKeyed map[any]int
	cases := []struct {
		name   string
		actual any
		key    any
		want   bool
	}{
		{"map[string]any present", map[string]any{"a": 1}, "a", true},
		{"map[string]any absent", map[string]any{"a": 1}, "b", false},
		{"map[string]any nil value still a key", map[string]any{"a": nil}, "a", true},
		{"map[string]string present", map[string]string{"a": "x"}, "a", true},
		{"map[string]string absent", map[string]string{"a": "x"}, "z", false},
		{"fast path with non-string key", map[string]string{"a": "x"}, 1, false},
		{"reflect int keys", map[int]bool{1: true}, 1, true},
		{"reflect int keys absent", map[int]bool{1: true}, 2, false},
		{"named key type", namedKeyMap{"a": 1}, namedText("a"), true},
		{"plain string is not a named key", namedKeyMap{"a": 1}, "a", false},
		{"interface keys", map[any]int{1: 1, "a": 2}, "a", true},
		{"interface keys, wrong dynamic type", map[any]int{1: 1}, "1", false},
		{"interface keys, nil key present", map[any]int{nil: 1}, nil, true},
		{"nil key against typed keys", map[string]int{"a": 1}, nil, false},
		{"unhashable key does not panic", map[any]int{1: 1}, []int{1}, false},
		{"nil map", map[string]int(nil), "a", false},
		{"nil interface keyed map", nilInterfaceKeyed, "a", false},
		{"empty map", map[string]any{}, "a", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer mustNotPanic(t)
			if got := HaveKey(tc.key).Match(tc.actual); got != tc.want {
				t.Fatalf("HaveKey(%v).Match(%v (%T)) = %v, want %v", tc.key, tc.actual, tc.actual, got, tc.want)
			}
		})
	}
}

func TestHaveValueMatches(t *testing.T) {
	sentinel := errors.New("boom")
	cases := []struct {
		name   string
		actual any
		value  any
		want   bool
	}{
		{"map[string]any present", map[string]any{"a": 1, "b": "x"}, "x", true},
		{"map[string]any absent", map[string]any{"a": 1}, 2, false},
		{"map[string]any nil value", map[string]any{"a": nil}, nil, true},
		{"map[string]any wrong kind of number", map[string]any{"a": 1}, int64(1), false},
		{"map[string]string present", map[string]string{"a": "x"}, "x", true},
		{"map[string]string absent", map[string]string{"a": "x"}, "y", false},
		{"map[string]string non-string value", map[string]string{"a": "x"}, 1, false},
		{"reflect values", map[int]float64{1: 2.5}, 2.5, true},
		{"reflect structural equality", map[string][]int{"a": {1, 2}}, []int{1, 2}, true},
		{"errors compare by identity", map[string]error{"a": fmt.Errorf("wrapped: %w", sentinel)}, sentinel, true},
		{"nil map", map[string]int(nil), 1, false},
		{"empty map", map[int]int{}, 1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer mustNotPanic(t)
			if got := HaveValue(tc.value).Match(tc.actual); got != tc.want {
				t.Fatalf("HaveValue(%v).Match(%v (%T)) = %v, want %v", tc.value, tc.actual, tc.actual, got, tc.want)
			}
		})
	}
}

func TestHavePairMatches(t *testing.T) {
	cases := []struct {
		name   string
		actual any
		key    any
		value  any
		want   bool
	}{
		{"map[string]any", map[string]any{"a": 1}, "a", 1, true},
		{"map[string]any wrong value", map[string]any{"a": 1}, "a", 2, false},
		{"map[string]any wrong key", map[string]any{"a": 1}, "b", 1, false},
		{"map[string]any nil value", map[string]any{"a": nil}, "a", nil, true},
		{"map[string]string", map[string]string{"a": "x"}, "a", "x", true},
		{"map[string]string wrong value", map[string]string{"a": "x"}, "a", "y", false},
		{"map[string]string non-string value", map[string]string{"a": "x"}, "a", 1, false},
		{"reflect", map[int]bool{1: true}, 1, true, true},
		{"reflect wrong value", map[int]bool{1: true}, 1, false, false},
		{"reflect key type mismatch", map[int]bool{1: true}, "1", true, false},
		{"value found under another key only", map[string]int{"a": 1, "b": 2}, "a", 2, false},
		{"nil map", map[string]int(nil), "a", 1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer mustNotPanic(t)
			if got := HavePair(tc.key, tc.value).Match(tc.actual); got != tc.want {
				t.Fatalf("HavePair(%v, %v).Match(%v (%T)) = %v, want %v", tc.key, tc.value, tc.actual, tc.actual, got, tc.want)
			}
		})
	}
}

func TestMapMatchersFailureMessages(t *testing.T) {
	cases := []struct {
		name    string
		matcher Matcher
		actual  any
		want    string
	}{
		{"HaveKey missing", HaveKey("b"), map[string]int{"a": 1}, "expected map[a:1] to have key b"},
		{"HaveKey missing on fast path", HaveKey("b"), map[string]any{"a": 1}, "expected map[a:1] to have key b"},
		{"HaveKey nil map", HaveKey("b"), map[string]int(nil), "expected map[] to have key b"},
		{"HaveKey type mismatch", HaveKey(1), map[string]int{"a": 1}, "expected map[a:1] to have key 1 — map[string]int keys are string, got int"},
		{"HaveKey nil key mismatch", HaveKey(nil), map[string]int{"a": 1}, "expected map[a:1] to have key <nil> — map[string]int keys are string, got <nil>"},
		{"HaveKey fast path type mismatch", HaveKey(1), map[string]string{"a": "x"}, "expected map[a:x] to have key 1 — map[string]string keys are string, got int"},
		{"HaveKey not a map", HaveKey("a"), 42, "HaveKey: int is not a map"},
		{"HaveKey nil actual", HaveKey("a"), nil, "HaveKey: <nil> is not a map"},
		{"HaveKey slice actual", HaveKey(0), []int{1}, "HaveKey: []int is not a map"},
		{"HaveValue missing", HaveValue(2), map[string]int{"a": 1}, "expected map[a:1] to have value 2"},
		{"HaveValue type mismatch", HaveValue("x"), map[string]int{"a": 1}, "expected map[a:1] to have value x — map[string]int values are int, got string"},
		{"HaveValue not a map", HaveValue(1), "abc", "HaveValue: string is not a map"},
		{"HavePair missing key", HavePair("b", 2), map[string]int{"a": 1}, "expected map[a:1] to have key b with value 2 — key is missing"},
		{"HavePair wrong value", HavePair("a", 2), map[string]int{"a": 1}, "expected map[a:1] to have key a with value 2 — key has value 1"},
		{"HavePair wrong value on fast path", HavePair("a", "y"), map[string]string{"a": "x"}, "expected map[a:x] to have key a with value y — key has value x"},
		{"HavePair key type mismatch", HavePair(1, 2), map[string]int{"a": 1}, "expected map[a:1] to have key 1 with value 2 — map[string]int keys are string, got int"},
		{"HavePair not a map", HavePair("a", 1), 3.5, "HavePair: float64 is not a map"},
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

func TestMapMatchersDoNotAllocateOnSuccess(t *testing.T) {
	ma := map[string]any{"a": 1}
	ms := map[string]string{"a": "x"}
	key, val, pair := HaveKey("a"), HaveValue("x"), HavePair("a", "x")
	anyKey, anyPair := HaveKey("a"), HavePair("a", 1)
	if allocs := testing.AllocsPerRun(100, func() {
		_ = key.Match(ms)
		_ = val.Match(ms)
		_ = pair.Match(ms)
		_ = anyKey.Match(ma)
		_ = anyPair.Match(ma)
	}); allocs != 0 {
		t.Fatalf("expected no allocations on the fast paths, got %v", allocs)
	}
}

func TestMapMatchersDescribe(t *testing.T) {
	cases := []struct {
		m    Matcher
		want string
	}{
		{HaveKey("a"), "having key a"},
		{HaveValue(2), "having value 2"},
		{HavePair("a", 1), "having key a with value 1"},
	}
	for _, tc := range cases {
		if got := tc.m.(Describer).Description(); got != tc.want {
			t.Fatalf("description = %q, want %q", got, tc.want)
		}
	}
}

func TestMapMatchersCompose(t *testing.T) {
	m := map[string]int{"a": 1, "b": 2}
	if !Not(HaveKey("z")).Match(m) || Not(HaveKey("a")).Match(m) {
		t.Fatal("Not(HaveKey) should negate")
	}
	if !All(HaveKey("a"), HaveValue(2), HavePair("b", 2), Not(HaveLen(0))).Match(m) {
		t.Fatal("All should hold")
	}
	if !Any(HaveKey("z"), HavePair("a", 1)).Match(m) {
		t.Fatal("Any should hold")
	}
	if All(HaveKey("a"), HavePair("a", 2)).Match(m) {
		t.Fatal("All should fail when one fails")
	}
}
