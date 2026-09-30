package assert

import (
	"math"
	"os"
	"reflect"
	"testing"
	"time"
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

// Finding 2, open: the cost of the tie-break fingerprints of one message is bounded per entry but
// not per message.
//
// FuzzMatcherDiagnostics and FuzzPollMessage found recipes of 15 to 500 bytes whose iteration took
// several seconds, which the fuzzing engine reports as a hung worker. The smallest,
// "A00.\r\r\r\r\r0N001" (before the fzMaxNaNTies cap), is a [3]any that holds a 14-entry
// map[float64]any with 13 NaN keys of one bit pattern; one render of it took about 60 ms, and the
// matcher target renders such a graph about 140 times per iteration (9 s in all). Nothing loops:
// every limit holds (bytes, depth, entries, nodes). The cost is in tiebreak.go and the way the
// renderers call it. Every entry whose key ties gets a fingerprint of at most 4,096 bytes, but
// building it visits nested maps one sub-builder per entry, splitting the bytes left equally, so the
// walk keeps visiting long after it stops writing; the renderer builds that fingerprint again on
// every render of a map, and a map of up to 1,024 entries has all its keys rendered and compared
// before it is cut to 16. The budget of 10,000 nodes allows about ten such maps in one message.
//
// It is not fixed here: a bound on the work that stays deterministic (each entry gets an equal
// share of the work, as it has of the bytes) changes the tie-break design of #371, not a renderer.
// The fuzz recipes cap tied NaN entries per map (fzMaxNaNTies) so the fuzzing engine stays usable,
// and this test keeps the shape. Run it with GOSPECS_FUZZ_FINDINGS=1 to see it fail.
func TestFuzzFindingTieBreakFingerprintCost(t *testing.T) {
	if os.Getenv("GOSPECS_FUZZ_FINDINGS") == "" {
		t.Skip("open finding 2 (tie-break fingerprint work is not bounded per message); set GOSPECS_FUZZ_FINDINGS=1 to run it")
	}
	// The recipe of the finding, built by hand (fzBuild now caps tied NaN entries): a 14-entry map
	// whose 13 NaN keys of one bit pattern hold an array that holds the map and two copies of a
	// smaller such array, seven levels deep. One render of it takes tens of milliseconds.
	m := map[float64]any{}
	var one any
	for depth := 0; depth < 7; depth++ {
		one = [3]any{m, one, one}
	}
	m[1] = one
	for i := 0; i < 13; i++ {
		m[math.NaN()] = one
	}
	start := time.Now()
	renderFallback(reflect.ValueOf(one))
	if took := time.Since(start); took > 20*time.Millisecond {
		t.Errorf("rendering a [3]any that holds a 14-entry map took %v, want under 20ms", took)
	}

	// The same value under 1,000 tied NaN keys, in ten maps: a render builds one fingerprint per tied
	// entry each time it renders such a map, and the node budget allows about ten renders.
	top := make([]map[float64]any, 10)
	for k := range top {
		top[k] = make(map[float64]any, 1000)
		for i := 0; i < 1000; i++ {
			top[k][math.NaN()] = one
		}
	}
	const limit = 250 * time.Millisecond
	start = time.Now()
	text := renderFallback(reflect.ValueOf(top))
	if took := time.Since(start); took > limit {
		t.Errorf("rendering ten maps of 1,000 tied NaN keys took %v (%d bytes), want under %v", took, len(text), limit)
	}
	start = time.Now()
	msg := EqualFailureMessage(top, nil)
	if took := time.Since(start); took > limit {
		t.Errorf("EqualFailureMessage of the same maps took %v (%d bytes), want under %v", took, len(msg), limit)
	}
}
