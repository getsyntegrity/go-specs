package assert

import "testing"

// length_matchers_test.go pins HaveLen and BeEmpty: which kinds have a length, the exact failure
// wording for a mismatch and for a type with no length, and that neither ever panics.

func TestHaveLenMatches(t *testing.T) {
	ch := make(chan int, 3)
	ch <- 1
	ch <- 2
	var nilSlice []int
	var nilMap map[string]int
	var nilChan chan int
	cases := []struct {
		name   string
		actual any
		n      int
	}{
		{"string", "abc", 3},
		{"multibyte string counts bytes", "é", 2},
		{"[]any", []any{1, "a"}, 2},
		{"[]int", []int{1, 2, 3}, 3},
		{"[]string", []string{"a"}, 1},
		{"[]byte", []byte("ab"), 2},
		{"map[string]any", map[string]any{"a": 1}, 1},
		{"reflect slice", []struct{ X int }{{1}, {2}}, 2},
		{"reflect map", map[int]bool{1: true, 2: false, 3: true}, 3},
		{"array", [3]int{1, 2, 3}, 3},
		{"chan reports buffered count", ch, 2},
		{"nil slice", nilSlice, 0},
		{"nil map", nilMap, 0},
		{"nil chan", nilChan, 0},
		{"empty string", "", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !HaveLen(tc.n).Match(tc.actual) {
				t.Fatalf("expected %v (%T) to have length %d", tc.actual, tc.actual, tc.n)
			}
			if HaveLen(tc.n + 1).Match(tc.actual) {
				t.Fatalf("expected %v (%T) not to have length %d", tc.actual, tc.actual, tc.n+1)
			}
		})
	}
}

func TestHaveLenFailureMessages(t *testing.T) {
	type point struct{ X, Y int }
	cases := []struct {
		name   string
		n      int
		actual any
		want   string
	}{
		{"slice mismatch", 3, []int{1, 2}, "expected [1 2] to have length 3, got length 2"},
		{"string mismatch", 1, "abc", "expected abc to have length 1, got length 3"},
		{"map mismatch", 0, map[string]int{"a": 1}, "expected map[a:1] to have length 0, got length 1"},
		{"int has no length", 3, 42, "HaveLen: int has no length"},
		{"struct has no length", 3, point{1, 2}, "HaveLen: assert.point has no length"},
		{"nil has no length", 3, nil, "HaveLen: <nil> has no length"},
		{"negative expected never matches", -1, []int{}, "expected [] to have length -1, got length 0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("expected a failure report rather than a panic, got panic: %v", r)
				}
			}()
			m := HaveLen(tc.n)
			if m.Match(tc.actual) {
				t.Fatalf("expected no match for %v (%T)", tc.actual, tc.actual)
			}
			if got := m.FailureMessage(tc.actual); got != tc.want {
				t.Fatalf("FailureMessage:\n got  %q\n want %q", got, tc.want)
			}
		})
	}
}

func TestBeEmptyMatches(t *testing.T) {
	var nilSlice []string
	var nilMap map[int]int
	var nilChan chan struct{}
	for name, actual := range map[string]any{
		"empty string": "",
		"[]any":        []any{},
		"nil slice":    nilSlice,
		"nil map":      nilMap,
		"nil chan":     nilChan,
		"reflect":      []struct{ X int }{},
		"empty map":    map[string]any{},
		"zero array":   [0]int{},
		"unbuffered":   make(chan int),
	} {
		t.Run(name, func(t *testing.T) {
			if !BeEmpty().Match(actual) {
				t.Fatalf("expected %v (%T) to be empty", actual, actual)
			}
		})
	}
	for name, actual := range map[string]any{
		"string": "a", "[]int": []int{0}, "map": map[string]int{"a": 1}, "array": [1]int{},
	} {
		t.Run("non-empty "+name, func(t *testing.T) {
			if BeEmpty().Match(actual) {
				t.Fatalf("expected %v (%T) not to be empty", actual, actual)
			}
		})
	}
}

func TestBeEmptyFailureMessages(t *testing.T) {
	cases := []struct {
		name   string
		actual any
		want   string
	}{
		{"slice", []int{1}, "expected [1] to be empty, got length 1"},
		{"string", "ab", "expected ab to be empty, got length 2"},
		{"int has no length", 7, "BeEmpty: int has no length"},
		{"struct has no length", struct{}{}, "BeEmpty: struct {} has no length"},
		{"nil has no length", nil, "BeEmpty: <nil> has no length"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("expected a failure report rather than a panic, got panic: %v", r)
				}
			}()
			m := BeEmpty()
			if m.Match(tc.actual) {
				t.Fatalf("expected no match for %v (%T)", tc.actual, tc.actual)
			}
			if got := m.FailureMessage(tc.actual); got != tc.want {
				t.Fatalf("FailureMessage:\n got  %q\n want %q", got, tc.want)
			}
		})
	}
}

func TestLengthMatchersDescribe(t *testing.T) {
	if got := HaveLen(3).(Describer).Description(); got != "having length 3" {
		t.Fatalf("HaveLen description = %q", got)
	}
	if got := BeEmpty().(Describer).Description(); got != "empty" {
		t.Fatalf("BeEmpty description = %q", got)
	}
}

func TestLengthMatchersCompose(t *testing.T) {
	if !Not(HaveLen(2)).Match([]int{1}) || Not(HaveLen(1)).Match([]int{1}) {
		t.Fatal("Not(HaveLen) should negate")
	}
	if !Not(BeEmpty()).Match("a") || Not(BeEmpty()).Match("") {
		t.Fatal("Not(BeEmpty) should negate")
	}
	if !All(HaveLen(2), Not(BeEmpty())).Match([]int{1, 2}) {
		t.Fatal("All should hold")
	}
	if !Any(BeEmpty(), HaveLen(2)).Match([]int{1, 2}) {
		t.Fatal("Any should hold")
	}
}
