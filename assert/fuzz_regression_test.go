package assert

import (
	"reflect"
	"testing"
)

// Regressions the fuzz targets found. Each keeps its minimal input as a committed corpus file under
// testdata/fuzz/<Target>/ (replayed by `go test` and by TestFuzzCorpusSubprocess) and as a named test
// here that fails without the fix.

// Finding 1, FuzzPollMessage testdata/fuzz/FuzzPollMessage/budget-exhausted-while-rendering-map-keys
// (recipe "B2cB000B0", 9 bytes): a map of up to 1,024 entries has all its keys rendered before the
// entries are sorted and cut to 16. The keys were rendered in map iteration order, so when the node
// budget ran out in the middle of them, which keys got their text and which were cut to
// "<truncated>" depended on the iteration order, and the same value printed differently from one call
// to the next. The keys are now rendered in their total order.
//
// The value: 16 maps of 1,000 int keys under one slice. The renderer prints 16 elements and each map
// costs 1,000 nodes of the 10,000 budget, so the budget runs out inside the 10th map's keys.
func TestRenderFallbackBudgetExhaustedInsideMapKeysIsDeterministic(t *testing.T) {
	maps := make([]map[int]int, 16)
	for i := range maps {
		maps[i] = make(map[int]int, 1000)
		for k := 0; k < 1000; k++ {
			maps[i][k*7+i] = k
		}
	}
	// A cycle keeps safeForFmt from taking the fmt path, so the bounded renderer is the one under test.
	var self []any
	self = append(self, self, maps)
	self[0] = &self
	root := reflect.ValueOf(&self)
	first := renderFallback(root)
	for i := 0; i < 50; i++ {
		if got := renderFallback(root); got != first {
			t.Fatalf("render %d differs from the first:\n first: %.400q\n now:   %.400q", i+2, first, got)
		}
	}
}
