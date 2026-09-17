package assert

import (
	"testing"
)

// The matcher failure message is the framework's product: when a spec fails, the string built here
// is the entire answer the user gets. Match() was already exercised through the DSL, but every
// FailureMessage sat at 0% — a wrong format verb or a swapped actual/expected pair would have
// shipped with nothing to catch it. These tests pin the exact wording of each message.

func TestEqualFailureMessageNamesActualThenExpected(t *testing.T) {
	got := Equal(43).FailureMessage(42)
	if got != "expected 42 to equal 43" {
		t.Fatalf("got %q, want %q", got, "expected 42 to equal 43")
	}
}

func TestNotEqualFailureMessageNamesActualThenExpected(t *testing.T) {
	got := NotEqual("same").FailureMessage("same")
	if got != "expected same not to equal same" {
		t.Fatalf("got %q, want %q", got, "expected same not to equal same")
	}
}

func TestBeNilFailureMessageReportsValueAndType(t *testing.T) {
	got := BeNil().FailureMessage(42)
	if got != "expected nil, got 42 (int)" {
		t.Fatalf("got %q, want %q", got, "expected nil, got 42 (int)")
	}
}

func TestBeTrueFailureMessageReportsValueAndType(t *testing.T) {
	got := BeTrue().FailureMessage("yes")
	if got != "expected true, got yes (string)" {
		t.Fatalf("got %q, want %q", got, "expected true, got yes (string)")
	}
}

func TestBeFalseFailureMessageReportsValueAndType(t *testing.T) {
	got := BeFalse().FailureMessage(true)
	if got != "expected false, got true (bool)" {
		t.Fatalf("got %q, want %q", got, "expected false, got true (bool)")
	}
}

func TestContainFailureMessageNamesContainerThenNeedle(t *testing.T) {
	got := Contain("x").FailureMessage("abc")
	if got != "expected abc to contain x" {
		t.Fatalf("got %q, want %q", got, "expected abc to contain x")
	}
}

// Nil is a value a failure message must survive, not crash on: BeTrue().FailureMessage(nil) runs
// whenever a spec asserts on an untyped nil interface.
func TestFailureMessagesHandleNilActual(t *testing.T) {
	cases := map[string]struct {
		matcher Matcher
		want    string
	}{
		"equal":     {Equal(1), "expected <nil> to equal 1"},
		"not equal": {NotEqual(1), "expected <nil> not to equal 1"},
		"be nil":    {BeNil(), "expected nil, got <nil> (<nil>)"},
		"be true":   {BeTrue(), "expected true, got <nil> (<nil>)"},
		"be false":  {BeFalse(), "expected false, got <nil> (<nil>)"},
		"contain":   {Contain(1), "expected <nil> to contain 1"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := tc.matcher.FailureMessage(nil); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// equalMatcher.Match opens with a type switch over int/string/bool/int64/float64 that shortcuts
// ValuesEqual. Only the int arm was ever reached (25% coverage), so the rest was unverified.
func TestEqualMatchesEachFastPathType(t *testing.T) {
	cases := map[string]struct {
		expected any
		actual   any
		want     bool
	}{
		"int equal":        {1, 1, true},
		"int different":    {1, 2, false},
		"string equal":     {"a", "a", true},
		"string different": {"a", "b", false},
		"bool equal":       {true, true, true},
		"bool different":   {true, false, false},
		"int64 equal":      {int64(1), int64(1), true},
		"int64 different":  {int64(1), int64(2), false},
		"float64 equal":    {1.5, 1.5, true},
		"float64 differs":  {1.5, 2.5, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := Equal(tc.expected).Match(tc.actual); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// A fast-path arm only fires when BOTH sides share the type; a mismatched pair must fall through to
// ValuesEqual rather than silently comparing across widths. int(1) and int64(1) are not equal here.
func TestEqualDoesNotCompareAcrossNumericTypes(t *testing.T) {
	if Equal(int64(1)).Match(1) {
		t.Fatal("expected int 1 and int64 1 to differ, but the matcher reported them equal")
	}
	if Equal(1.0).Match(1) {
		t.Fatal("expected int 1 and float64 1.0 to differ, but the matcher reported them equal")
	}
}

// Beyond the fast-path types, Equal falls back to ValuesEqual, which ends in reflect.DeepEqual.
func TestEqualFallsBackToDeepEqualForCompositeValues(t *testing.T) {
	type sample struct {
		ID   int
		Tags []string
	}
	left := sample{ID: 1, Tags: []string{"a"}}
	right := sample{ID: 1, Tags: []string{"a"}}
	if !Equal(left).Match(right) {
		t.Fatal("expected structurally identical structs to match")
	}
	if Equal(left).Match(sample{ID: 1, Tags: []string{"b"}}) {
		t.Fatal("expected structs with different slice contents to differ")
	}
}

// Contain's type switch handles string, []int, []string and []float64 directly and sends everything
// else through reflection. Only one arm was covered.
func TestContainMatchesEachSupportedContainer(t *testing.T) {
	cases := map[string]struct {
		expected  any
		container any
		want      bool
	}{
		"substring present":       {"bc", "abcd", true},
		"substring absent":        {"xy", "abcd", false},
		"int slice present":       {2, []int{1, 2, 3}, true},
		"int slice absent":        {9, []int{1, 2, 3}, false},
		"string slice present":    {"b", []string{"a", "b"}, true},
		"string slice absent":     {"z", []string{"a", "b"}, false},
		"float64 slice present":   {2.5, []float64{1.5, 2.5}, true},
		"float64 slice absent":    {9.5, []float64{1.5, 2.5}, false},
		"reflected slice present": {int64(2), []int64{1, 2}, true},
		"reflected slice absent":  {int64(9), []int64{1, 2}, false},
		"array present":           {2, [3]int{1, 2, 3}, true},
		"array absent":            {9, [3]int{1, 2, 3}, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := Contain(tc.expected).Match(tc.container); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// A needle of the wrong type, or a container that holds nothing, must report "does not contain"
// rather than panicking on a failed type assertion or a reflect.Value of the wrong kind.
func TestContainRejectsUnsupportedOperands(t *testing.T) {
	cases := map[string]struct {
		expected  any
		container any
	}{
		"non-string needle in string": {1, "abc"},
		"string needle in int slice":  {"1", []int{1}},
		"int needle in string slice":  {1, []string{"1"}},
		"int needle in float slice":   {1, []float64{1}},
		"scalar container":            {1, 42},
		"nil container":               {1, nil},
		"map container":               {1, map[string]int{"a": 1}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if Contain(tc.expected).Match(tc.container) {
				t.Fatalf("expected no match for %v in %v", tc.expected, tc.container)
			}
		})
	}
}

func TestBeNilMatchesNilableKinds(t *testing.T) {
	var (
		ptr   *int
		slice []int
		m     map[string]int
		fn    func()
		ch    chan int
	)
	nils := map[string]any{"untyped": nil, "pointer": ptr, "slice": slice, "map": m, "func": fn, "chan": ch}
	for name, value := range nils {
		t.Run(name+" is nil", func(t *testing.T) {
			if !BeNil().Match(value) {
				t.Fatal("expected the matcher to report nil")
			}
		})
	}

	zero := 0
	nonNils := map[string]any{"zero int": 0, "empty string": "", "pointer to zero": &zero, "empty slice": []int{}}
	for name, value := range nonNils {
		t.Run(name+" is not nil", func(t *testing.T) {
			if BeNil().Match(value) {
				t.Fatal("expected the matcher to reject a non-nil value")
			}
		})
	}
}

func TestBeTrueAndBeFalseRejectNonBooleans(t *testing.T) {
	for _, value := range []any{1, "true", nil, struct{}{}} {
		if BeTrue().Match(value) {
			t.Fatalf("expected BeTrue to reject %v (%T)", value, value)
		}
		if BeFalse().Match(value) {
			t.Fatalf("expected BeFalse to reject %v (%T)", value, value)
		}
	}
}

func TestEqualComparableComparesWithoutReflection(t *testing.T) {
	if !EqualComparable(42, 42) {
		t.Fatal("expected identical ints to be equal")
	}
	if EqualComparable("a", "b") {
		t.Fatal("expected different strings to differ")
	}
}

// ValuesEqual's fast path is a type switch over every comparable builtin; only the int arm was
// reached. Each arm must both accept an equal pair and reject a different one, and it must never
// match across types.
func TestValuesEqualCoversEveryComparableBuiltin(t *testing.T) {
	cases := map[string]struct{ same, other any }{
		"bool":       {true, false},
		"string":     {"a", "b"},
		"int":        {int(1), int(2)},
		"int8":       {int8(1), int8(2)},
		"int16":      {int16(1), int16(2)},
		"int32":      {int32(1), int32(2)},
		"int64":      {int64(1), int64(2)},
		"uint":       {uint(1), uint(2)},
		"uint8":      {uint8(1), uint8(2)},
		"uint16":     {uint16(1), uint16(2)},
		"uint32":     {uint32(1), uint32(2)},
		"uint64":     {uint64(1), uint64(2)},
		"uintptr":    {uintptr(1), uintptr(2)},
		"float32":    {float32(1), float32(2)},
		"float64":    {float64(1), float64(2)},
		"complex64":  {complex64(1i), complex64(2i)},
		"complex128": {complex128(1i), complex128(2i)},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if !ValuesEqual(tc.same, tc.same) {
				t.Fatal("expected identical values to be equal")
			}
			if ValuesEqual(tc.same, tc.other) {
				t.Fatal("expected different values to differ")
			}
		})
	}
}

func TestValuesEqualTreatsNilOperandsAsEqualOnlyToNil(t *testing.T) {
	if !ValuesEqual(nil, nil) {
		t.Fatal("expected two nils to be equal")
	}
	if ValuesEqual(nil, 1) {
		t.Fatal("expected nil and 1 to differ")
	}
	if ValuesEqual(1, nil) {
		t.Fatal("expected 1 and nil to differ")
	}
}
