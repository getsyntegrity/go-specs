package assert

import (
	"math"
	"reflect"
	"testing"
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
