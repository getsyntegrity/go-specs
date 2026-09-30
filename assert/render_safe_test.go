package assert

import (
	"fmt"
	"math"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRefIdentityTellsViewsOfOneBackingArrayApartByLength(t *testing.T) {
	backing := make([]any, 3)
	short, long := reflect.ValueOf(backing[:1]), reflect.ValueOf(backing[:3])
	ks, ok1 := refIdentity(short)
	kl, ok2 := refIdentity(long)
	if !ok1 || !ok2 {
		t.Fatalf("non-empty slices must have an identity, got %v %v", ok1, ok2)
	}
	if ks == kl {
		t.Fatal("views of different length must have different identities")
	}
	again, _ := refIdentity(reflect.ValueOf(backing[:3]))
	if again != kl {
		t.Fatal("the same view must have the same identity")
	}
}

func TestRefIdentityCoversPointersAndMapsAndIgnoresNilAndEmpty(t *testing.T) {
	x := 1
	m := map[string]int{"a": 1}
	if k, ok := refIdentity(reflect.ValueOf(&x)); !ok || k.ptr == 0 {
		t.Fatalf("a pointer has an identity, got %+v %v", k, ok)
	}
	if k1, _ := refIdentity(reflect.ValueOf(m)); k1 != mustIdentity(t, reflect.ValueOf(m)) {
		t.Fatal("a map's identity is stable")
	}
	for name, v := range map[string]reflect.Value{
		"nil pointer": reflect.ValueOf((*int)(nil)),
		"nil map":     reflect.ValueOf(map[string]int(nil)),
		"nil slice":   reflect.ValueOf([]int(nil)),
		"empty slice": reflect.ValueOf([]int{}),
		"int":         reflect.ValueOf(3),
		"struct":      reflect.ValueOf(struct{ A int }{}),
	} {
		if _, ok := refIdentity(v); ok {
			t.Errorf("%s must not have an identity", name)
		}
	}
	type other map[string]int
	if mustIdentity(t, reflect.ValueOf(m)) == mustIdentity(t, reflect.ValueOf(other(m))) {
		t.Fatal("the same address under a different type is a different identity")
	}
}

func mustIdentity(t *testing.T, v reflect.Value) refKey {
	t.Helper()
	k, ok := refIdentity(v)
	if !ok {
		t.Fatalf("no identity for %v", v.Type())
	}
	return k
}

func TestRefPairOfDistinguishesOrderAndLength(t *testing.T) {
	backing := make([]any, 2)
	a, b := reflect.ValueOf(backing[:1]), reflect.ValueOf(backing[:2])
	if refPairOf(a, b) == refPairOf(b, a) {
		t.Fatal("a pair is ordered")
	}
	if refPairOf(a, a) != refPairOf(a, a) {
		t.Fatal("a pair is stable")
	}
	x, y := 1, 2
	if refPairOf(reflect.ValueOf(&x), reflect.ValueOf(&y)) == refPairOf(reflect.ValueOf(&x), reflect.ValueOf(&x)) {
		t.Fatal("different second pointers make different pairs")
	}
}

func TestSafeForFmtAcceptsOrdinaryValues(t *testing.T) {
	shared := []any{1, "x"}
	type item struct {
		Name string
		Tags []string
		Next *item
	}
	for name, v := range map[string]any{
		"scalars":        3,
		"map":            map[string]any{"a": 1, "b": []int{1, 2}},
		"shared subtree": []any{shared, shared},
		"struct":         item{Name: "a", Tags: []string{"t"}, Next: &item{Name: "b"}},
	} {
		if !safeForFmt(reflect.ValueOf(v)) {
			t.Errorf("%s must be safe for fmt", name)
		}
	}
}

func TestSafeForFmtRejectsCyclesNaNKeysAndOversizedValues(t *testing.T) {
	m := map[string]any{}
	m["self"] = m
	s := make([]any, 1)
	s[0] = s
	backing := make([]any, 2)
	long := backing[:2]
	backing[0], backing[1] = 0, long
	type node struct{ next *node }
	n := &node{}
	n.next = n
	wide := make([]any, boundedNodeBudget+1)
	huge := make([]int, boundedNodeBudget+1)
	for name, v := range map[string]any{
		"map cycle":         m,
		"slice cycle":       s,
		"long view cycle":   long,
		"pointer cycle":     n,
		"NaN key":           map[float64]int{math.NaN(): 1},
		"NaN struct key":    map[struct{ F float64 }]int{{math.NaN()}: 1},
		"wide slice":        wide,
		"scalar slice size": huge,
	} {
		if safeForFmt(reflect.ValueOf(v)) {
			t.Errorf("%s must not be safe for fmt", name)
		}
	}
	short := backing[:1]
	if !safeForFmt(reflect.ValueOf(short)) {
		t.Error("the short acyclic view of a cyclic backing array is safe")
	}
}

// --- effective limits and safe fallback (T2) ---

func allocatedBytes(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

func TestPollFallbackStopsAtItsNodeBudgetWithAMarker(t *testing.T) {
	leaf := []any{1}
	for d := 0; d < 5; d++ {
		next := make([]any, boundedMaxElems)
		for i := range next {
			next[i] = leaf
		}
		leaf = next
	}
	var out string
	start := time.Now()
	out = formatUserValue(leaf, "%v")
	if !strings.Contains(out, truncationMarker) {
		t.Fatalf("an exhausted budget must say so with %q, got %d bytes starting %.120q", truncationMarker, len(out), out)
	}
	if len(out) > 400_000 {
		t.Fatalf("output is %d bytes, want it bounded by the node budget", len(out))
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("rendering took %v", d)
	}
}

type wideStruct struct {
	F0, F1, F2, F3, F4, F5, F6, F7, F8, F9, F10, F11, F12, F13, F14, F15, F16, F17 int
}

func TestPollFallbackCapsStructFields(t *testing.T) {
	type holder struct {
		Self *holder
		W    wideStruct
	}
	h := &holder{}
	h.Self = h
	out := formatUserValue(h, "%v")
	if !strings.Contains(out, "<cycle>") || !strings.Contains(out, "... 2 more") || strings.Contains(out, "F17") {
		t.Fatalf("struct fields must be capped at %d with a count of the rest, got %q", boundedMaxElems, out)
	}
}

func TestPollFallbackOrdersNaNAndAlikeKeysDeterministically(t *testing.T) {
	first := ""
	for i := 0; i < 50; i++ {
		m := map[any]int{}
		for v := 1; v <= 5; v++ {
			m[math.NaN()] = v
		}
		m["self"] = 0
		holder := []any{m}
		holder = append(holder, holder) // a slice that contains itself forces the fallback
		out := formatUserValue(m, "%v") + formatUserValue(holder, "%v")
		if i == 0 {
			first = out
		} else if out != first {
			t.Fatalf("key order depends on map iteration:\n%s\n---\n%s", first, out)
		}
	}
}

func TestFallbackRenderersDoNotCallUserMethods(t *testing.T) {
	stringerCalls = 0
	m := map[string]any{"mood": mood(1), "opaque": opaqueT{hidden: 1}, "err": errorT{code: 2}}
	m["self"] = m
	n := map[string]any{"mood": mood(2), "opaque": opaqueT{hidden: 2}, "err": errorT{code: 3}}
	n["self"] = n
	_ = EqualFailureMessage(m, n)
	_ = formatUserValue(m, "%v")
	_ = formatUserValue(m, "%#v")
	if stringerCalls != 0 {
		t.Fatalf("the safe fallback called user String/Error methods %d times", stringerCalls)
	}
}

func TestOrdinaryDiffKeepsStringerText(t *testing.T) {
	msg := EqualFailureMessage(map[string]any{"a": mood(1)}, map[string]any{"a": mood(2)})
	if !strings.Contains(msg, `["a"]: expected mood-1, actual mood-2`) {
		t.Fatalf("ordinary values keep their String text in the diff:\n%s", msg)
	}
	if got := renderDiffValue(reflect.ValueOf(time.Second), true); got != "1s" {
		t.Fatalf("Duration renders as %q, want 1s", got)
	}
}

type mood int

var stringerCalls int

func (m mood) String() string { stringerCalls++; return "mood-" + strconv.Itoa(int(m)) }

type opaqueT struct{ hidden int }

func (o opaqueT) String() string { stringerCalls++; return "opaque" }

type errorT struct{ code int }

func (e errorT) Error() string { stringerCalls++; return "err" }

func TestDiffRendererTruncatesLongStringsBeforeQuoting(t *testing.T) {
	long := strings.Repeat("é\"", 8<<20) // 16M runes; quoting the whole thing would allocate well over 40 MB
	var out string
	n := allocatedBytes(func() { out = renderDiffValue(reflect.ValueOf(long), true) })
	if n > 1<<20 {
		t.Fatalf("rendering allocated %d bytes for a huge string, want it bounded", n)
	}
	if r := []rune(out); len(r) != diffMaxValueRunes+1 || r[len(r)-1] != '…' {
		t.Fatalf("want %d runes and an ellipsis, got %d runes: %.60q", diffMaxValueRunes, len(r), out)
	}
}

func TestDiffRendererSelectsMapEntriesWithoutSortingTheWholeMap(t *testing.T) {
	m := make(map[int]int, 300_000)
	for i := 0; i < 300_000; i++ {
		m[i] = i
	}
	var out string
	n := allocatedBytes(func() { out = renderDiffValue(reflect.ValueOf(m), true) })
	if want := "map[0: 0, 1: 1, 2: 2, 3: 3, … +299996 more]"; out != want {
		t.Fatalf("got %q, want %q", out, want)
	}
	if n > 1<<20 {
		t.Fatalf("rendering a wide map allocated %d bytes, want it bounded", n)
	}
}

func TestPollFallbackRendersWideMapsWithBoundedWork(t *testing.T) {
	m := make(map[int]any, 300_000)
	for i := 0; i < 300_000; i++ {
		m[i] = i
	}
	m[-1] = m // a cycle forces the fallback
	var out string
	n := allocatedBytes(func() { out = formatUserValue(m, "%v") })
	if !strings.Contains(out, "more}") || !strings.Contains(out, "<cycle>") {
		t.Fatalf("unexpected rendering %.200q", out)
	}
	if n > 1<<20 {
		t.Fatalf("rendering a wide map allocated %d bytes, want it bounded", n)
	}
}

func TestSmallestMapEntriesMatchesAFullSort(t *testing.T) {
	m := map[float64]string{}
	for i := 0; i < 100; i++ {
		m[float64((i*37)%101)-50] = strconv.Itoa(i)
	}
	m[math.NaN()] = "nan"
	top, total := smallestMapEntries(reflect.ValueOf(m), 5)
	want := rangeMapEntries(reflect.ValueOf(m), true, nil)
	sort.SliceStable(want, func(i, j int) bool { return compareMapEntries(&want[i], &want[j]) < 0 })
	if total != len(want) || len(top) != 5 {
		t.Fatalf("total=%d top=%d, want %d and 5", total, len(top), len(want))
	}
	for i := range top {
		if compareMapEntries(&top[i], &want[i]) != 0 {
			t.Fatalf("entry %d differs from the sorted map", i)
		}
	}
}

func TestDiffRendererStopsAtItsNodeBudgetWithAMarker(t *testing.T) {
	type leafT struct{ A, B, C, D int }
	v := []leafT{{}, {}, {}, {}}
	got := renderDiffLimited(reflect.ValueOf(v), renderLimits{maxDepth: 3, maxElems: 4, nodeBudget: 6}, true)
	if !strings.Contains(got, truncationMarker) {
		t.Fatalf("an exhausted budget must say so with %q, got %q", truncationMarker, got)
	}
	full := renderDiffLimited(reflect.ValueOf(v), diffRenderLimits, true)
	if strings.Contains(full, truncationMarker) {
		t.Fatalf("the default budget must not truncate an ordinary value: %q", full)
	}
}

// The method-free leaf text must equal what fmt prints for a value without methods, so switching a
// value to the safe fallback never changes how a plain scalar reads.
func TestDiffLeafWithoutMethodsMatchesFmt(t *testing.T) {
	for _, v := range []any{true, 7, int8(-3), uint16(9), uintptr(12), 1.5, 1e21, 1e-7, 100000000.0, float32(0.1),
		complex(1, -2), complex64(complex(0.5, 3)), math.NaN(), math.Inf(-1)} {
		got := renderDiffValue(reflect.ValueOf(v), false)
		if want := fmt.Sprintf("%v", v); got != want {
			t.Errorf("%T(%v): got %q, want %q", v, v, got, want)
		}
	}
}

// A map reached through an unexported field cannot be loaded through Set, so the window falls back
// to copying entries; it must still render the same smallest entries.
func TestDiffRendererSelectsEntriesOfAMapBehindAnUnexportedField(t *testing.T) {
	type holder struct{ m map[int]int }
	h := holder{m: map[int]int{}}
	for i := 9; i >= 0; i-- {
		h.m[i] = i * 10
	}
	out := renderDiffValue(reflect.ValueOf(h).Field(0), true)
	if want := "map[0: 0, 1: 10, 2: 20, 3: 30, … +6 more]"; out != want {
		t.Fatalf("got %q, want %q", out, want)
	}
}
