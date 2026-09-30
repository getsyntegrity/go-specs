package assert

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"reflect"
	"strings"
	"testing"
)

// Entries of a map whose keys tie (NaN keys with the same bit pattern) are ordered by their values.
// These tests pin the policy that makes that order deterministic and bounded: a fingerprint of each
// value computed once, a pure comparator over fingerprints, and an explicit ambiguity group for
// entries that still cannot be told apart within the fingerprint budget.

// bigSlice is longer than any fingerprint budget; only its last element is given by tail, so two
// bigSlices with different tails agree on everything the budget can see.
func bigSlice(tail int) []int {
	s := make([]int, 20_000)
	for i := range s {
		s[i] = i % 7
	}
	s[len(s)-1] = tail
	return s
}

// tailValue differs from its siblings only in Tag, which comes after Big: past any fingerprint budget,
// yet inside what a rendered line shows.
type tailValue struct {
	Big []int
	Tag string
}

func tailValues(tags ...string) []any {
	out := make([]any, len(tags))
	for i, tag := range tags {
		out[i] = tailValue{Big: bigSlice(0), Tag: tag}
	}
	return out
}

func ifaceOf(v any) reflect.Value { return reflect.ValueOf(&v).Elem() }

// nanTied builds a map whose entries all have the same NaN key and the given values, inserted in
// the given order.
func nanTied(order []int, vals []any) map[float64]any {
	m := map[float64]any{}
	for _, i := range order {
		m[math.NaN()] = vals[i]
	}
	return m
}

func TestTiedEntriesBeyondTheBudgetAreSummarisedNotOrdinalled(t *testing.T) {
	build := func(seed int64, tag string) map[float64]any {
		order := rand.New(rand.NewSource(seed)).Perm(3)
		return nanTied(order, tailValues(tag+"1", tag+"2", tag+"3"))
	}
	var first string
	for i := 0; i < 50; i++ {
		msg := EqualFailureMessage(build(int64(i), "e"), build(int64(i)+7, "a"))
		if i == 0 {
			first = msg
			if !strings.Contains(msg, "6 entries indistinguishable within the diagnostic budget: 3 missing in actual, 3 unexpected in actual") {
				t.Fatalf("expected one explicit ambiguity line, got:\n%s", msg)
			}
			if strings.Contains(msg, "#") {
				t.Fatalf("an ambiguity group must not carry per-entry ordinals:\n%s", msg)
			}
			if got := len(diffLines(t, msg)); got != 1 {
				t.Fatalf("want a single summary line, got %d:\n%s", got, msg)
			}
			continue
		}
		if msg != first {
			t.Fatalf("diagnostic depends on map iteration:\n%s\n---\n%s", first, msg)
		}
	}
}

func TestAmbiguityGroupOnOneSideOnly(t *testing.T) {
	a := nanTied([]int{0, 1, 2}, tailValues("1", "2", "3"))
	msg := EqualFailureMessage(a, map[float64]any{})
	if !strings.Contains(msg, "all 3 missing in actual") || !strings.Contains(msg, "indistinguishable") {
		t.Fatalf("missing one-sided summary:\n%s", msg)
	}
	msg = EqualFailureMessage(map[float64]any{}, a)
	if !strings.Contains(msg, "all 3 unexpected in actual") {
		t.Fatalf("missing one-sided summary:\n%s", msg)
	}
}

func TestDistinguishableTiedEntriesKeepValuesAndStableOrdinals(t *testing.T) {
	// Three entries share one NaN key, two are told apart by an early difference and three only past
	// the budget: the ordinals of the units (two entries, one group) must not move between builds.
	vals := append([]any{"x", "y"}, tailValues("1", "2", "3")...)
	var first string
	for i := 0; i < 50; i++ {
		order := rand.New(rand.NewSource(int64(i))).Perm(len(vals))
		msg := EqualFailureMessage(nanTied(order, vals), map[float64]any{})
		if i == 0 {
			first = msg
			for _, want := range []string{`(expected "x")`, `(expected "y")`, "3 entries indistinguishable", "all 3 missing in actual"} {
				if !strings.Contains(msg, want) {
					t.Fatalf("missing %q in:\n%s", want, msg)
				}
			}
			continue
		}
		if msg != first {
			t.Fatalf("diagnostic depends on insertion order:\n%s\n---\n%s", first, msg)
		}
	}
}

func TestThreeTiedEntriesGiveTheSameTextForEveryInsertionOrder(t *testing.T) {
	vals := []any{"c", "a", "b"}
	orders := [][]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}}
	var first string
	for round := 0; round < 10; round++ {
		for oi, order := range orders {
			msg := EqualFailureMessage(nanTied(order, vals), nanTied(order, []any{"d", "e", "f"}))
			if round == 0 && oi == 0 {
				first = msg
				continue
			}
			if msg != first {
				t.Fatalf("order %v round %d changed the text:\n%s\n---\n%s", order, round, first, msg)
			}
		}
	}
}

func TestOrdinaryMapDiffTextIsPinned(t *testing.T) {
	msg := EqualFailureMessage(map[string]any{"k": []int{1, 2}, "z": map[string]int{"q": 1}}, map[string]any{"k": []int{1, 3}, "y": nil})
	want := "expected map[k:[1 3] y:<nil>] to equal map[k:[1 2] z:map[q:1]]\n" +
		"differences:\n" +
		"  [\"k\"][1]: expected 2, actual 3\n" +
		"  [\"z\"]: missing in actual (expected map[\"q\": 1])\n" +
		"  [\"y\"]: unexpected in actual (nil)"
	if msg != want {
		t.Fatalf("ordinary map text changed:\n%s\n--- want\n%s", msg, want)
	}
	msg = EqualFailureMessage(map[string]int{"a": 1, "b": 2}, map[string]int{"a": 1, "b": 3, "c": 4})
	want = "expected map[a:1 b:3 c:4] to equal map[a:1 b:2]\n" +
		"differences:\n" +
		"  [\"b\"]: expected 2, actual 3\n" +
		"  [\"c\"]: unexpected in actual (4)"
	if msg != want {
		t.Fatalf("ordinary map text changed:\n%s\n--- want\n%s", msg, want)
	}
}

func TestVerdictsOfAmbiguousMapsAreUnchanged(t *testing.T) {
	a := nanTied([]int{0, 1}, tailValues("1", "2"))
	b := nanTied([]int{1, 0}, tailValues("1", "2"))
	if ValuesEqual(a, b) || Equal(a).Match(b) || reflect.DeepEqual(a, b) {
		t.Fatal("maps with NaN keys stay unequal, whatever their values")
	}
	if !ValuesEqual(a, a) || !Equal(a).Match(a) {
		t.Fatal("a map still equals itself")
	}
	big1, big2 := map[string][]int{"k": bigSlice(1)}, map[string][]int{"k": bigSlice(1)}
	if !ValuesEqual(big1, big2) {
		t.Fatal("equal maps with huge values must still pass")
	}
	if ValuesEqual(big1, map[string][]int{"k": bigSlice(2)}) {
		t.Fatal("maps differing only in a huge value must still fail")
	}
}

// The rendered text of a map whose window contains tied entries must not depend on which of them the
// runtime iterates first: an entry that cannot be told apart from another has its value hidden.
func TestRenderersHideValuesOfIndistinguishableEntries(t *testing.T) {
	var firstDiff, firstPoll string
	for i := 0; i < 50; i++ {
		order := rand.New(rand.NewSource(int64(i))).Perm(3)
		m := nanTied(order, tailValues("1", "2", "3"))
		d := renderDiffValue(reflect.ValueOf(m), true)
		p := renderFallback(reflect.ValueOf(m))
		if i == 0 {
			firstDiff, firstPoll = d, p
			if !strings.Contains(d, ambiguousValueMarker) || !strings.Contains(p, ambiguousValueMarker) {
				t.Fatalf("ambiguous entries must be marked:\n%s\n%s", d, p)
			}
			continue
		}
		if d != firstDiff || p != firstPoll {
			t.Fatalf("rendering depends on iteration order:\n%s\n%s\n---\n%s\n%s", firstDiff, firstPoll, d, p)
		}
	}
}

func TestRenderersKeepValuesOfDistinguishableTiedEntries(t *testing.T) {
	m := nanTied([]int{0, 1}, []any{"x", "y"})
	for _, got := range []string{renderDiffValue(reflect.ValueOf(m), true), renderFallback(reflect.ValueOf(m))} {
		if !strings.Contains(got, `"x"`) || !strings.Contains(got, `"y"`) || strings.Contains(got, ambiguousValueMarker) {
			t.Fatalf("distinguishable tied entries must keep their values: %s", got)
		}
	}
}

type tiedNode struct {
	V    int
	Next *tiedNode
}

type tiedStruct struct {
	A int
	B string
	c []float64
}

// tiedCorpus is a set of values, all wrapped in interfaces so different dynamic types compare.
func tiedCorpus() []reflect.Value {
	nanA := math.Float64frombits(0x7ff8000000000001)
	nanB := math.Float64frombits(0x7ff8000000000002)
	negZero := math.Copysign(0, -1)
	nestedNaN := func(a, b int) map[float64]int {
		m := map[float64]int{}
		m[math.NaN()] = a
		m[math.NaN()] = b
		return m
	}
	nanBig := func(at int, marks ...int) map[float64][]int {
		m := map[float64][]int{}
		for _, mark := range marks {
			s := make([]int, 4000)
			s[at] = mark
			m[math.NaN()] = s
		}
		return m
	}
	chain := func(n, last int) *tiedNode {
		head := &tiedNode{V: last}
		for i := 0; i < n; i++ {
			head = &tiedNode{V: i, Next: head}
		}
		return head
	}
	vals := []any{
		nil, true, false, 0, 1, -1, int8(1), int64(1), uint(1), uintptr(1), "", "a", "b", "a\x00b", "a\x00",
		0.0, negZero, 1.5, math.Inf(1), math.Inf(-1), math.NaN(), nanA, nanB, float32(1.5), float32(math.NaN()),
		complex(1, 2), complex(1, math.NaN()),
		[2]int{1, 2}, [2]int{1, 3}, [3]int{1, 2, 3}, [0]int{}, [2]string{"a", "b"},
		[]int(nil), []int{}, []int{1}, []int{1, 2}, []int{1, 3}, []string{"1"},
		tiedStruct{A: 1, B: "x", c: []float64{1}}, tiedStruct{A: 1, B: "x", c: []float64{2}}, tiedStruct{},
		map[string]int(nil), map[string]int{}, map[string]int{"a": 1}, map[string]int{"a": 2}, map[string]int{"b": 1},
		nestedNaN(1, 2), nestedNaN(2, 1), nestedNaN(1, 3),
		nanBig(3999, 1, 2, 3, 4), nanBig(3999, 1, 2, 3, 5), nanBig(0, 1, 2, 3, 4), nanBig(0, 4, 3, 2, 1),
		tailValues("a")[0], tailValues("b")[0],
		map[string]any{"k": []any{1, "x"}, "j": map[string]any{"z": nil}}, map[string]any{"j": map[string]any{"z": nil}, "k": []any{1, "x"}},
		(*int)(nil), new(int), func() *int { i := 7; return &i }(),
		chain(3, 1), chain(3, 2), chain(4, 1),
		bigSlice(1), bigSlice(2), bigSlice(1),
		[]any{1, "a", nil}, []any{1, "a"}, []any{[]any{}},
		(func())(nil), func() {}, (chan int)(nil), make(chan int),
		&tiedStruct{A: 1}, &tiedStruct{A: 2},
	}
	out := make([]reflect.Value, len(vals))
	for i, v := range vals {
		out[i] = ifaceOf(v)
	}
	return out
}

func sign(c int) int {
	switch {
	case c < 0:
		return -1
	case c > 0:
		return 1
	}
	return 0
}

// checkComparatorProperties fails unless compareTiedValues is a consistent strict weak order over
// the corpus: repeatable, independent of earlier comparisons, antisymmetric and transitive.
func checkComparatorProperties(t *testing.T, corpus []reflect.Value) {
	t.Helper()
	n := len(corpus)
	first := make([][]int, n)
	for i := range corpus {
		first[i] = make([]int, n)
		for j := range corpus {
			first[i][j] = sign(compareTiedValues(corpus[i], corpus[j]))
		}
	}
	// Consistency: the same pair again, in a shuffled order that interleaves every other pair.
	rng := rand.New(rand.NewSource(1))
	for _, k := range rng.Perm(n * n) {
		i, j := k/n, k%n
		if got := sign(compareTiedValues(corpus[i], corpus[j])); got != first[i][j] {
			t.Fatalf("comparison (%d,%d) changed after other comparisons: %d then %d", i, j, first[i][j], got)
		}
	}
	for i := 0; i < n; i++ {
		if first[i][i] != 0 {
			t.Fatalf("value %d does not tie with itself", i)
		}
		for j := 0; j < n; j++ {
			if first[i][j] != -first[j][i] {
				t.Fatalf("antisymmetry broken for (%d,%d): %d vs %d", i, j, first[i][j], first[j][i])
			}
		}
	}
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			for k := 0; k < n; k++ {
				if first[i][j] <= 0 && first[j][k] <= 0 && first[i][k] > 0 {
					t.Fatalf("transitivity (<=) broken for (%d,%d,%d)", i, j, k)
				}
				if first[i][j] < 0 && first[j][k] <= 0 && first[i][k] >= 0 ||
					first[i][j] <= 0 && first[j][k] < 0 && first[i][k] >= 0 {
					t.Fatalf("transitivity (<) broken for (%d,%d,%d)", i, j, k)
				}
			}
		}
	}
}

func TestCompareTiedValuesIsAConsistentStrictWeakOrder(t *testing.T) {
	checkComparatorProperties(t, tiedCorpus())
}

func TestCompareTiedValuesSeparatesWhatItCanSee(t *testing.T) {
	distinct := [][2]any{
		{1, 2}, {1, int8(1)}, {"a", "b"}, {[]int{1}, []int{1, 2}}, {[2]int{1, 2}, [3]int{1, 2, 3}},
		{0.0, math.Copysign(0, -1)}, {math.Float64frombits(0x7ff8000000000001), math.Float64frombits(0x7ff8000000000002)},
		{map[string]int{"a": 1}, map[string]int{"a": 2}}, {[]int(nil), []int{}}, {nil, 0},
		{&tiedNode{V: 1}, &tiedNode{V: 2}}, {tiedStruct{A: 1}, tiedStruct{A: 2}},
	}
	for _, p := range distinct {
		a, b := ifaceOf(p[0]), ifaceOf(p[1])
		if compareTiedValues(a, b) == 0 {
			t.Errorf("%#v and %#v must not tie", p[0], p[1])
		}
	}
	same := [][2]any{
		{1, 1}, {"a", "a"}, {&tiedNode{V: 1}, &tiedNode{V: 1}}, {map[string]int{"a": 1}, map[string]int{"a": 1}},
		{math.NaN(), math.NaN()}, {[]int(nil), []int(nil)},
	}
	for _, p := range same {
		fa, fb := newValueFingerprint(ifaceOf(p[0])), newValueFingerprint(ifaceOf(p[1]))
		if compareFingerprints(fa, fb) != 0 || !fa.complete || !fb.complete {
			t.Errorf("%#v and %#v are identical and small: they must tie as complete", p[0], p[1])
		}
	}
}

func TestFingerprintNeverCallsItEqualBecauseTheBudgetRanOut(t *testing.T) {
	a, b := newValueFingerprint(ifaceOf(bigSlice(1))), newValueFingerprint(ifaceOf(bigSlice(2)))
	if a.complete || b.complete {
		t.Fatal("a value beyond the budget must be marked incomplete")
	}
	if compareFingerprints(a, b) != 0 || !fingerprintsAmbiguous(a, b) {
		t.Fatal("values equal on everything the budget saw are ambiguous, not equal")
	}
	small := newValueFingerprint(ifaceOf(1))
	if fingerprintsAmbiguous(small, small) {
		t.Fatal("complete equal fingerprints are identical values, not ambiguous")
	}
	if compareFingerprints(small, a) == 0 {
		t.Fatal("a complete and a truncated fingerprint never tie")
	}
}

func TestFingerprintOfMapsIgnoresIterationOrder(t *testing.T) {
	var first *valueFingerprint
	for i := 0; i < 50; i++ {
		m := map[string]any{}
		for _, k := range rand.New(rand.NewSource(int64(i))).Perm(40) {
			m[fmt.Sprint("k", k)] = map[float64]any{math.NaN(): k, 1: []int{k}}
		}
		nested := map[float64]int{}
		for _, k := range rand.New(rand.NewSource(int64(i))).Perm(5) {
			nested[math.NaN()] = k
		}
		fp := newValueFingerprint(ifaceOf([]any{m, nested}))
		if first == nil {
			first = fp
			continue
		}
		if compareFingerprints(first, fp) != 0 || string(first.data) != string(fp.data) {
			t.Fatalf("fingerprint depends on iteration order (round %d)", i)
		}
	}
}

func TestFingerprintRespectsItsLimits(t *testing.T) {
	deep := &tiedNode{}
	for i := 0; i < 200_000; i++ {
		deep = &tiedNode{V: i, Next: deep}
	}
	nested := map[string]any{}
	cur := nested
	for i := 0; i < 50_000; i++ {
		next := map[string]any{}
		cur["n"] = next
		cur = next
	}
	wide := map[int]int{}
	for i := 0; i < 5_000; i++ {
		wide[i] = i
	}
	long := strings.Repeat("s", 1<<20)
	for name, v := range map[string]any{
		"deep pointer chain": deep, "deep nested map": nested, "wide map": wide, "huge slice": make([]int, 3_000_000),
		"huge string": long, "slice of huge strings": []string{long, long},
	} {
		fp := newValueFingerprint(ifaceOf(v))
		if len(fp.data) > fingerprintMaxBytes {
			t.Errorf("%s: fingerprint has %d bytes, limit %d", name, len(fp.data), fingerprintMaxBytes)
		}
		if fp.complete {
			t.Errorf("%s: must be marked incomplete", name)
		}
	}
	if fp := newValueFingerprint(ifaceOf([]int{1, 2, 3})); !fp.complete {
		t.Error("a small value is complete")
	}
}

func TestFingerprintIsAllocationBoundedOnHugeValues(t *testing.T) {
	v := ifaceOf(make([]int, 5_000_000))
	if n := testing.AllocsPerRun(3, func() { _ = newValueFingerprint(v) }); n > 16 {
		t.Fatalf("fingerprinting a huge slice made %v allocations", n)
	}
}

const tiedCycleEnv = "GO_SPECS_TIED_CYCLE_CHILD"

func cyclicTiedValues() []any {
	p1, p2 := &tiedNode{V: 1}, &tiedNode{V: 1}
	p1.Next, p2.Next = p1, p2
	p3 := &tiedNode{V: 1}
	p3.Next = &tiedNode{V: 2, Next: p3}
	s1, s2 := make([]any, 2), make([]any, 2)
	s1[0], s1[1] = s1, 1
	s2[0], s2[1] = s2, 2
	m1, m2 := map[string]any{"tag": "a"}, map[string]any{"tag": "b"}
	m1["self"], m2["self"] = m1, m2
	mixed := map[string]any{}
	mixed["slice"] = []any{mixed, &tiedNode{V: 3}}
	return []any{p1, p2, p3, s1, s2, m1, m2, mixed, []any{m1, s1, p1}}
}

func TestCyclicValuesKeepTheComparatorConsistent(t *testing.T) {
	if os.Getenv(tiedCycleEnv) == "1" {
		vals := cyclicTiedValues()
		corpus := make([]reflect.Value, len(vals))
		for i, v := range vals {
			corpus[i] = ifaceOf(v)
		}
		checkComparatorProperties(t, corpus)
		// Isomorphic cycles are identical: same back-references, complete.
		fa, fb := newValueFingerprint(corpus[0]), newValueFingerprint(corpus[1])
		if compareFingerprints(fa, fb) != 0 || !fa.complete {
			t.Error("isomorphic pointer cycles must have equal complete fingerprints")
		}
		if compareTiedValues(corpus[0], corpus[2]) == 0 {
			t.Error("cycles with different contents must differ")
		}
		// And the whole diagnostic over cyclic tied values is stable.
		var first string
		for i := 0; i < 20; i++ {
			mk := func() map[float64]any {
				out := map[float64]any{}
				for _, v := range cyclicTiedValues() {
					out[math.NaN()] = v
				}
				return out
			}
			msg := EqualFailureMessage(mk(), mk())
			if i == 0 {
				first = msg
			} else if msg != first {
				t.Errorf("cyclic diagnostic changed between builds:\n%s\n---\n%s", first, msg)
			}
		}
		fmt.Println("CYCLE-CHILD-DONE")
		return
	}
	out := runInSubprocess(t, "TestCyclicValuesKeepTheComparatorConsistent", tiedCycleEnv)
	if !strings.Contains(out, "CYCLE-CHILD-DONE") {
		t.Fatalf("child did not finish:\n%s", out)
	}
}

// A map with a very large number of tied NaN entries must still be selected in one pass with bounded
// allocation: fingerprints are built in one scratch buffer and only entries that enter the window
// keep a copy.
func TestSmallestMapEntriesStaysAllocationBoundedOnTiedKeys(t *testing.T) {
	shared := bigSlice(0)
	m := make(map[float64]tailValue, 100_000)
	for i := 0; i < 100_000; i++ {
		m[math.NaN()] = tailValue{Big: shared, Tag: fmt.Sprint(i)}
	}
	var top []mapEntry
	n := allocatedBytes(func() { top, _ = smallestMapEntries(reflect.ValueOf(m), renderMaxElems) })
	if n > 1<<20 {
		t.Fatalf("selecting from a map of tied entries allocated %d bytes, want it bounded", n)
	}
	for i, e := range top {
		if !e.ambiguous {
			t.Errorf("window entry %d has partners it cannot be told apart from and must be flagged", i)
		}
	}
	if got := renderDiffValue(reflect.ValueOf(m), true); strings.Count(got, ambiguousValueMarker) != renderMaxElems {
		t.Fatalf("want %d ambiguous values, got %s", renderMaxElems, got)
	}
}
