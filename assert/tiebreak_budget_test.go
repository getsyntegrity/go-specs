package assert

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

// The tie-break fingerprints of tiebreak.go are bounded per entry, and the work of one diagnostic
// message is bounded by fingerprintMessageBudget nodes. These tests pin that bound as a NODE COUNT
// (the meter of the message counts every value visited while fingerprinting, including the work of
// sorting nested maps), add a coarse wall-clock check on top, and pin that spending the budget is
// deterministic: the same value gives the same text whatever order its maps were built in.

// workClockLimit is the coarse wall-clock sanity limit of one message. The node count is the real
// assertion; this only catches a cost the node count does not see. A healthy message takes 40-60ms
// on a shared CI runner (the old 50ms limit flaked at 59ms), so the limit sits several times above
// that: it still fails on a cost that is orders of magnitude off (seconds), not on runner noise.
func workClockLimit() time.Duration {
	if raceEnabled {
		return 500 * time.Millisecond
	}
	return 250 * time.Millisecond
}

// nestedOne builds the value of the original finding: a [3]any that holds a 14-entry map whose 13
// NaN keys of one bit pattern hold an array that holds the map again and two copies of a smaller
// such array, seven levels deep. It returns the array.
func nestedOne() any {
	m := map[float64]any{}
	var one any
	for depth := 0; depth < 7; depth++ {
		one = [3]any{m, one, one}
	}
	m[1] = one
	for i := 0; i < 13; i++ {
		m[math.NaN()] = one
	}
	return one
}

// tiedMap returns a map of n entries with NaN keys of one bit pattern, each holding mk(i).
func tiedMap(n int, mk func(i int) any) map[float64]any {
	m := make(map[float64]any, n)
	for i := 0; i < n; i++ {
		m[math.NaN()] = mk(i)
	}
	return m
}

// chainOfTiedMaps is a chain of depth maps; every map has width NaN keys that all hold the next one.
func chainOfTiedMaps(depth, width int) any {
	var next any = "leaf"
	for d := 0; d < depth; d++ {
		next = tiedMap(width, func(int) any { return next })
	}
	return next
}

// deepSharedMaps is a DAG of depth levels; every level's map has two tied NaN entries that hold the
// level below, so an unfolding of it has 2^depth nodes.
func deepSharedMaps(depth int) any {
	var next any = 0
	for d := 0; d < depth; d++ {
		next = tiedMap(2, func(int) any { return next })
	}
	return next
}

// wideTiedValue is a fresh map of width entries, every key a NaN of one bit pattern, every value a
// small slice, so each fingerprint of it walks width nested entries and sorts them.
func wideTiedValue(width int) any {
	return tiedMap(width, func(i int) any { return []int{i % 5, i % 3} })
}

// workShape is one value whose message must stay inside the work bound. mk builds it fresh, so the
// same shape can be rendered as expected and as actual without sharing memory.
type workShape struct {
	name string
	mk   func() any
}

func acyclicWorkShapes() []workShape {
	return []workShape{
		{"14-entry map with 13 same-payload NaN keys", func() any { return nestedOne() }},
		{"1,000 tied NaN keys holding nested maps", func() any {
			one := nestedOne()
			return tiedMap(1000, func(int) any { return one })
		}},
		{"ten maps of 1,000 tied NaN keys", func() any {
			one := nestedOne()
			top := make([]map[float64]any, 10)
			for k := range top {
				top[k] = tiedMap(1000, func(int) any { return one })
			}
			return top
		}},
		{"ten maps of 1,000 tied NaN keys holding scalars", func() any {
			top := make([]map[float64]any, 10)
			for k := range top {
				top[k] = tiedMap(1000, func(i int) any { return i % 3 })
			}
			return top
		}},
		{"ten maps nested, 1,000 tied keys each", func() any { return chainOfTiedMaps(10, 1000) }},
		{"tied entries holding deep nested maps", func() any {
			return tiedMap(200, func(int) any { return deepSharedMaps(60) })
		}},
		{"tied entries holding wide nested maps", func() any {
			return tiedMap(100, func(int) any { return wideTiedValue(250) })
		}},
		{"tied entries holding distinct pointers to wide maps", func() any {
			out := map[float64]any{}
			for i := 0; i < 100; i++ {
				w := wideTiedValue(250)
				out[math.NaN()] = &w
			}
			return out
		}},
	}
}

// cyclicWorkShapes hold a reference to themselves under tied keys. They run in a subprocess, because
// a regression here would overflow the stack, which no recover can catch.
func cyclicWorkShapes() []workShape {
	return []workShape{
		{"map holding itself under 1,000 tied keys", func() any {
			m := map[float64]any{}
			for i := 0; i < 1000; i++ {
				m[math.NaN()] = m
			}
			return m
		}},
		{"tied keys holding distinct pointer cycles", func() any {
			m := map[float64]any{}
			for i := 0; i < 300; i++ {
				n := &tiedNode{V: i % 4}
				n.Next = n
				m[math.NaN()] = []any{n, m}
			}
			return m
		}},
		{"cycle through a tied map inside a slice", func() any {
			s := make([]any, 3)
			m := tiedMap(14, func(int) any { return s })
			s[0], s[1], s[2] = m, m, &s
			return s
		}},
	}
}

// messageRoutes are the ways a diagnostic message is built from a value: the value as expected, and
// a fresh twin of it as actual (nil for the routes that render one value). Each spends one meter.
func messageRoutes() []struct {
	name  string
	twin  bool
	build func(v, twin any, m *fpMeter) string
} {
	type route = struct {
		name  string
		twin  bool
		build func(v, twin any, m *fpMeter) string
	}
	return []route{
		{"EqualFailureMessage(v, nil)", false, func(v, _ any, m *fpMeter) string { return equalFailureMessageWith(v, nil, m) }},
		{"EqualFailureMessage(v, twin)", true, func(v, twin any, m *fpMeter) string { return equalFailureMessageWith(v, twin, m) }},
		{"poll renderer", false, func(v, _ any, m *fpMeter) string { return renderFallbackWith(reflect.ValueOf(v), m) }},
	}
}

// checkWorkBound builds the message of every route for shape and fails when a route visits more than
// the budget's nodes, builds a fingerprint outside the meter, or takes longer than the coarse clock
// limit. The values are built before the clock starts, and the clock is the best of three runs, so a
// loaded machine does not fail a check whose real assertion is the node count.
func checkWorkBound(t *testing.T, shape workShape) {
	t.Helper()
	for _, r := range messageRoutes() {
		v := shape.mk()
		var twin any
		if r.twin {
			twin = shape.mk()
		}
		best, visited, size := time.Duration(math.MaxInt64), 0, 0
		for run := 0; run < 3; run++ {
			m := newFPMeter()
			unmetered := unmeteredFingerprints.Load()
			start := time.Now()
			out := r.build(v, twin, m)
			best = min(best, time.Since(start))
			if n := unmeteredFingerprints.Load() - unmetered; n != 0 {
				t.Errorf("%s / %s: %d fingerprints were built outside the message meter", shape.name, r.name, n)
			}
			if run == 0 {
				visited, size = m.visited, len(out)
			} else if m.visited != visited || len(out) != size {
				t.Errorf("%s / %s: run %d visited %d nodes and printed %d bytes, the first run %d and %d", shape.name, r.name, run, m.visited, len(out), visited, size)
			}
		}
		t.Logf("%-58s %-30s nodes=%-7d bytes=%-6d took=%v", shape.name, r.name, visited, size, best)
		if visited > fingerprintMessageBudget {
			t.Errorf("%s / %s: visited %d fingerprint nodes, the message budget is %d", shape.name, r.name, visited, fingerprintMessageBudget)
		}
		if best > workClockLimit() {
			t.Errorf("%s / %s: took %v, want under %v", shape.name, r.name, best, workClockLimit())
		}
	}
}

func TestFingerprintWorkPerMessageIsBounded(t *testing.T) {
	for _, shape := range acyclicWorkShapes() {
		t.Run(shape.name, func(t *testing.T) { checkWorkBound(t, shape) })
	}
}

const workCycleEnv = "GO_SPECS_WORK_CYCLE_CHILD"

func TestFingerprintWorkPerMessageIsBoundedOnCycles(t *testing.T) {
	if os.Getenv(workCycleEnv) == "1" {
		for _, shape := range cyclicWorkShapes() {
			checkWorkBound(t, shape)
		}
		if !t.Failed() {
			fmt.Println("WORK-CYCLE-CHILD-DONE")
		}
		return
	}
	out := runInSubprocess(t, "TestFingerprintWorkPerMessageIsBoundedOnCycles", workCycleEnv)
	if !strings.Contains(out, "WORK-CYCLE-CHILD-DONE") {
		t.Fatalf("child did not finish:\n%s", out)
	}
}

// The budget is split by the size of the tied entries, never by the order the runtime iterates them,
// and an exhausted budget means ambiguity, never equality.
func TestTieAllowanceIsASharedPowerOfTwo(t *testing.T) {
	m := newFPMeter()
	for _, c := range []struct{ tied, want int }{
		{0, 0}, {1, 4096}, {2, 4096}, {3, 4096}, {4, 4096}, {5, 2048}, {13, 1024}, {1000, 16}, {100_000, 0},
	} {
		if got := m.tieAllowance(c.tied); got != c.want {
			t.Errorf("tieAllowance(%d) = %d, want %d (half of %d nodes over %d entries, rounded down to a power of two, at most %d)",
				c.tied, got, c.want, m.left, c.tied, fingerprintMaxNodes)
		}
	}
	m.left = 0
	if got := m.tieAllowance(2); got != 0 {
		t.Errorf("an exhausted meter must allow 0 nodes, got %d", got)
	}
}

// padded is a value whose first pad elements agree between siblings and whose last field tells them
// apart: a fingerprint has to reach about pad nodes to see it.
type padded struct {
	Pad []bool
	ID  int
}

func paddedValue(id int) padded { return padded{Pad: make([]bool, 900), ID: id} }

// exhaustingMaps is count maps of six tied entries. Each entry needs about 900 nodes to be told
// apart. The first line of the message renders the maps and the diff walks them, both spending the
// one budget of the message: the first map of the diff still gets what it needs, what it spends
// leaves the second and the third a smaller share, so the budget runs out between maps, inside the
// message.
func exhaustingMaps(rng *rand.Rand, count int) []map[float64]any {
	out := make([]map[float64]any, count)
	for k := range out {
		m := make(map[float64]any, 6)
		for _, id := range rng.Perm(6) {
			m[math.NaN()] = paddedValue(id)
		}
		out[k] = m
	}
	return out
}

func TestBudgetExhaustedBetweenMapsIsDeterministicAndWholeClasses(t *testing.T) {
	empty := []map[float64]any{{}, {}, {}}
	var firstDiff, firstPoll string
	for i := 0; i < 50; i++ {
		rng := rand.New(rand.NewSource(int64(i)))
		exp := exhaustingMaps(rng, 3)
		meter := newFPMeter()
		diff := equalFailureMessageWith(exp, empty, meter)
		// The poll renderer has no first line to share the budget with, so it takes more maps to run out.
		poll := renderFallbackWith(reflect.ValueOf(exhaustingMaps(rand.New(rand.NewSource(int64(i)+100)), 10)), newFPMeter())
		if i == 0 {
			firstDiff, firstPoll = diff, poll
			if !strings.Contains(diff, "[0][NaN #1]") || !strings.Contains(diff, "[0][NaN #6]") {
				t.Fatalf("the first map must be told apart entry by entry:\n%s", diff)
			}
			if !strings.Contains(diff, "6 entries indistinguishable within the diagnostic budget: all 6 missing in actual") {
				t.Fatalf("a map the budget ran out for must be one whole ambiguity group of its six entries:\n%s", diff)
			}
			if strings.Contains(diff, "[1][NaN #") {
				t.Fatalf("a tie class must never be split by the budget:\n%s", diff)
			}
			if !strings.Contains(poll, ambiguousValueMarker) {
				t.Fatalf("the poll renderer must hide the values it could not tell apart:\n%.400s", poll)
			}
			if meter.visited > fingerprintMessageBudget {
				t.Fatalf("visited %d nodes, budget %d", meter.visited, fingerprintMessageBudget)
			}
			continue
		}
		if diff != firstDiff {
			t.Fatalf("diff depends on insertion order (round %d):\n%s\n---\n%s", i, firstDiff, diff)
		}
		if poll != firstPoll {
			t.Fatalf("poll text depends on insertion order (round %d):\n%.600s\n---\n%.600s", i, firstPoll, poll)
		}
	}
}

// The same maps, rebuilt and rendered 50 times, print the same text, including when the map is
// larger than the window of the diff renderer and its tied classes are cut to it.
func TestBudgetedTiesRebuildToIdenticalText(t *testing.T) {
	build := func(seed int64) map[float64]any {
		rng := rand.New(rand.NewSource(seed))
		m := make(map[float64]any, 40)
		for _, id := range rng.Perm(40) {
			m[math.NaN()] = []any{id % 5, paddedValue(id % 7)}
		}
		return m
	}
	var first [3]string
	for i := 0; i < 50; i++ {
		got := [3]string{
			equalFailureMessageWith(build(int64(i)), nil, newFPMeter()),
			renderFallbackWith(reflect.ValueOf(build(int64(i))), newFPMeter()),
			renderDiffValue(reflect.ValueOf(build(int64(i))), true),
		}
		if i == 0 {
			first = got
			continue
		}
		if got != first {
			t.Fatalf("round %d differs:\n%q\n---\n%q", i, first, got)
		}
	}
}

// A recalled fingerprint is the fingerprint that would be computed again, so memoizing a tied entry
// that holds a reference changes neither the text nor, for a later use, what the message spends.
func TestSharedReferencesUnderTiedKeysAreFingerprintedOnce(t *testing.T) {
	shared := &padded{Pad: make([]bool, 300), ID: 1}
	distinct := func() map[float64]any {
		m := map[float64]any{}
		for i := 0; i < 50; i++ {
			m[math.NaN()] = &padded{Pad: make([]bool, 300), ID: 1}
		}
		return m
	}
	same := map[float64]any{}
	for i := 0; i < 50; i++ {
		same[math.NaN()] = shared
	}
	sharedMeter, distinctMeter := newFPMeter(), newFPMeter()
	sameText := renderFallbackWith(reflect.ValueOf(same), sharedMeter)
	distinctText := renderFallbackWith(reflect.ValueOf(distinct()), distinctMeter)
	if sharedMeter.visited*10 > distinctMeter.visited {
		t.Errorf("50 entries holding one pointer visited %d nodes, 50 distinct pointers %d: want the shared one fingerprinted once",
			sharedMeter.visited, distinctMeter.visited)
	}
	if sameText == "" || distinctText == "" {
		t.Fatal("empty rendering")
	}
}

// The verdicts, and the number of matcher evaluations, never depend on the budget.
func TestExhaustedMeterStillGivesAValidMessage(t *testing.T) {
	a := tiedMap(5, func(i int) any { return paddedValue(i) })
	m := newFPMeter()
	m.left = 0
	msg := equalFailureMessageWith(a, map[float64]any{}, m)
	if !strings.Contains(msg, "5 entries indistinguishable within the diagnostic budget: all 5 missing in actual") {
		t.Fatalf("an exhausted budget must read as ambiguity, got:\n%s", msg)
	}
	if m.visited != 0 {
		t.Fatalf("an exhausted meter visited %d nodes", m.visited)
	}
	if ValuesEqual(a, tiedMap(5, func(i int) any { return paddedValue(i) })) {
		t.Fatal("maps with NaN keys are never equal")
	}
}
