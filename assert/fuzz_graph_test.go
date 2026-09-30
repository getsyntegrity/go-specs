package assert

import (
	"math"
	"reflect"
	"testing"
)

// Unit tests of the recipe interpreter (fuzz_recipe_test.go). The fuzz targets are only as good as
// the graphs they can reach, so these pin down what one recipe byte string is able to build.

func TestFuzzBuildIsDeterministicAndBounded(t *testing.T) {
	for _, data := range [][]byte{nil, {0}, {255, 255, 255}, fzRecipeSelfMap(), fzRecipeNaNTies()} {
		g1, g2 := fzBuild(data), fzBuild(data)
		if g1.n != g2.n || g1.n < 1 || g1.n > fzMaxNodes {
			t.Fatalf("node count %d/%d outside 1..%d", g1.n, g2.n, fzMaxNodes)
		}
		if !reflect.DeepEqual(g1.shape(), g2.shape()) {
			t.Fatalf("rebuilding %x changed the shape: %v vs %v", data, g1.shape(), g2.shape())
		}
	}
}

func TestFuzzBuildReachesTheHardShapes(t *testing.T) {
	t.Run("a map that contains itself", func(t *testing.T) {
		g := fzBuild(fzRecipeSelfMap())
		m, ok := g.actual().(map[string]any)
		if !ok || len(m) == 0 {
			t.Fatalf("root is %T, want a non-empty map[string]any", g.actual())
		}
		if !fzInfinite(g.actual()) {
			t.Fatal("the oracle does not see the cycle")
		}
	})
	t.Run("two slice views share an address and differ in length", func(t *testing.T) {
		g := fzBuild(fzRecipeViews())
		a, aok := g.value(1, 0).([]any)
		b, bok := g.value(2, 0).([]any)
		if !aok || !bok || len(a) == len(b) || &a[0] != &b[0] {
			t.Fatalf("views %d/%d elements, same address %v", len(a), len(b), aok && bok && len(a) > 0 && len(b) > 0 && &a[0] == &b[0])
		}
	})
	t.Run("several NaN keys with different payloads", func(t *testing.T) {
		g := fzBuild(fzRecipeNaNTies())
		m, ok := g.actual().(map[float64]any)
		if !ok || len(m) < 3 {
			t.Fatalf("root is %T with %d entries, want map[float64]any with >= 3 NaN entries", g.actual(), len(m))
		}
		bits := map[uint64]int{}
		for k := range m {
			if !math.IsNaN(k) {
				t.Fatalf("key %v is not NaN", k)
			}
			bits[math.Float64bits(k)]++
		}
		if len(bits) < 2 {
			t.Fatalf("only one NaN payload: %v", bits)
		}
	})
	t.Run("many NaN keys of one payload in one map", func(t *testing.T) {
		m, ok := fzBuild(fzRecipeManyNaNTies()).actual().(map[float64]any)
		if !ok || len(m) != 24 {
			t.Fatalf("root is %T with %d entries, want map[float64]any with 24 tied NaN entries", fzBuild(fzRecipeManyNaNTies()).actual(), len(m))
		}
		bits := map[uint64]bool{}
		for k := range m {
			bits[math.Float64bits(k)] = true
		}
		if len(bits) != 1 {
			t.Fatalf("want one NaN payload, got %d", len(bits))
		}
	})
	t.Run("limits: wide map, big slice, deep chain, long string", func(t *testing.T) {
		wide := fzBuild(fzRecipeLimits(fkWideMap, 255))
		if m := wide.actual().(map[int]any); len(m) <= mapSortCap {
			t.Fatalf("wide map has %d entries, want > %d", len(m), mapSortCap)
		}
		mixed := fzBuild(fzRecipeLimits(fkWideMap, 200))
		if m := mixed.actual().(map[any]any); len(m) <= mapSortCap {
			t.Fatalf("mixed-key wide map has %d entries, want > %d", len(m), mapSortCap)
		}
		big := fzBuild(fzRecipeLimits(fkBigInts, 255))
		if s := big.actual().([]int); len(s) <= boundedNodeBudget {
			t.Fatalf("big slice has %d elements, want > %d", len(s), boundedNodeBudget)
		}
		long := fzBuild(fzRecipeLimits(fkLongStr, 255))
		if s := long.actual().(string); len(s) <= 256 {
			t.Fatalf("long string has %d bytes, want > 256", len(s))
		}
		if !fzInfinite(fzBuild(fzRecipeLimits(fkChain, 255)).actual()) {
			t.Fatal("the deep chain closes back on itself and must read as infinite")
		}
	})
	t.Run("pointer map keys are flagged", func(t *testing.T) {
		if !fzBuild(fzRecipePtrKey()).addrKeys {
			t.Fatal("a recipe with a pointer key must set addrKeys")
		}
		if fzBuild(fzRecipeNaNTies()).addrKeys {
			t.Fatal("NaN keys are not address keys")
		}
	})
}

func TestFuzzOracleCountsUnrolledNodes(t *testing.T) {
	if n := fzUnrolled(reflect.ValueOf([]any{1, "a", nil}), 100); n < 3 || n > 8 {
		t.Fatalf("small slice counted %d", n)
	}
	if n := fzUnrolled(reflect.ValueOf(make([]int, 500)), 10_000); n < 500 {
		t.Fatalf("scalar slice counted %d, want at least its length", n)
	}
}

func TestFuzzTwinDiffersInOneLeaf(t *testing.T) {
	data := fzRecipeNestedMaps()
	g := fzBuild(data)
	// Node 4 is an int; change it through the tail: mode 1, node 4, change 2.
	tl := &fzReader{data: []byte{1, 4, 2}}
	other, twin := g.operandB(tl)
	if twin == g {
		t.Fatal("mode 1 must build a twin")
	}
	if !reflect.DeepEqual(g.shape(), twin.shape()) {
		t.Fatalf("a twin keeps the shape: %v vs %v", g.shape(), twin.shape())
	}
	if g.nodes[4].val == twin.nodes[4].val {
		t.Fatalf("the twin's leaf is unchanged: %v", g.nodes[4].val)
	}
	if other == nil {
		t.Fatal("the twin operand is nil")
	}
	// A node that is not a leaf is left alone: the twin is an exact copy.
	_, same := g.operandB(&fzReader{data: []byte{1, 0, 2}})
	if !reflect.DeepEqual(g.shape(), same.shape()) {
		t.Fatal("twinning a container must keep the shape")
	}
}
