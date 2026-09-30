package assert

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Structural diffs are built only after a failed comparison, from EqualFailureMessage. These tests
// pin the exact wording of each diff line, because the line is what a user reads to find the
// mismatch in a large value. Every value is in memory: no I/O, no clocks, no sleeps.

type diffItem struct {
	SKU   string
	Price float64
}

type diffOrder struct {
	ID    int
	Items []diffItem
	Meta  map[string]string
	Note  *string
}

type diffSecret struct {
	Public int
	secret int
}

type diffNode struct {
	Name string
	Next *diffNode
}

type diffBox struct{ V any }

type diffEvent struct{ At time.Time }

// diffLines returns the lines after the header, so tests assert on the diff itself.
func diffLines(t *testing.T, msg string) []string {
	t.Helper()
	lines := strings.Split(msg, "\n")
	if len(lines) < 2 || lines[1] != "differences:" {
		t.Fatalf("message has no differences section:\n%s", msg)
	}
	return lines[2:]
}

func wantLine(t *testing.T, msg, line string) {
	t.Helper()
	for _, l := range strings.Split(msg, "\n") {
		if l == line {
			return
		}
	}
	t.Fatalf("missing line %q in:\n%s", line, msg)
}

func TestStructuralDiffReportsNestedFieldPath(t *testing.T) {
	expected := diffOrder{ID: 1, Items: []diffItem{{"a", 1}, {"b", 2}, {"c", 9.99}}}
	actual := diffOrder{ID: 1, Items: []diffItem{{"a", 1}, {"b", 2}, {"c", 12.5}}}

	msg := Equal(expected).FailureMessage(actual)

	if !strings.HasPrefix(msg, "expected {1 [{a 1} {b 2} {c 12.5}] map[] <nil>} to equal {1 [{a 1} {b 2} {c 9.99}] map[] <nil>}\ndifferences:\n") {
		t.Fatalf("header or section changed:\n%s", msg)
	}
	got := diffLines(t, msg)
	want := []string{"  diffOrder.Items[2].Price: expected 9.99, actual 12.5"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestStructuralDiffReportsMissingAndUnexpectedMapKeys(t *testing.T) {
	expected := map[string]string{"a": "1", "b": "2"}
	actual := map[string]string{"a": "1", "c": "3"}

	msg := EqualFailureMessage(expected, actual)

	wantLine(t, msg, `  ["b"]: missing in actual (expected "2")`)
	wantLine(t, msg, `  ["c"]: unexpected in actual ("3")`)
}

func TestStructuralDiffReportsMissingAndUnexpectedSliceElements(t *testing.T) {
	msg := EqualFailureMessage([]int{1, 2, 3}, []int{1, 2})
	wantLine(t, msg, "  <root>: length: expected 3, actual 2")
	wantLine(t, msg, "  [2]: missing in actual (expected 3)")

	msg = EqualFailureMessage([]int{1}, []int{1, 7})
	wantLine(t, msg, "  <root>: length: expected 1, actual 2")
	wantLine(t, msg, "  [1]: unexpected in actual (7)")
}

func TestStructuralDiffDistinguishesNilFromEmpty(t *testing.T) {
	msg := EqualFailureMessage(diffOrder{Items: nil}, diffOrder{Items: []diffItem{}})
	wantLine(t, msg, "  diffOrder.Items: expected nil slice, actual empty non-nil slice")

	msg = EqualFailureMessage(diffOrder{Meta: map[string]string{}}, diffOrder{Meta: nil})
	wantLine(t, msg, "  diffOrder.Meta: expected empty non-nil map, actual nil map")

	msg = EqualFailureMessage(diffOrder{Items: []diffItem{{"a", 1}}}, diffOrder{Items: nil})
	wantLine(t, msg, `  diffOrder.Items: expected non-nil slice [{SKU: "a", Price: 1}], actual nil slice`)
}

func TestStructuralDiffReportsNilPointerDifferences(t *testing.T) {
	note := "x"
	msg := EqualFailureMessage(diffOrder{Note: nil}, diffOrder{Note: &note})
	wantLine(t, msg, `  diffOrder.Note: expected nil, actual &"x"`)

	msg = EqualFailureMessage(diffOrder{Note: &note}, diffOrder{Note: nil})
	wantLine(t, msg, `  diffOrder.Note: expected &"x", actual nil`)

	other := "y"
	msg = EqualFailureMessage(diffOrder{Note: &note}, diffOrder{Note: &other})
	wantLine(t, msg, `  diffOrder.Note: expected "x", actual "y"`)
}

func TestStructuralDiffTerminatesOnPointerCycles(t *testing.T) {
	expected := &diffNode{Name: "b"}
	expected.Next = expected
	actual := &diffNode{Name: "a"}
	actual.Next = actual

	msg := Equal(expected).FailureMessage(actual)

	wantLine(t, msg, `  diffNode.Name: expected "b", actual "a"`)
}

func TestStructuralDiffTerminatesOnSelfContainingMaps(t *testing.T) {
	expected := map[string]any{"n": 2}
	expected["self"] = expected
	actual := map[string]any{"n": 1}
	actual["self"] = actual

	msg := EqualFailureMessage(expected, actual)

	wantLine(t, msg, `  ["n"]: expected 2, actual 1`)
}

func TestStructuralDiffReportsTypeMismatch(t *testing.T) {
	msg := EqualFailureMessage([]int{1}, [1]int{1})
	wantLine(t, msg, "  <root>: type mismatch: expected []int, actual [1]int")

	msg = EqualFailureMessage(diffBox{V: 1}, diffBox{V: "1"})
	wantLine(t, msg, `  diffBox.V: type mismatch: expected int (1), actual string ("1")`)

	msg = EqualFailureMessage(diffBox{V: nil}, diffBox{V: 1})
	wantLine(t, msg, "  diffBox.V: expected nil, actual 1")
}

func TestStructuralDiffMapOutputIsStableAndSorted(t *testing.T) {
	expected, actual := map[string]int{}, map[string]int{}
	for i := 0; i < 8; i++ {
		k := fmt.Sprintf("k%d", i)
		expected[k], actual[k] = i, i+100
	}

	first := EqualFailureMessage(expected, actual)
	for i := 0; i < 25; i++ {
		if again := EqualFailureMessage(expected, actual); again != first {
			t.Fatalf("map diff is not deterministic:\n%s\n---\n%s", first, again)
		}
	}
	got := diffLines(t, first)
	if len(got) != 8 || got[0] != `  ["k0"]: expected 0, actual 100` || got[7] != `  ["k7"]: expected 7, actual 107` {
		t.Fatalf("keys are not in sorted order:\n%s", first)
	}
}

func TestStructuralDiffReadsUnexportedFieldsSafely(t *testing.T) {
	msg := EqualFailureMessage(diffSecret{Public: 1, secret: 1}, diffSecret{Public: 1, secret: 2})
	wantLine(t, msg, "  diffSecret.secret: expected 1, actual 2")
}

func TestStructuralDiffTreatsStringerStructsWithHiddenFieldsAsValues(t *testing.T) {
	a := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	b := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)

	msg := EqualFailureMessage(diffEvent{At: a}, diffEvent{At: b})

	wantLine(t, msg, "  diffEvent.At: expected 2020-01-01 00:00:00 +0000 UTC, actual 2021-01-01 00:00:00 +0000 UTC")
	if strings.Contains(msg, "wall") || strings.Contains(msg, "ext") {
		t.Fatalf("time.Time internals leaked into the diff:\n%s", msg)
	}
}

func TestStructuralDiffBoundsEntryCountAndSaysSo(t *testing.T) {
	expected, actual := make([]int, 30), make([]int, 30)
	for i := range actual {
		actual[i] = i + 1
	}

	msg := EqualFailureMessage(expected, actual)

	got := diffLines(t, msg)
	if len(got) != diffMaxEntries+1 {
		t.Fatalf("want %d entries plus a truncation note, got %d:\n%s", diffMaxEntries, len(got), msg)
	}
	if got[diffMaxEntries] != fmt.Sprintf("  ... more differences not shown (limit %d)", diffMaxEntries) {
		t.Fatalf("missing truncation note:\n%s", msg)
	}
	if got[0] != "  [0]: expected 0, actual 1" {
		t.Fatalf("first entry: %q", got[0])
	}
}

func TestStructuralDiffBoundsDepthAndSaysSo(t *testing.T) {
	deep := func(leaf int) any {
		var v any = leaf
		for i := 0; i < diffMaxDepth+3; i++ {
			v = map[string]any{"n": v}
		}
		return v
	}

	msg := EqualFailureMessage(deep(1), deep(2))

	path := strings.Repeat(`["n"]`, diffMaxDepth)
	wantLine(t, msg, fmt.Sprintf("  %s: differs below this point (depth limit %d reached)", path, diffMaxDepth))
}

func TestStructuralDiffDoesNotReportDifferencesBelowTheDepthLimitWhenEqual(t *testing.T) {
	build := func(leaf int) any {
		var v any = map[string]any{"same": []int{1}, "leaf": leaf}
		for i := 0; i < diffMaxDepth+3; i++ {
			v = map[string]any{"n": v}
		}
		return v
	}
	// Only "leaf" differs; the equal "same" subtree must not produce an entry of its own.
	msg := EqualFailureMessage(build(1), build(2))
	for _, l := range diffLines(t, msg) {
		if strings.Contains(l, "same") {
			t.Fatalf("equal subtree reported: %s", l)
		}
	}
}

func TestStructuralDiffTruncatesLongValues(t *testing.T) {
	long := strings.Repeat("x", 500)
	msg := EqualFailureMessage(diffBox{V: long}, diffBox{V: "short"})

	for _, l := range diffLines(t, msg) {
		if len([]rune(l)) > 200 {
			t.Fatalf("diff line is not bounded (%d runes): %s", len([]rune(l)), l)
		}
	}
	if !strings.Contains(msg, "…") {
		t.Fatalf("truncation is not marked:\n%s", msg)
	}
}

func TestStructuralDiffIsAbsentWhenItAddsNothing(t *testing.T) {
	tests := map[string]struct{ expected, actual any }{
		"scalars":       {43, 42},
		"strings":       {"a", "b"},
		"type mismatch": {1, "1"},
		"nil":           {nil, 1},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if msg := EqualFailureMessage(tc.expected, tc.actual); strings.Contains(msg, "differences:") {
				t.Fatalf("unexpected diff for scalars:\n%s", msg)
			}
		})
	}
}

func TestStructuralDiffLeavesErrorIdentityDiagnosticsAlone(t *testing.T) {
	sentinel := errors.New("boom")
	impostor := errors.New("boom")
	want := "expected error boom (*errors.errorString) to match boom (*errors.errorString) — errors.Is(actual, expected) is false"

	if got := EqualFailureMessage(sentinel, impostor); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if strings.Contains(NotEqual(sentinel).FailureMessage(sentinel), "differences:") {
		t.Fatal("NotEqual must not carry a diff")
	}
}

// The diff only explains a verdict that was already reached. It must never decide one.
func TestStructuralDiffDoesNotChangeEqualityVerdicts(t *testing.T) {
	note, same := "x", "x"
	wrapped := fmt.Errorf("ctx: %w", errors.New("inner"))
	tests := map[string]struct{ expected, actual any }{
		"equal structs":       {diffOrder{ID: 1}, diffOrder{ID: 1}},
		"different structs":   {diffOrder{ID: 1}, diffOrder{ID: 2}},
		"nil vs empty slice":  {[]int(nil), []int{}},
		"equal via pointers":  {diffOrder{Note: &note}, diffOrder{Note: &same}},
		"type mismatch":       {[]int{1}, [1]int{1}},
		"errors wrapped":      {wrapped, wrapped},
		"error vs non-error":  {errors.New("a"), "a"},
		"map equal":           {map[string]int{"a": 1}, map[string]int{"a": 1}},
		"map different value": {map[string]int{"a": 1}, map[string]int{"a": 2}},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			before := ValuesEqual(tc.expected, tc.actual)
			m := Equal(tc.expected)
			matched := m.Match(tc.actual)
			if !matched {
				_ = m.FailureMessage(tc.actual)
			}
			if after := ValuesEqual(tc.expected, tc.actual); before != after || before != matched {
				t.Fatalf("verdict changed: before=%v after=%v match=%v", before, after, matched)
			}
			if matched != (func() bool {
				if _, _, isErr := errorOperands(tc.expected, tc.actual); isErr {
					return errorsMatch(tc.expected.(error), tc.actual.(error))
				}
				return reflect.DeepEqual(tc.expected, tc.actual)
			})() {
				t.Fatal("verdict diverged from the documented semantics")
			}
		})
	}
}

// A diff line is produced for exactly the values reflect.DeepEqual calls unequal, and for nothing
// it calls equal.
func TestStructuralDiffAgreesWithDeepEqual(t *testing.T) {
	note, same := "x", "x"
	pairs := [][2]any{
		{diffOrder{ID: 1}, diffOrder{ID: 1}},
		{diffOrder{Note: &note}, diffOrder{Note: &same}},
		{diffOrder{Items: []diffItem{}}, diffOrder{Items: nil}},
		{map[string][]int{"a": {1}}, map[string][]int{"a": {1}}},
		{map[string][]int{"a": {1}}, map[string][]int{"a": {2}}},
		{[3]int{1, 2, 3}, [3]int{1, 2, 4}},
		{diffSecret{1, 1}, diffSecret{1, 2}},
	}
	for i, p := range pairs {
		diff := structuralDiff(p[0], p[1])
		if equal := reflect.DeepEqual(p[0], p[1]); equal != (diff == "") {
			t.Fatalf("pair %d: DeepEqual=%v but diff=%q", i, equal, diff)
		}
	}
}
