package assert

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

// The ordinal of a map key that renders like another (`["kkkk… #1]`) is decided among the keys of
// the same map that appear in the printed lines, not among a window of the map's keys. These tests
// pin that with keys far past any fixed window and with look-alikes that are not shown.

// likeKey is the bounded label of a long string key: a quote, 79 characters of the shared prefix and
// the cut marker.
var likeKey = `"` + strings.Repeat("k", diffMaxValueRunes-1) + "…"

// likePrefix is longer than the display limit, so keys that share it collide after truncation.
var likePrefix = strings.Repeat("k", diffMaxValueRunes*2)

// equalFillers adds n keys ("a00", "a01", ...) with the same value on both sides. They sort before
// likePrefix keys and never produce a line.
func equalFillers(n int, exp, act map[string]int) {
	for i := 0; i < n; i++ {
		k := fmt.Sprintf("a%02d", i)
		exp[k], act[k] = i, i
	}
}

func TestStructuralDiffOrdinalsForCollidingKeysPastTheFortiethKey(t *testing.T) {
	exp, act := map[string]int{}, map[string]int{}
	equalFillers(40, exp, act)
	exp[likePrefix+"A"], act[likePrefix+"A"] = 1, 101
	exp[likePrefix+"B"], act[likePrefix+"B"] = 2, 102
	want := "differences:\n" +
		"  [" + likeKey + " #1]: expected 1, actual 101\n" +
		"  [" + likeKey + " #2]: expected 2, actual 102"
	msg := EqualFailureMessage(exp, act)
	if got := msg[strings.Index(msg, "differences:"):]; got != want {
		t.Fatalf("keys 41 and 42 share a label without ordinals:\n%s\nwant:\n%s", got, want)
	}
}

func TestStructuralDiffOrdinalsForOneKeyInsideAndOneOutsideTheFirstForty(t *testing.T) {
	exp, act := map[string]int{}, map[string]int{}
	equalFillers(39, exp, act)
	exp[likePrefix+"A"], act[likePrefix+"A"] = 1, 101 // 40th key
	exp[likePrefix+"B"], act[likePrefix+"B"] = 2, 102 // 41st key
	want := "differences:\n" +
		"  [" + likeKey + " #1]: expected 1, actual 101\n" +
		"  [" + likeKey + " #2]: expected 2, actual 102"
	msg := EqualFailureMessage(exp, act)
	if got := msg[strings.Index(msg, "differences:"):]; got != want {
		t.Fatalf("a colliding pair split by position 40 is not disambiguated:\n%s\nwant:\n%s", got, want)
	}
}

func TestStructuralDiffNoOrdinalWhenTheLookAlikeIsEqual(t *testing.T) {
	exp := map[string]int{likePrefix + "A": 1, likePrefix + "B": 2}
	act := map[string]int{likePrefix + "A": 101, likePrefix + "B": 2}
	msg := EqualFailureMessage(exp, act)
	want := "differences:\n  [" + likeKey + "]: expected 1, actual 101"
	if got := msg[strings.Index(msg, "differences:"):]; got != want {
		t.Fatalf("an equal look-alike must not add an ordinal:\n%s\nwant:\n%s", got, want)
	}
}

func TestStructuralDiffNoOrdinalWhenTheLookAlikeIsBeyondTheEntryLimit(t *testing.T) {
	exp, act := map[string]int{}, map[string]int{}
	for i := 0; i < diffMaxEntries-1; i++ { // nine short keys that differ
		k := fmt.Sprintf("a%02d", i)
		exp[k], act[k] = i, i+1000
	}
	exp[likePrefix+"A"], act[likePrefix+"A"] = 1, 101 // the 10th difference, shown
	exp[likePrefix+"B"], act[likePrefix+"B"] = 2, 102 // the 11th, cut off
	msg := EqualFailureMessage(exp, act)
	lines := diffLines(t, msg)
	if len(lines) != diffMaxEntries+1 || !strings.Contains(lines[diffMaxEntries], "more differences not shown (limit 10)") {
		t.Fatalf("expected 10 lines and the truncation notice:\n%s", msg)
	}
	if want := "  [" + likeKey + "]: expected 1, actual 101"; lines[diffMaxEntries-1] != want {
		t.Fatalf("a look-alike that is not shown must not add an ordinal:\n%s\nwant line: %s", msg, want)
	}
	if strings.Contains(msg, "#") {
		t.Fatalf("no ordinal expected anywhere:\n%s", msg)
	}
}

type labelLeaf struct{ X, Y int }

func TestStructuralDiffSameKeySameOrdinalOnEveryLineBelowIt(t *testing.T) {
	exp := map[string]labelLeaf{likePrefix + "A": {1, 2}, likePrefix + "B": {3, 4}}
	act := map[string]labelLeaf{likePrefix + "A": {10, 20}, likePrefix + "B": {30, 4}}
	msg := EqualFailureMessage(exp, act)
	want := "differences:\n" +
		"  [" + likeKey + " #1].X: expected 1, actual 10\n" +
		"  [" + likeKey + " #1].Y: expected 2, actual 20\n" +
		"  [" + likeKey + " #2].X: expected 3, actual 30"
	if got := msg[strings.Index(msg, "differences:"):]; got != want {
		t.Fatalf("lines below one key disagree on its ordinal:\n%s\nwant:\n%s", got, want)
	}
}

func TestStructuralDiffOrdinalsAreScopedToOneMap(t *testing.T) {
	exp := map[string]map[string]int{"x": {likePrefix + "A": 1}, "y": {likePrefix + "A": 2}}
	act := map[string]map[string]int{"x": {likePrefix + "A": 11}, "y": {likePrefix + "A": 12}}
	msg := EqualFailureMessage(exp, act)
	if strings.Contains(msg, "#") {
		t.Fatalf("equal labels in different maps must not get ordinals:\n%s", msg)
	}
}

func TestStructuralDiffOrdinalsDoNotDependOnInsertionOrder(t *testing.T) {
	suffixes := []string{"A", "B", "C", "D", "E", "F"}
	build := func(rng *rand.Rand, delta int) map[string]int {
		type kv struct {
			k string
			v int
		}
		var kvs []kv
		for i := 0; i < 45; i++ {
			kvs = append(kvs, kv{fmt.Sprintf("a%02d", i), i})
		}
		for i, s := range suffixes {
			kvs = append(kvs, kv{likePrefix + s, i + delta})
		}
		m := map[string]int{}
		for _, j := range rng.Perm(len(kvs)) {
			m[kvs[j].k] = kvs[j].v
		}
		return m
	}
	var first string
	for i := 0; i < 50; i++ {
		rng := rand.New(rand.NewSource(int64(i)))
		msg := EqualFailureMessage(build(rng, 0), build(rng, 100))
		if i == 0 {
			first = msg
			lines := diffLines(t, msg)
			if len(lines) != len(suffixes) {
				t.Fatalf("want %d lines:\n%s", len(suffixes), msg)
			}
			for n, l := range lines {
				if !strings.Contains(l, fmt.Sprintf(" #%d]", n+1)) {
					t.Fatalf("line %d lacks ordinal #%d:\n%s", n, n+1, msg)
				}
			}
		} else if msg != first {
			t.Fatalf("message depends on insertion order:\n%s\n---\n%s", first, msg)
		}
	}
}
