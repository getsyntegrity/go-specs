package specs

import (
	"reflect"
	"strings"
	"testing"
)

// collectSequence drains a pathSequence, formatting each candidate the same way ForEach's
// caller would (via FormatPathValuesForReport) so it can be compared against ForEach's output.
func collectSequence(g *PathGenerator) []string {
	seq := g.sequence()
	var got []string
	for {
		pv, ok := seq.next()
		if !ok {
			break
		}
		got = append(got, g.FormatPathValuesForReport(pv))
	}
	return got
}

func collectForEach(g *PathGenerator) []string {
	var want []string
	g.ForEach(func(pv PathValues) { want = append(want, g.FormatPathValuesForReport(pv)) })
	return want
}

// collectSequenceWithFeedback drains a pathSequence like collectSequence, but — matching how
// runExecutionContext actually drives a sequence in production — calls admitFeedback(pv, true, nil)
// right after each next(), before proposing the next candidate. For strategyPlain (Explore) this
// interleaving is what makes its incremental corpus growth line up with ForEach's synchronous
// growth timing; plain collectSequence (no feedback) leaves its corpus frozen at the seed. The nil
// Coverage is correct here: this helper never executes a real case, so it has no real coverage to
// report, matching strategyPlain (which ignores cov) but making this helper unusable for
// strategyCoverage/strategySmart parity — see their corpus-growth tests instead, which use real
// synthetic Coverage values.
func collectSequenceWithFeedback(g *PathGenerator) []string {
	seq := g.sequence()
	var got []string
	for {
		pv, ok := seq.next()
		if !ok {
			break
		}
		got = append(got, g.FormatPathValuesForReport(pv))
		seq.admitFeedback(pv, true, nil)
	}
	return got
}

func TestPathSequenceCartesianMatchesForEachOrder(t *testing.T) {
	gen := newPathGenerator([]PathVar{
		{Name: "x", Values: []any{1, 2, 3}},
		{Name: "y", Values: []any{"a", "b"}},
	}, nil, 0, 0, false, 0, 0, 0)

	got, want := collectSequence(gen), collectForEach(gen)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sequence order = %v, want %v (must match ForEach exactly, including the first combination)", got, want)
	}
	if len(got) != 6 {
		t.Fatalf("len(got) = %d, want 6 (3x2 combinations)", len(got))
	}
}

func TestPathSequenceCartesianWithFiltersMatchesForEachOrder(t *testing.T) {
	gen := newPathGenerator([]PathVar{
		{Name: "x", Values: []any{1, 2, 3, 4}},
	}, []PathFilter{func(pv PathValues) bool {
		v, _ := pv.lookup("x")
		return v.(int)%2 == 0
	}}, 0, 0, false, 0, 0, 0)

	got, want := collectSequence(gen), collectForEach(gen)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("filtered sequence order = %v, want %v", got, want)
	}
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2 (only even x)", len(got))
	}
}

func TestPathSequenceTrivialGeneratorYieldsOneEmptyValue(t *testing.T) {
	gen := newPathGenerator(nil, nil, 0, 0, false, 0, 0, 0)
	seq := gen.sequence()
	if _, ok := seq.next(); !ok {
		t.Fatal("expected exactly one candidate for a generator with no vars")
	}
	if _, ok := seq.next(); ok {
		t.Fatal("expected exhaustion after the single trivial candidate")
	}
}

func TestPathSequenceAdmitFeedbackIsNoOpForCartesian(t *testing.T) {
	gen := newPathGenerator([]PathVar{{Name: "x", Values: []any{1, 2}}}, nil, 0, 0, false, 0, 0, 0)
	seq := gen.sequence()
	before, _ := seq.next()
	seq.admitFeedback(before, true, nil)
	seq.admitFeedback(before, false, nil)
	after, ok := seq.next()
	if !ok {
		t.Fatal("expected a second candidate")
	}
	if reflect.DeepEqual(before, after) {
		t.Fatal("second candidate must differ from the first regardless of admitFeedback calls")
	}
}

func TestPathGeneratorBoundsIsConservativeUpperBound(t *testing.T) {
	gen := newPathGenerator([]PathVar{
		{Name: "x", Values: []any{1, 2, 3}},
		{Name: "y", Values: []any{"a", "b"}},
	}, []PathFilter{func(pv PathValues) bool {
		v, _ := pv.lookup("x")
		return v.(int) == 1
	}}, 0, 0, false, 0, 0, 0)

	maxAttempts, maxAccepted, maxRejections := gen.bounds()
	if maxAttempts != 6 || maxAccepted != 6 || maxRejections != 6 {
		t.Fatalf("bounds = (%d, %d, %d), want (6, 6, 6) — the raw product, ignoring filters", maxAttempts, maxAccepted, maxRejections)
	}
	actual := len(collectSequence(gen))
	if actual >= maxAccepted {
		t.Fatalf("actual accepted candidates = %d, want fewer than the conservative bound %d (filters reduce the real count)", actual, maxAccepted)
	}
}

func TestPathSequenceSampleEmitsExactlyRequestedCount(t *testing.T) {
	const samples = 7
	gen := newPathGenerator([]PathVar{
		{Name: "x", rangeSpec: &intRange{min: 0, max: 100}},
	}, nil, samples, 0, false, 0, 0, 0)

	got := collectSequence(gen)
	if len(got) != samples {
		t.Fatalf("len(got) = %d, want %d", len(got), samples)
	}
}

func TestPathSequenceSampleMatchesRunSamplesForSameSeed(t *testing.T) {
	newGen := func() *PathGenerator {
		return newPathGenerator([]PathVar{
			{Name: "x", rangeSpec: &intRange{min: 0, max: 50}},
		}, nil, 5, 42, true, 0, 0, 0)
	}

	got := collectSequence(newGen())
	want := collectForEach(newGen())
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sequence-based samples = %v, want %v (must match runSamples exactly for the same seed)", got, want)
	}
}

func TestPathSequenceSampleRespectsFilters(t *testing.T) {
	gen := newPathGenerator([]PathVar{
		{Name: "x", rangeSpec: &intRange{min: 0, max: 20}},
	}, []PathFilter{func(pv PathValues) bool {
		v, _ := pv.lookup("x")
		return v.(int)%2 == 0
	}}, 6, 0, false, 0, 0, 0)

	seq := gen.sequence()
	for i := 0; i < 6; i++ {
		pv, ok := seq.next()
		if !ok {
			t.Fatalf("expected candidate %d, got exhaustion", i)
		}
		if pv.Int("x")%2 != 0 {
			t.Fatalf("expected even x, got %d", pv.Int("x"))
		}
	}
	if _, ok := seq.next(); ok {
		t.Fatal("expected exhaustion after 6 accepted candidates")
	}
}

func TestPathSequenceSamplePanicsWhenFiltersCannotBeSatisfied(t *testing.T) {
	gen := newPathGenerator([]PathVar{
		{Name: "x", Values: []any{1}},
	}, []PathFilter{func(PathValues) bool { return false }}, 3, 0, false, 0, 0, 0)

	seq := gen.sequence()
	assertPanics(t, func() { seq.next() })
}

func TestPathSequenceSampleBoundsIsExact(t *testing.T) {
	gen := newPathGenerator([]PathVar{
		{Name: "x", rangeSpec: &intRange{min: 0, max: 100}},
	}, nil, 9, 0, false, 0, 0, 0)

	maxAttempts, maxAccepted, maxRejections := gen.bounds()
	if maxAttempts != 9 || maxAccepted != 9 || maxRejections != 9 {
		t.Fatalf("bounds = (%d, %d, %d), want (9, 9, 9)", maxAttempts, maxAccepted, maxRejections)
	}
	if got := len(collectSequence(gen)); got != maxAccepted {
		t.Fatalf("actual accepted candidates = %d, want exactly %d", got, maxAccepted)
	}
}

func TestPathSequenceSampleAdmitFeedbackDoesNotAlterSequence(t *testing.T) {
	newGen := func() *PathGenerator {
		return newPathGenerator([]PathVar{
			{Name: "x", rangeSpec: &intRange{min: 0, max: 50}},
		}, nil, 5, 7, true, 0, 0, 0)
	}

	baseline := collectSequence(newGen())

	gen := newGen()
	seq := gen.sequence()
	var withFeedback []string
	for {
		pv, ok := seq.next()
		if !ok {
			break
		}
		withFeedback = append(withFeedback, gen.FormatPathValuesForReport(pv))
		seq.admitFeedback(pv, withFeedback != nil, nil)
	}
	if !reflect.DeepEqual(baseline, withFeedback) {
		t.Fatalf("admitFeedback altered the sequence: got %v, want %v", withFeedback, baseline)
	}
}

func TestPathSequenceExploreEmitsExactlyRequestedIterations(t *testing.T) {
	const iterations = 8
	gen := newPathGenerator([]PathVar{
		{Name: "x", rangeSpec: &intRange{min: 0, max: 100}},
	}, nil, 0, 0, false, iterations, 0, 0)

	got := collectSequence(gen)
	if len(got) != iterations {
		t.Fatalf("len(got) = %d, want %d", len(got), iterations)
	}
}

func TestPathSequenceExploreMatchesForEachForSameSeed(t *testing.T) {
	newGen := func() *PathGenerator {
		return newPathGenerator([]PathVar{
			{Name: "x", rangeSpec: &intRange{min: 0, max: 50}},
		}, nil, 0, 42, true, 6, 0, 0)
	}

	// Corpus growth now happens in admitFeedback, not next() — so the comparison must drive the
	// sequence the way runExecutionContext actually does (next() then admitFeedback()) to match
	// ForEach's synchronous growth timing. See collectSequenceWithFeedback's doc comment.
	got := collectSequenceWithFeedback(newGen())
	want := collectForEach(newGen())
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sequence-based explore = %v, want %v (must match runPlainExploration exactly for the same seed)", got, want)
	}
}

func TestPathSequenceExploreRespectsFilters(t *testing.T) {
	gen := newPathGenerator([]PathVar{
		{Name: "x", rangeSpec: &intRange{min: 0, max: 20}},
	}, []PathFilter{func(pv PathValues) bool {
		v, _ := pv.lookup("x")
		return v.(int)%2 == 0
	}}, 0, 0, false, 7, 0, 0)

	seq := gen.sequence()
	for i := 0; i < 7; i++ {
		pv, ok := seq.next()
		if !ok {
			t.Fatalf("expected candidate %d, got exhaustion", i)
		}
		if pv.Int("x")%2 != 0 {
			t.Fatalf("expected even x, got %d", pv.Int("x"))
		}
	}
	if _, ok := seq.next(); ok {
		t.Fatal("expected exhaustion after 7 accepted candidates")
	}
}

func TestPathSequenceExploreBoundsIsExact(t *testing.T) {
	gen := newPathGenerator([]PathVar{
		{Name: "x", rangeSpec: &intRange{min: 0, max: 100}},
	}, nil, 0, 0, false, 9, 0, 0)

	maxAttempts, maxAccepted, maxRejections := gen.bounds()
	if maxAttempts != 9 || maxAccepted != 9 || maxRejections != 9 {
		t.Fatalf("bounds = (%d, %d, %d), want (9, 9, 9)", maxAttempts, maxAccepted, maxRejections)
	}
	if got := len(collectSequence(gen)); got != maxAccepted {
		t.Fatalf("actual accepted candidates = %d, want exactly %d", got, maxAccepted)
	}
}

// TestPathSequenceExploreCorpusGrowsOnlyAfterAdmitFeedback is #54's PR A architectural boundary:
// nextExplore only proposes candidates, it must never grow exploreCorpus itself — corpus growth
// happens exclusively in admitFeedback, after the candidate has actually run. This freezes the
// post-execution feedback boundary that PR B (real coverage) will build on.
func TestPathSequenceExploreCorpusGrowsOnlyAfterAdmitFeedback(t *testing.T) {
	gen := newPathGenerator([]PathVar{
		{Name: "x", rangeSpec: &intRange{min: 0, max: 50}},
	}, nil, 0, 7, true, 6, 0, 0)

	seq := gen.sequence()
	pv, ok := seq.next()
	if !ok {
		t.Fatal("expected a candidate")
	}
	if got := len(seq.exploreCorpus); got != 1 {
		t.Fatalf("next() grew the corpus: len(exploreCorpus) = %d, want 1 (seed only, before any admitFeedback)", got)
	}

	seq.admitFeedback(pv, true, nil)
	if got := len(seq.exploreCorpus); got != 2 {
		t.Fatalf("admitFeedback did not grow the corpus: len(exploreCorpus) = %d, want 2 (seed + admitted candidate)", got)
	}

	// Further next() calls must not grow the corpus either — only admitFeedback does.
	before := len(seq.exploreCorpus)
	if _, ok := seq.next(); !ok {
		t.Fatal("expected a second candidate")
	}
	if got := len(seq.exploreCorpus); got != before {
		t.Fatalf("a later next() grew the corpus: len(exploreCorpus) = %d, want %d (unchanged)", got, before)
	}
}

func TestPathSequenceCoverageEmitsExactlyRequestedIterations(t *testing.T) {
	const iterations = 8
	gen := newPathGenerator([]PathVar{
		{Name: "x", rangeSpec: &intRange{min: 0, max: 100}},
	}, nil, 0, 0, false, 0, iterations, 0)

	got := collectSequence(gen)
	if len(got) != iterations {
		t.Fatalf("len(got) = %d, want %d", len(got), iterations)
	}
}

func TestPathSequenceCoverageRespectsFilters(t *testing.T) {
	gen := newPathGenerator([]PathVar{
		{Name: "x", rangeSpec: &intRange{min: 0, max: 20}},
	}, []PathFilter{func(pv PathValues) bool {
		v, _ := pv.lookup("x")
		return v.(int)%2 == 0
	}}, 0, 0, false, 0, 7, 0)

	seq := gen.sequence()
	for i := 0; i < 7; i++ {
		pv, ok := seq.next()
		if !ok {
			t.Fatalf("expected candidate %d, got exhaustion", i)
		}
		if pv.Int("x")%2 != 0 {
			t.Fatalf("expected even x, got %d", pv.Int("x"))
		}
	}
	if _, ok := seq.next(); ok {
		t.Fatal("expected exhaustion after 7 accepted candidates")
	}
}

func TestPathSequenceCoverageBoundsIsExact(t *testing.T) {
	gen := newPathGenerator([]PathVar{
		{Name: "x", rangeSpec: &intRange{min: 0, max: 100}},
	}, nil, 0, 0, false, 0, 9, 0)

	maxAttempts, maxAccepted, maxRejections := gen.bounds()
	if maxAttempts != 9 || maxAccepted != 9 || maxRejections != 9 {
		t.Fatalf("bounds = (%d, %d, %d), want (9, 9, 9)", maxAttempts, maxAccepted, maxRejections)
	}
	if got := len(collectSequence(gen)); got != maxAccepted {
		t.Fatalf("actual accepted candidates = %d, want exactly %d", got, maxAccepted)
	}
}

// TestPathSequenceCoverageCorpusGrowsOnlyAfterAdmitFeedback is #54 PR B's core invariant for
// strategyCoverage: nextGuided must never grow the CoverageExplorer's corpus itself, and growth
// through admitFeedback must be driven by genuine coverage novelty (CoverageExplorer.Feedback +
// Coverage.HasNewCoverage) rather than merely "an admitFeedback call happened" — so this uses real
// synthetic Coverage values built with Coverage.Hit, the same primitive real assertions record
// coverage through in production (see Context.RecordCoverage).
func TestPathSequenceCoverageCorpusGrowsOnlyAfterAdmitFeedback(t *testing.T) {
	gen := newPathGenerator([]PathVar{
		{Name: "x", rangeSpec: &intRange{min: 0, max: 50}},
	}, nil, 0, 7, true, 0, 6, 0)

	seq := gen.sequence()
	pv, ok := seq.next()
	if !ok {
		t.Fatal("expected a candidate")
	}
	if got := gen.coverageExplorer.CorpusLen(); got != 0 {
		t.Fatalf("next() grew the corpus: CorpusLen() = %d, want 0 (before any admitFeedback)", got)
	}

	novel := &Coverage{}
	novel.Hit(1)
	seq.admitFeedback(pv, true, novel)
	if got := gen.coverageExplorer.CorpusLen(); got != 1 {
		t.Fatalf("admitFeedback with novel coverage did not grow the corpus: CorpusLen() = %d, want 1", got)
	}

	pv2, ok := seq.next()
	if !ok {
		t.Fatal("expected a second candidate")
	}
	seq.admitFeedback(pv2, true, novel)
	if got := gen.coverageExplorer.CorpusLen(); got != 1 {
		t.Fatalf("admitFeedback with already-seen coverage grew the corpus: CorpusLen() = %d, want 1 (unchanged)", got)
	}

	pv3, ok := seq.next()
	if !ok {
		t.Fatal("expected a third candidate")
	}
	seq.admitFeedback(pv3, true, nil)
	if got := gen.coverageExplorer.CorpusLen(); got != 1 {
		t.Fatalf("admitFeedback with nil coverage grew the corpus: CorpusLen() = %d, want 1 (unchanged, nil-safe)", got)
	}
}

func TestPathSequenceSmartEmitsExactlyRequestedIterations(t *testing.T) {
	const iterations = 8
	gen := newPathGenerator([]PathVar{
		{Name: "x", rangeSpec: &intRange{min: 0, max: 100}},
	}, nil, 0, 0, false, 0, 0, iterations)

	got := collectSequence(gen)
	if len(got) != iterations {
		t.Fatalf("len(got) = %d, want %d", len(got), iterations)
	}
}

func TestPathSequenceSmartRespectsFilters(t *testing.T) {
	gen := newPathGenerator([]PathVar{
		{Name: "x", rangeSpec: &intRange{min: 0, max: 20}},
	}, []PathFilter{func(pv PathValues) bool {
		v, _ := pv.lookup("x")
		return v.(int)%2 == 0
	}}, 0, 0, false, 0, 0, 7)

	seq := gen.sequence()
	for i := 0; i < 7; i++ {
		pv, ok := seq.next()
		if !ok {
			t.Fatalf("expected candidate %d, got exhaustion", i)
		}
		if pv.Int("x")%2 != 0 {
			t.Fatalf("expected even x, got %d", pv.Int("x"))
		}
	}
	if _, ok := seq.next(); ok {
		t.Fatal("expected exhaustion after 7 accepted candidates")
	}
}

func TestPathSequenceSmartBoundsIsExact(t *testing.T) {
	gen := newPathGenerator([]PathVar{
		{Name: "x", rangeSpec: &intRange{min: 0, max: 100}},
	}, nil, 0, 0, false, 0, 0, 9)

	maxAttempts, maxAccepted, maxRejections := gen.bounds()
	if maxAttempts != 9 || maxAccepted != 9 || maxRejections != 9 {
		t.Fatalf("bounds = (%d, %d, %d), want (9, 9, 9)", maxAttempts, maxAccepted, maxRejections)
	}
	if got := len(collectSequence(gen)); got != maxAccepted {
		t.Fatalf("actual accepted candidates = %d, want exactly %d", got, maxAccepted)
	}
}

// TestPathSequenceSmartCorpusGrowsOnlyAfterAdmitFeedback mirrors the Coverage case for
// strategySmart: nextGuided must never grow the SmartExplorer's corpus itself, and growth through
// admitFeedback must be driven by genuine coverage novelty (SmartExplorer.Feedback +
// Coverage.HasNewCoverage), not merely "an admitFeedback call happened."
func TestPathSequenceSmartCorpusGrowsOnlyAfterAdmitFeedback(t *testing.T) {
	gen := newPathGenerator([]PathVar{
		{Name: "x", rangeSpec: &intRange{min: 0, max: 50}},
	}, nil, 0, 7, true, 0, 0, 6)

	seq := gen.sequence()
	pv, ok := seq.next()
	if !ok {
		t.Fatal("expected a candidate")
	}
	if got := gen.smartExplorer.CorpusLen(); got != 0 {
		t.Fatalf("next() grew the corpus: CorpusLen() = %d, want 0 (before any admitFeedback)", got)
	}

	novel := &Coverage{}
	novel.Hit(1)
	seq.admitFeedback(pv, true, novel)
	if got := gen.smartExplorer.CorpusLen(); got != 1 {
		t.Fatalf("admitFeedback with novel coverage did not grow the corpus: CorpusLen() = %d, want 1", got)
	}

	pv2, ok := seq.next()
	if !ok {
		t.Fatal("expected a second candidate")
	}
	seq.admitFeedback(pv2, true, novel)
	if got := gen.smartExplorer.CorpusLen(); got != 1 {
		t.Fatalf("admitFeedback with already-seen coverage grew the corpus: CorpusLen() = %d, want 1 (unchanged)", got)
	}

	pv3, ok := seq.next()
	if !ok {
		t.Fatal("expected a third candidate")
	}
	seq.admitFeedback(pv3, true, nil)
	if got := gen.smartExplorer.CorpusLen(); got != 1 {
		t.Fatalf("admitFeedback with nil coverage grew the corpus: CorpusLen() = %d, want 1 (unchanged, nil-safe)", got)
	}
}

// TestPathSequenceExplorePanicsWhenFiltersCannotBeSatisfied is #18's regression: before the
// attempt budget, an impossible filter made nextExplore's internal retry loop spin forever
// instead of failing. Same shape as TestPathSequenceSamplePanicsWhenFiltersCannotBeSatisfied.
func TestPathSequenceExplorePanicsWhenFiltersCannotBeSatisfied(t *testing.T) {
	gen := newPathGenerator([]PathVar{
		{Name: "x", Values: []any{1}},
	}, []PathFilter{func(PathValues) bool { return false }}, 0, 0, false, 3, 0, 0)

	seq := gen.sequence()
	assertPanicsWith(t, func() { seq.next() }, "Explore", "attempts", "iterations")
}

func TestPathSequenceCoveragePanicsWhenFiltersCannotBeSatisfied(t *testing.T) {
	gen := newPathGenerator([]PathVar{
		{Name: "x", Values: []any{1}},
	}, []PathFilter{func(PathValues) bool { return false }}, 0, 0, false, 0, 3, 0)

	seq := gen.sequence()
	assertPanicsWith(t, func() { seq.next() }, "ExploreCoverage", "attempts", "iterations")
}

func TestPathSequenceSmartPanicsWhenFiltersCannotBeSatisfied(t *testing.T) {
	gen := newPathGenerator([]PathVar{
		{Name: "x", Values: []any{1}},
	}, []PathFilter{func(PathValues) bool { return false }}, 0, 0, false, 0, 0, 3)

	seq := gen.sequence()
	assertPanicsWith(t, func() { seq.next() }, "ExploreSmart", "attempts", "iterations")
}

func assertPanics(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("expected a panic")
		}
	}()
	fn()
}

// assertPanicsWith is assertPanics plus a check that the panic message names the diagnostics
// #18 requires: which strategy hit the budget, and that it's about attempts/iterations, not a
// bare failure with no actionable detail.
func assertPanicsWith(t *testing.T, fn func(), wantSubstrings ...string) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected a panic")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("expected a string panic message, got %T: %v", r, r)
		}
		for _, want := range wantSubstrings {
			if !strings.Contains(msg, want) {
				t.Fatalf("panic message %q missing expected substring %q", msg, want)
			}
		}
	}()
	fn()
}
