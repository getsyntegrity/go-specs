package assert

import (
	"errors"
	"fmt"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
	"unsafe"
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

// runInSubprocess re-executes this test binary running only the named test with envVar set, so a
// stack overflow (which no recover can catch) fails one assertion instead of killing the suite.
func runInSubprocess(t *testing.T, name, envVar string) string {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^"+name+"$")
	cmd.Env = append(os.Environ(), envVar+"=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		text := string(out)
		if len(text) > 600 {
			text = text[:600]
		}
		t.Fatalf("child failed: %v\n%s", err, text)
	}
	return string(out)
}

func TestStructuralDiffTerminatesOnSelfContainingSlices(t *testing.T) {
	const env = "GO_SPECS_SLICE_CYCLE_CHILD"
	if os.Getenv(env) == "1" {
		a, b := make([]any, 2), make([]any, 2)
		a[0], a[1] = a, 1
		b[0], b[1] = b, 2
		fmt.Println("MSG-BEGIN")
		fmt.Println(EqualFailureMessage(a, b))

		m1, m2 := map[string]any{"n": 1}, map[string]any{"n": 2}
		s1, s2 := []any{m1}, []any{m2}
		m1["s"], m2["s"] = s1, s2
		fmt.Println(EqualFailureMessage(s1, s2))
		fmt.Println("MSG-END")
		return
	}
	out := runInSubprocess(t, "TestStructuralDiffTerminatesOnSelfContainingSlices", env)
	if !strings.Contains(out, "  [1]: expected 1, actual 2") {
		t.Fatalf("slice cycle diff missing element difference:\n%s", out)
	}
	if !strings.Contains(out, `  [0]["n"]: expected 1, actual 2`) || !strings.Contains(out, "MSG-END") {
		t.Fatalf("map-in-slice cycle diff missing or child did not finish:\n%s", out)
	}
}

// Two views of one backing array share a data pointer and a type. The header's cycle check must tell
// them apart by length: registering the short, acyclic view must not hide the long view's cycle, or
// the header falls back to %v and overflows the stack.
func TestStructuralDiffHeaderDetectsCyclesInLongerViewsOfASharedBackingArray(t *testing.T) {
	const env = "GO_SPECS_SHARED_BACKING_CYCLE_CHILD"
	if os.Getenv(env) == "1" {
		backing := make([]any, 2)
		short, long := backing[:1], backing[:2]
		backing[0], backing[1] = 0, long
		fmt.Println("MSG-BEGIN")
		fmt.Println(EqualFailureMessage([]any{}, []any{short, long}))
		fmt.Println(EqualFailureMessage([]any{}, []any{long, short}))
		fmt.Println("MSG-END")
		return
	}
	out := runInSubprocess(t, "TestStructuralDiffHeaderDetectsCyclesInLongerViewsOfASharedBackingArray", env)
	if !strings.Contains(out, "MSG-END") || strings.Count(out, "to equal") != 2 {
		t.Fatalf("child did not render both failure messages:\n%s", out)
	}
}

func TestStructuralDiffOrdersKeysSharingALongPrefixByFullKey(t *testing.T) {
	prefix := strings.Repeat("k", diffMaxValueRunes*2)
	var first string
	for i := 0; i < 50; i++ {
		expected := map[string]int{prefix + "B": 2, prefix + "A": 1, prefix + "C": 3}
		actual := map[string]int{prefix + "B": 102, prefix + "A": 101, prefix + "C": 103}
		msg := EqualFailureMessage(expected, actual)
		if i == 0 {
			first = msg
			got := diffLines(t, msg)
			if len(got) != 3 || !strings.HasSuffix(got[0], "expected 1, actual 101") ||
				!strings.HasSuffix(got[1], "expected 2, actual 102") || !strings.HasSuffix(got[2], "expected 3, actual 103") {
				t.Fatalf("keys are not ordered by their full value:\n%s", msg)
			}
		} else if msg != first {
			t.Fatalf("order depends on map iteration:\n%s\n---\n%s", first, msg)
		}
	}
}

// diffWideKey has more fields than renderMaxElems, so its bounded rendering hides the last ones.
type diffWideKey struct{ A, B, C, D, E, F int }

func TestStructuralDiffOrdersStructKeysDifferingBeyondTheRenderedFields(t *testing.T) {
	k1 := diffWideKey{1, 2, 3, 4, 1, 0}
	k2 := diffWideKey{1, 2, 3, 4, 2, 0}
	k3 := diffWideKey{1, 2, 3, 4, 2, 1}
	var first string
	for i := 0; i < 50; i++ {
		expected := map[diffWideKey]int{k3: 3, k2: 2, k1: 1}
		actual := map[diffWideKey]int{k3: 103, k2: 102, k1: 101}
		msg := EqualFailureMessage(expected, actual)
		if i == 0 {
			first = msg
			got := diffLines(t, msg)
			if len(got) != 3 || !strings.HasSuffix(got[0], "expected 1, actual 101") ||
				!strings.HasSuffix(got[1], "expected 2, actual 102") || !strings.HasSuffix(got[2], "expected 3, actual 103") {
				t.Fatalf("struct keys are not ordered by all their fields:\n%s", msg)
			}
			paths := map[string]bool{}
			for _, l := range got {
				paths[strings.SplitN(l, ": expected", 2)[0]] = true
			}
			if len(paths) != 3 {
				t.Fatalf("struct key paths are not distinct:\n%s", msg)
			}
		} else if msg != first {
			t.Fatalf("order depends on map iteration:\n%s\n---\n%s", first, msg)
		}
	}
}

func TestStructuralDiffDisambiguatesLongKeysSharingAPrefix(t *testing.T) {
	prefix := strings.Repeat("k", diffMaxValueRunes*2)
	var first string
	for i := 0; i < 50; i++ {
		expected := map[string]int{prefix + "B": 2, prefix + "A": 1}
		actual := map[string]int{prefix + "B": 102, prefix + "A": 101}
		msg := EqualFailureMessage(expected, actual)
		if i == 0 {
			first = msg
			got := diffLines(t, msg)
			if len(got) != 2 || !strings.Contains(got[0], " #1]: expected 1, actual 101") ||
				!strings.Contains(got[1], " #2]: expected 2, actual 102") {
				t.Fatalf("colliding key paths carry no ordinal, in key order:\n%s", msg)
			}
		} else if msg != first {
			t.Fatalf("output depends on map iteration:\n%s\n---\n%s", first, msg)
		}
	}
}

func TestStructuralDiffDisambiguatesMissingAndUnexpectedKeysThatRenderAlike(t *testing.T) {
	prefix := strings.Repeat("k", diffMaxValueRunes*2)
	msg := EqualFailureMessage(map[string]int{prefix + "A": 1}, map[string]int{prefix + "B": 2})
	got := diffLines(t, msg)
	if len(got) != 2 || !strings.Contains(got[0], " #1]: missing in actual") ||
		!strings.Contains(got[1], " #2]: unexpected in actual") {
		t.Fatalf("missing and unexpected keys share one label:\n%s", msg)
	}
}

func TestStructuralDiffKeepsPlainKeyPathsWithoutOrdinal(t *testing.T) {
	msg := EqualFailureMessage(map[string]int{"a": 1, "b": 2}, map[string]int{"a": 9, "b": 8})
	wantLine(t, msg, `  ["a"]: expected 1, actual 9`)
	if strings.Contains(msg, "#") {
		t.Fatalf("ordinary keys must not carry an ordinal:\n%s", msg)
	}
}

func TestCompareMapKeysIsATotalOrder(t *testing.T) {
	type pair struct {
		a int
		b string
	}
	x, y := 1, 2
	nan := math.NaN()
	negZero := math.Copysign(0, -1)
	ordered := func(name string, lo, hi any) {
		t.Helper()
		l, h := reflect.ValueOf(lo), reflect.ValueOf(hi)
		if compareMapKeys(l, h) >= 0 || compareMapKeys(h, l) <= 0 {
			t.Errorf("%s: %v should order before %v", name, lo, hi)
		}
	}
	ordered("NaN before numbers", nan, -1e300)
	ordered("negative before positive float", -1.5, 2.5)
	ordered("complex real part", complex(1, 9), complex(2, 0))
	ordered("complex imag part", complex(1, 1), complex(1, 2))
	ordered("array elementwise", [3]int{1, 2, 3}, [3]int{1, 2, 4})
	ordered("struct past the rendered fields", diffWideKey{1, 2, 3, 4, 1, 0}, diffWideKey{1, 2, 3, 4, 2, 0})
	ordered("unexported struct fields", pair{1, "a"}, pair{1, "b"})
	ordered("string full value", strings.Repeat("k", 500)+"A", strings.Repeat("k", 500)+"B")
	if compareMapKeys(reflect.ValueOf(negZero), reflect.ValueOf(0.0)) != 0 {
		t.Error("-0 and +0 must compare equal")
	}
	if compareMapKeys(reflect.ValueOf(nan), reflect.ValueOf(nan)) != 0 {
		t.Error("NaN must tie with NaN")
	}
	lo, hi := &x, &y
	if uintptr(unsafe.Pointer(lo)) > uintptr(unsafe.Pointer(hi)) {
		lo, hi = hi, lo
	}
	ordered("pointers by address", lo, hi)

	iface := func(v any) reflect.Value { return reflect.ValueOf(&v).Elem() }
	nilIface := reflect.ValueOf(new(any)).Elem()
	if compareMapKeys(nilIface, iface(1)) >= 0 || compareMapKeys(iface(1), nilIface) <= 0 {
		t.Error("nil interface must order first")
	}
	if compareMapKeys(nilIface, nilIface) != 0 {
		t.Error("two nil interfaces must tie")
	}
	mixed := []any{"b", 2, "a", 1, 1.5, true, [1]int{1}, pair{1, "x"}}
	want := mixed
	for i := 0; i < 20; i++ {
		got := append([]any(nil), mixed...)
		rand.Shuffle(len(got), func(i, j int) { got[i], got[j] = got[j], got[i] })
		sort.SliceStable(got, func(i, j int) bool { return compareMapKeys(iface(got[i]), iface(got[j])) < 0 })
		if i == 0 {
			want = got
		} else if !reflect.DeepEqual(got, want) {
			t.Fatalf("mixed dynamic types order depends on input order:\n%v\n%v", want, got)
		}
	}
}

// NaN != NaN, so a NaN key can never be looked up again: MapIndex on the source map returns an invalid
// Value. The diff therefore keeps each key with its value from MapRange and never looks it up.

type nanStructKey struct {
	F float64
	N int
}

func nanDiffOf(t *testing.T, expected, actual any) string {
	t.Helper()
	msg := EqualFailureMessage(expected, actual)
	if strings.Contains(msg, "nil") || strings.Contains(msg, "invalid") {
		t.Fatalf("diff shows a missing value:\n%s", msg)
	}
	return msg
}

func TestStructuralDiffShowsValuesOfNaNKeys(t *testing.T) {
	nan := math.NaN()
	nan32 := float32(math.NaN())
	tests := map[string]struct{ expected, actual any }{
		"float64":   {map[float64]string{nan: "expected"}, map[float64]string{nan: "actual"}},
		"float32":   {map[float32]string{nan32: "expected"}, map[float32]string{nan32: "actual"}},
		"struct":    {map[nanStructKey]string{{nan, 1}: "expected"}, map[nanStructKey]string{{nan, 1}: "actual"}},
		"array":     {map[[2]float64]string{{nan, 1}: "expected"}, map[[2]float64]string{{nan, 1}: "actual"}},
		"interface": {map[any]string{nan: "expected"}, map[any]string{nan: "actual"}},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			msg := nanDiffOf(t, tc.expected, tc.actual)
			got := diffLines(t, msg)
			if len(got) != 2 ||
				!strings.Contains(got[0], `missing in actual (expected "expected")`) ||
				!strings.Contains(got[1], `unexpected in actual ("actual")`) {
				t.Fatalf("NaN key must be missing/unexpected with its real values:\n%s", msg)
			}
		})
	}
}

func TestStructuralDiffOrdersNaNKeysDeterministically(t *testing.T) {
	build := func(v1, v2 string) map[float64]string {
		m := map[float64]string{}
		m[math.NaN()] = v1
		m[math.NaN()] = v2
		m[math.Float64frombits(0x7ff8000000000123)] = "payload-" + v1
		m[math.Float64frombits(0x7ff8000000000456)] = "payload-" + v2
		m[1] = "one"
		return m
	}
	var first string
	for i := 0; i < 50; i++ {
		msg := nanDiffOf(t, build("a", "b"), build("c", "d"))
		if i == 0 {
			first = msg
			for _, want := range []string{`(expected "a")`, `(expected "b")`, `("c")`, `("d")`, "#1]", "#4]"} {
				if !strings.Contains(msg, want) {
					t.Fatalf("missing %q in:\n%s", want, msg)
				}
			}
			continue
		}
		if msg != first {
			t.Fatalf("order or ordinals depend on map iteration:\n%s\n---\n%s", first, msg)
		}
	}
}

func TestStructuralDiffKeepsValuesOfOrdinaryKeys(t *testing.T) {
	msg := EqualFailureMessage(map[string]string{"a": "x", "b": "y"}, map[string]string{"a": "x", "c": "z"})
	wantLine(t, msg, `  ["b"]: missing in actual (expected "y")`)
	wantLine(t, msg, `  ["c"]: unexpected in actual ("z")`)
}

func TestStructuralDiffDoesNotChangeVerdictsOfNaNKeyedMaps(t *testing.T) {
	nan := math.NaN()
	a, b := map[float64]int{nan: 1}, map[float64]int{nan: 1}
	if ValuesEqual(a, b) || reflect.DeepEqual(a, b) || Equal(a).Match(b) {
		t.Fatal("maps with NaN keys are unequal under reflect.DeepEqual and must stay so")
	}
	c, d := map[float64]int{1: 1, 2: 2}, map[float64]int{2: 2, 1: 1}
	if !ValuesEqual(c, d) || !Equal(c).Match(d) {
		t.Fatal("equal ordinary maps must still pass")
	}
	if structuralDiff(a, a) != "" {
		t.Fatal("a map compared with itself has no differences")
	}
}

func TestStructuralDiffTerminatesOnNaNKeysWithCyclicValues(t *testing.T) {
	const env = "GO_SPECS_NAN_CYCLE_CHILD"
	if os.Getenv(env) == "1" {
		nan := math.NaN()
		mk := func(tag string) map[float64]any {
			m1, m2 := map[string]any{"tag": tag}, map[string]any{"tag": tag + "2"}
			m1["self"], m2["self"] = m1, m2
			s := make([]any, 1)
			s[0] = s
			out := map[float64]any{}
			out[nan] = m1
			out[nan] = m2 // same NaN bits, distinct cyclic values
			out[nan] = s
			return out
		}
		fmt.Println("MSG-BEGIN")
		fmt.Println(EqualFailureMessage(mk("a"), mk("b")))
		fmt.Println("MSG-END")
		return
	}
	out := runInSubprocess(t, "TestStructuralDiffTerminatesOnNaNKeysWithCyclicValues", env)
	if !strings.Contains(out, "MSG-END") || !strings.Contains(out, "missing in actual") ||
		!strings.Contains(out, "unexpected in actual") {
		t.Fatalf("child did not finish or lacks the NaN entries:\n%s", out)
	}
}

func TestCompareMapKeysOrdersNaNsByBitPattern(t *testing.T) {
	lo, hi := math.Float64frombits(0x7ff8000000000001), math.Float64frombits(0x7ff8000000000002)
	if compareMapKeys(reflect.ValueOf(lo), reflect.ValueOf(hi)) >= 0 ||
		compareMapKeys(reflect.ValueOf(hi), reflect.ValueOf(lo)) <= 0 {
		t.Error("NaNs with different payloads must order by bit pattern")
	}
	if compareMapKeys(reflect.ValueOf(lo), reflect.ValueOf(lo)) != 0 {
		t.Error("identical NaN bit patterns tie")
	}
}
