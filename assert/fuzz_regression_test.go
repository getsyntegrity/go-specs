package assert

import (
	"fmt"
	"math"
	"reflect"
	"strings"
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

// Finding 2, FuzzMatcherDiagnostics and FuzzPollMessage (fixed in #371): the cost of the tie-break
// fingerprints of one message was bounded per entry but not per message.
//
// The fuzz targets found recipes of 15 to 500 bytes whose iteration took several seconds, which the
// fuzzing engine reports as a hung worker. The smallest, "A00.\r\r\r\r\r0N001", is a [3]any that holds a
// 14-entry map[float64]any with 13 NaN keys of one bit pattern; one render of it took about 60 ms, and
// the matcher target renders such a graph about 140 times per iteration (9 s in all). Nothing looped:
// every limit held (bytes, depth, entries, nodes). The cost was in tiebreak.go and the way the
// renderers call it. Every entry whose key ties got a fingerprint of at most 4,096 bytes, but building
// it visited nested maps one sub-builder per entry, splitting the bytes left equally, so the walk kept
// visiting long after it stopped writing; the renderer built that fingerprint again on every render of
// a map, and a map of up to 1,024 entries had all its keys rendered and compared before it was cut to
// 16. The node budget of the renderer allowed about ten such maps in one message: 492,000 fingerprint
// nodes (88 ms) for the value below and 8.3 million (1.4 s) for ten maps of 1,000 tied keys.
//
// The fix bounds the fingerprint work of one message to fingerprintMessageBudget nodes, split by tie
// class size and never by iteration order (see tiebreak.go and docs/DSL.md). The test keeps the
// minimal shape and asserts the node count, with a coarse clock on top; the fuzz recipes no longer cap
// the NaN entries that share a key.
func TestFuzzFindingTieBreakFingerprintCost(t *testing.T) {
	// The recipe of the finding, built by hand: a 14-entry map whose 13 NaN keys of one bit pattern
	// hold an array that holds the map and two copies of a smaller such array, seven levels deep.
	m := map[float64]any{}
	var one any
	for depth := 0; depth < 7; depth++ {
		one = [3]any{m, one, one}
	}
	m[1] = one
	for i := 0; i < 13; i++ {
		m[math.NaN()] = one
	}
	// The same value under 1,000 tied NaN keys, in ten maps: each render of such a map used to build
	// one fingerprint per tied entry.
	top := make([]map[float64]any, 10)
	for k := range top {
		top[k] = make(map[float64]any, 1000)
		for i := 0; i < 1000; i++ {
			top[k][math.NaN()] = one
		}
	}
	for _, c := range []struct {
		name  string
		build func(m *fpMeter) string
	}{
		{"render of the [3]any that holds a 14-entry map", func(m *fpMeter) string { return renderFallbackWith(reflect.ValueOf(one), m) }},
		{"render of ten maps of 1,000 tied NaN keys", func(m *fpMeter) string { return renderFallbackWith(reflect.ValueOf(top), m) }},
		{"EqualFailureMessage of the same maps", func(m *fpMeter) string { return equalFailureMessageWith(top, nil, m) }},
	} {
		best, visited, size := time.Duration(math.MaxInt64), 0, 0
		for run := 0; run < 3; run++ {
			meter := newFPMeter()
			start := time.Now()
			out := c.build(meter)
			best, visited, size = min(best, time.Since(start)), meter.visited, len(out)
		}
		if visited > fingerprintMessageBudget {
			t.Errorf("%s visited %d fingerprint nodes, the message budget is %d", c.name, visited, fingerprintMessageBudget)
		}
		if best > workClockLimit() {
			t.Errorf("%s took %v (%d bytes, %d nodes), want under %v", c.name, best, size, visited, workClockLimit())
		}
	}
}

// Finding 3, FuzzEqualFailureMessage testdata/fuzz/FuzzEqualFailureMessage/depth-limit-probes-rewalk-shared-subtrees
// (a 512-byte recipe of 26 nodes, maps of 1,000 to 14,000 entries, found by the first 120 s run after
// finding 2 was fixed): the structural diff took about 1.9 s to build one message, three times that
// for the target, which the fuzzing engine reports as a hung worker. The fingerprints were not the
// cost (1,556 nodes).
//
// Below the path depth limit the diff asks differs whether a subtree differs at all, to decide
// whether it needs a line. differs is a fresh walk: it sorted and labelled every map it met, as the
// diff itself does, and a subtree shared under many paths was walked again for each of them (735
// probes, 700,000 entries sorted, for one message). A probe now walks maps without sorting or
// labelling (probeMap) and a reference pair asked about twice is answered once (w.probed).
//
// The value: 8 levels of single-entry maps, then a slice of 400 pointers to one map of 3,000 entries
// (expected and actual hold equal, separately built maps) and a last element that differs.
func TestFuzzFindingDepthLimitProbesWalkSharedSubtreesOnce(t *testing.T) {
	big := func() *map[int]int {
		m := make(map[int]int, 3000)
		for i := 0; i < 3000; i++ {
			m[i] = i * 2
		}
		return &m
	}
	build := func(last int) any {
		shared := big()
		elems := make([]any, 401)
		for i := 0; i < 400; i++ {
			elems[i] = shared
		}
		elems[400] = last
		var v any = elems
		for d := 0; d < 7; d++ {
			v = map[string]any{"k": v}
		}
		return v
	}
	expected, actual := build(1), build(2)
	best, walks, msg := time.Duration(math.MaxInt64), int64(0), ""
	for run := 0; run < 3; run++ {
		before := probeWalks.Load()
		start := time.Now()
		msg = EqualFailureMessage(expected, actual)
		best, walks = min(best, time.Since(start)), probeWalks.Load()-before
	}
	if !strings.Contains(msg, "expected 1, actual 2") {
		t.Fatalf("the differing element is missing from the message:\n%s", msg)
	}
	if walks > 3 {
		t.Errorf("a pointer pair shared by 400 elements was probed %d times in one message, want once", walks)
	}
	if best > workClockLimit() {
		t.Errorf("building the message took %v, want under %v", best, workClockLimit())
	}
}

// Finding 4, FuzzMatcherDiagnostics testdata/fuzz/FuzzMatcherDiagnostics/big-map-scanned-once-per-render
// (a 512-byte recipe of 26 nodes with maps of 1,000 to 14,000 entries, found by the second 120 s run):
// building the diagnostics of the matcher target took 3 s, which the fuzzing engine reports as a hung
// worker (its limit for one input is about a second). The tie-break was not the cost.
//
// Rendering a map selects its 4 or 16 smallest entries with one pass over the whole map, and a map
// shared by several parents, or containing itself, is rendered once per path that reaches it: up to
// 1 + 4 + 16 + 64 times per value at the diff renderer's 3 levels, and again for every line and
// operand of the message. A map of 14,000 entries was scanned thousands of times. The window of a big
// map is now selected once per message (fpMeter.windows), so it also prints the same everywhere in
// the message.
//
// The value: a map of 20,000 entries every one of which holds the map itself, against one that
// differs by a single entry.
func TestFuzzFindingBigMapIsScannedOncePerMessage(t *testing.T) {
	build := func(first int) map[int]any {
		m := make(map[int]any, 20_000)
		for i := 0; i < 20_000; i++ {
			m[i] = m
		}
		m[0] = first
		return m
	}
	expected, actual := build(1), build(2)
	best, scans, msg := time.Duration(math.MaxInt64), 0, ""
	for run := 0; run < 3; run++ {
		meter := newFPMeter()
		start := time.Now()
		msg = equalFailureMessageWith(expected, actual, meter)
		best, scans = min(best, time.Since(start)), meter.scans
	}
	if !strings.Contains(msg, "expected 1, actual 2") {
		t.Fatalf("the differing entry is missing from the message:\n%s", msg)
	}
	// Two maps, each selected once for the diff renderer's window of 4 entries.
	if scans > 2 {
		t.Errorf("the message scanned big maps %d times, want once per map", scans)
	}
	// Sorting 20,000 entries for the diff itself is most of the time: the scan count is the assertion.
	if limit := 5 * workClockLimit(); best > limit {
		t.Errorf("building the message took %v, want under %v", best, limit)
	}
}

// Finding 5 (review of #372): the structural diff kept walking, building paths and rendering values
// after its entry limit was full, so comparing a huge slice or map with an empty one rendered every
// element even though the message shows ten differences.
func TestStructuralDiffStopsRenderingOncePastItsLimit(t *testing.T) {
	const n = 100_000
	bigMap := func() map[int]int {
		m := make(map[int]int, n)
		for i := 0; i < n; i++ {
			m[i] = i
		}
		return m
	}
	section := func(lines func(i int) string, first string) string {
		var b strings.Builder
		b.WriteString("differences:")
		if first != "" {
			b.WriteString("\n  " + first)
		}
		for i := 0; i < diffMaxEntries-btoi(first != ""); i++ {
			b.WriteString("\n  " + lines(i))
		}
		return b.String() + "\n  ... more differences not shown (limit 10)"
	}
	cases := []struct {
		name         string
		expected, ac any
		want         string
	}{
		{"slice missing", make([]int, n), []int{}, section(func(i int) string {
			return fmt.Sprintf("[%d]: missing in actual (expected 0)", i)
		}, fmt.Sprintf("<root>: length: expected %d, actual 0", n))},
		{"slice unexpected", []int{}, make([]int, n), section(func(i int) string {
			return fmt.Sprintf("[%d]: unexpected in actual (0)", i)
		}, fmt.Sprintf("<root>: length: expected 0, actual %d", n))},
		{"map missing", bigMap(), map[int]int{}, section(func(i int) string {
			return fmt.Sprintf("[%d]: missing in actual (expected %d)", i, i)
		}, "")},
		{"map unexpected", map[int]int{}, bigMap(), section(func(i int) string {
			return fmt.Sprintf("[%d]: unexpected in actual (%d)", i, i)
		}, "")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := diffRenders.Load()
			msg := EqualFailureMessage(tc.expected, tc.ac)
			renders := diffRenders.Load() - before
			t.Logf("renders=%d", renders)
			if renders > int64(diffLabelWindow+3*diffMaxEntries) { // label window + line values + the two header renders
				t.Errorf("rendered %d values for a message that shows %d differences", renders, diffMaxEntries)
			}
			i := strings.Index(msg, "differences:")
			if i < 0 || msg[i:] != tc.want {
				t.Fatalf("differences section changed:\n%s\nwant:\n%s", msg[max(i, 0):], tc.want)
			}
		})
	}
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}
