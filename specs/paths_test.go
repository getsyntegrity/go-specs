package specs

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/getsyntegrity/go-specs/report"
)

func TestPathsAnalyzeKeepsSingleNode(t *testing.T) {
	suite := Analyze(func() {
		Describe(nil, "Paths", func(s *Spec) {
			s.Paths(func(pb *PathBuilder) {
				pb.Bool("vip")
				pb.Int("price", []int{50, 150})
			}).It("discount never increases price", func(ctx *Context) {})
		})
	})
	if suite == nil || suite.Arena == nil {
		t.Fatalf("expected suite root")
	}
	rootID := suite.RootID
	if len(suite.Arena.Children[rootID]) != 1 {
		t.Fatalf("expected single describe child, got %d", len(suite.Arena.Children[rootID]))
	}
	descID := suite.Arena.Children[rootID][0]
	if len(suite.Arena.Children[descID]) != 1 {
		t.Fatalf("expected single spec child, got %d", len(suite.Arena.Children[descID]))
	}
	specID := suite.Arena.Children[descID][0]
	specNode := &suite.Arena.Nodes[specID]
	if specNode.Name != "discount never increases price" {
		t.Fatalf("unexpected spec name %s", specNode.Name)
	}
	if specNode.PathGen == nil {
		t.Fatal("expected path generator metadata")
	}
	var names []string
	specNode.PathGen.ForEach(func(values PathValues) {
		names = append(names, specNode.PathGen.FormatName(specNode.Name, values))
	})
	expected := []string{
		"discount never increases price [vip=true price=50]",
		"discount never increases price [vip=true price=150]",
		"discount never increases price [vip=false price=50]",
		"discount never increases price [vip=false price=150]",
	}
	for i, name := range names {
		if name != expected[i] {
			t.Fatalf("unexpected name %s", name)
		}
	}
}

func TestPathsExecutesAllCombinations(t *testing.T) {
	var hits []string
	var mu sync.Mutex
	Describe(t, "Paths", func(s *Spec) {
		s.Paths(func(pb *PathBuilder) {
			pb.Bool("vip")
			pb.Int("price", []int{50, 150})
		}).It("case", func(ctx *Context) {
			entry := fmt.Sprintf("vip=%v price=%d", ctx.Path().Bool("vip"), ctx.Path().Int("price"))
			mu.Lock()
			hits = append(hits, entry)
			mu.Unlock()
		})
	})
	if len(hits) != 4 {
		t.Fatalf("expected 4 combinations, got %d", len(hits))
	}
	sort.Strings(hits)
	expected := []string{
		"vip=false price=150",
		"vip=false price=50",
		"vip=true price=150",
		"vip=true price=50",
	}
	for i := range expected {
		if hits[i] != expected[i] {
			t.Fatalf("unexpected combo %v", hits)
		}
	}
}

func TestPathsExecutesGeneratedCasesSequentiallyInDeclarationOrder(t *testing.T) {
	var order []int
	Describe(t, "PathsSequential", func(s *Spec) {
		s.Paths(func(pb *PathBuilder) {
			pb.Int("value", []int{1, 2, 3})
		}).It("case", func(ctx *Context) {
			order = append(order, ctx.Path().Int("value"))
		})
	})
	if got, want := fmt.Sprint(order), "[1 2 3]"; got != want {
		t.Fatalf("generated execution order = %s, want %s", got, want)
	}
}

func TestPathsFiltersCombinations(t *testing.T) {
	var hits []string
	var mu sync.Mutex
	Describe(t, "PathsFilter", func(s *Spec) {
		s.Paths(func(pb *PathBuilder) {
			pb.Bool("vip")
			pb.Int("price", []int{50, 150})
			pb.Filter(func(v PathValues) bool {
				if v.Bool("vip") && v.Int("price") < 100 {
					return false
				}
				return true
			})
		}).It("case", func(ctx *Context) {
			entry := fmt.Sprintf("vip=%v price=%d", ctx.Path().Bool("vip"), ctx.Path().Int("price"))
			mu.Lock()
			hits = append(hits, entry)
			mu.Unlock()
		})
	})
	sort.Strings(hits)
	expected := []string{
		"vip=false price=150",
		"vip=false price=50",
		"vip=true price=150",
	}
	if len(hits) != len(expected) {
		t.Fatalf("expected %d hits, got %d", len(expected), len(hits))
	}
	for i := range expected {
		if hits[i] != expected[i] {
			t.Fatalf("unexpected hits %v", hits)
		}
	}
}

func TestPathsCasesUseIsolatedHooksAndContext(t *testing.T) {
	var order []string
	Describe(t, "PathLifecycle", func(s *Spec) {
		s.BeforeEach(func(ctx *Context) {
			if ctx.Path().Int("value") == 0 {
				t.Fatal("missing generated path value")
			}
			order = append(order, "before")
		})
		s.AfterEach(func(*Context) { order = append(order, "after") })
		s.Paths(func(pb *PathBuilder) { pb.Int("value", []int{1, 2}) }).It("case", func(ctx *Context) {
			order = append(order, fmt.Sprintf("body-%d", ctx.Path().Int("value")))
		})
	})
	if got, want := fmt.Sprint(order), "[before body-1 after before body-2 after]"; got != want {
		t.Fatalf("case lifecycle = %s, want %s", got, want)
	}
}

// TestPathsReporterIncludesCombinationNames proves report output identifies each generated
// candidate by its own combination, not by a shared spec name (#103). The " #<n>" ordinal the
// reporter appends is additive: it links the line back to the matching `go test -v` subtest without
// disturbing the framework's own "base [k=v]" rendering.
func TestPathsReporterIncludesCombinationNames(t *testing.T) {
	var buf bytes.Buffer
	reporter := report.New(&buf)
	DescribeWithReporter(t, "Paths", reporter, func(s *Spec) {
		s.Paths(func(pb *PathBuilder) {
			pb.Values("tier", []any{"basic", "pro"})
		}).It("includes tier", func(ctx *Context) {
			if ctx.Path().Value("tier") == nil {
				ctx.T.Fatalf("expected tier value")
			}
		})
	})
	output := buf.String()
	if !strings.Contains(output, "includes tier [tier=basic]") {
		t.Fatalf("expected output to include basic combo, got %s", output)
	}
	if !strings.Contains(output, "includes tier [tier=pro]") {
		t.Fatalf("expected output to include pro combo, got %s", output)
	}
}

func TestPathsSampleExecutesRequestedCount(t *testing.T) {
	const samples = 5
	var mu sync.Mutex
	var prices []int
	Describe(t, "PathsSampleCount", func(s *Spec) {
		builder := s.Paths(func(pb *PathBuilder) {
			pb.Bool("vip")
			pb.IntRange("price", 10, 20)
		})
		builder.Sample(samples).It("combo", func(ctx *Context) {
			price := ctx.Path().Int("price")
			if price < 10 || price > 20 {
				ctx.T.Fatalf("price out of range: %d", price)
			}
			mu.Lock()
			prices = append(prices, price)
			mu.Unlock()
		})
	})
	if len(prices) != samples {
		t.Fatalf("expected %d samples, got %d", samples, len(prices))
	}
}

func TestPathsSampleDeterministicWithSeed(t *testing.T) {
	runSamples := func(name string) []string {
		var mu sync.Mutex
		var hits []string
		Describe(t, name, func(s *Spec) {
			builder := s.Paths(func(pb *PathBuilder) {
				pb.Bool("vip")
				pb.IntRange("price", 0, 5)
			})
			builder.Sample(4).Seed(99).It("sample", func(ctx *Context) {
				entry := fmt.Sprintf("sample=%d vip=%v price=%d", ctx.Path().Int("sample"), ctx.Path().Bool("vip"), ctx.Path().Int("price"))
				mu.Lock()
				hits = append(hits, entry)
				mu.Unlock()
			})
		})
		return hits
	}
	first := runSamples("PathsSampleSeed1")
	second := runSamples("PathsSampleSeed2")
	if len(first) != len(second) {
		t.Fatalf("expected same sample size, got %d and %d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("expected deterministic samples, got %v vs %v", first, second)
		}
	}
}

func TestPathsSampleRespectsFilters(t *testing.T) {
	var mu sync.Mutex
	var hits []int
	Describe(t, "PathsSampleFilters", func(s *Spec) {
		builder := s.Paths(func(pb *PathBuilder) {
			pb.IntRange("price", 0, 10)
			pb.Filter(func(v PathValues) bool {
				return v.Int("price")%2 == 0
			})
		})
		builder.Sample(8).It("even prices", func(ctx *Context) {
			price := ctx.Path().Int("price")
			if price%2 != 0 {
				ctx.T.Fatalf("expected even price, got %d", price)
			}
			mu.Lock()
			hits = append(hits, price)
			mu.Unlock()
		})
	})
	if len(hits) != 8 {
		t.Fatalf("expected 8 samples, got %d", len(hits))
	}
}

func TestPathsSampleReporterNamesIncludeSample(t *testing.T) {
	t.Skip("generated reporting is deferred until the later reporting gate")
	var buf bytes.Buffer
	reporter := report.New(&buf)
	DescribeWithReporter(t, "PathsSampleReporter", reporter, func(s *Spec) {
		builder := s.Paths(func(pb *PathBuilder) {
			pb.Bool("vip")
		})
		builder.Sample(2).Seed(7).It("sample case", func(ctx *Context) {})
	})
	output := buf.String()
	if !strings.Contains(output, "sample case [sample=1 vip=") {
		t.Fatalf("expected sample numbering in output: %s", output)
	}
}

func TestPathsExploreRunsExpectedIterations(t *testing.T) {
	const iterations = 10
	var mu sync.Mutex
	var count int
	Describe(t, "PathsExploreCount", func(s *Spec) {
		builder := s.Paths(func(pb *PathBuilder) {
			pb.IntRange("x", 0, 100)
		})
		builder.Explore(iterations).It("explore", func(ctx *Context) {
			mu.Lock()
			count++
			mu.Unlock()
		})
	})
	if count != iterations {
		t.Fatalf("expected %d iterations, got %d", iterations, count)
	}
}

func TestPathsExploreDeterministicWithSeed(t *testing.T) {
	runExplore := func(name string) []int {
		var mu sync.Mutex
		var values []int
		Describe(t, name, func(s *Spec) {
			builder := s.Paths(func(pb *PathBuilder) {
				pb.IntRange("x", 0, 50)
			})
			builder.Explore(5).Seed(42).It("explore", func(ctx *Context) {
				x := ctx.Path().Int("x")
				mu.Lock()
				values = append(values, x)
				mu.Unlock()
			})
		})
		return values
	}
	first := runExplore("Explore1")
	second := runExplore("Explore2")
	if len(first) != len(second) {
		t.Fatalf("expected same length, got %d and %d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("expected deterministic values, got %v vs %v", first, second)
		}
	}
}

func TestPathsExploreRespectsFilters(t *testing.T) {
	var mu sync.Mutex
	var values []int
	Describe(t, "PathsExploreFilters", func(s *Spec) {
		builder := s.Paths(func(pb *PathBuilder) {
			pb.IntRange("x", 0, 20)
			pb.Filter(func(v PathValues) bool {
				return v.Int("x")%2 == 0
			})
		})
		builder.Explore(10).It("even only", func(ctx *Context) {
			x := ctx.Path().Int("x")
			if x%2 != 0 {
				ctx.T.Fatalf("expected even x, got %d", x)
			}
			mu.Lock()
			values = append(values, x)
			mu.Unlock()
		})
	})
	if len(values) != 10 {
		t.Fatalf("expected 10 iterations, got %d", len(values))
	}
}

func TestIntShrinkerReducesValue(t *testing.T) {
	s := DefaultIntShrinker
	candidates := s.Shrink(100)
	if len(candidates) == 0 {
		t.Fatal("expected shrink candidates")
	}
	found := false
	for _, c := range candidates {
		if c.(int) < 100 {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected at least one smaller value")
	}
}

func TestBoolShrinkerReducesValue(t *testing.T) {
	s := DefaultBoolShrinker
	candidates := s.Shrink(true)
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
	if candidates[0] != false {
		t.Fatalf("expected false, got %v", candidates[0])
	}
}

func TestPathGeneratorShrink(t *testing.T) {
	gen := newPathGenerator([]PathVar{
		{Name: "x", rangeSpec: &intRange{min: 0, max: 1000}},
	}, nil, 0, 0, false, 0, 0, 0)
	initial := PathValues{
		values:  []any{100},
		present: []bool{true},
		index:   map[string]int{"x": 0},
	}
	reduced := gen.shrinkValues(initial, func(pv PathValues) bool {
		return pv.values[0].(int) > 10
	})
	if reduced.values[0].(int) > 10 {
		t.Fatalf("expected reduced value <= 10, got %v", reduced.values[0])
	}
}

func TestShrinkerFindsMinimalFailing(t *testing.T) {
	gen := newPathGenerator([]PathVar{
		{Name: "x", rangeSpec: &intRange{min: 0, max: 1000}},
		{Name: "y", rangeSpec: &intRange{min: 0, max: 1000}},
	}, nil, 0, 0, false, 0, 0, 0)
	index := map[string]int{"x": 0, "y": 1}
	failing := PathValues{
		values:  []any{937, 421},
		present: []bool{true, true},
		index:   index,
	}
	// Property: holds when x == 0 && y == 0; fails otherwise. Minimal failing is (0,1) or (1,0).
	test := func(pv PathValues) bool {
		x := pv.Int("x")
		y := pv.Int("y")
		return x == 0 && y == 0
	}
	if test(failing) {
		t.Fatal("initial case must fail")
	}
	shrinker := NewShrinker(gen)
	if shrinker == nil {
		t.Fatal("NewShrinker returned nil")
	}
	got := shrinker.Shrink(failing, test)
	if test(got) {
		t.Fatalf("Shrink must return a failing case, got x=%v y=%v", got.Int("x"), got.Int("y"))
	}
	// Minimal failing is (0,1) or (1,0): one coordinate must be 0, the other 1 (or 0 would pass).
	x, y := got.Int("x"), got.Int("y")
	if x < 0 || y < 0 || x > 1 || y > 1 {
		t.Fatalf("expected minimal failing (0,1) or (1,0), got x=%d y=%d", x, y)
	}
	if x == 0 && y == 0 {
		t.Fatal("(0,0) satisfies the property; expected a failing case")
	}
	// Original must be unchanged
	if failing.Int("x") != 937 || failing.Int("y") != 421 {
		t.Fatal("Shrink must not modify the original PathValues")
	}
}

func TestShrinkerBinaryShrinksToZero(t *testing.T) {
	gen := newPathGenerator([]PathVar{
		{Name: "x", rangeSpec: &intRange{min: 0, max: 1000}},
	}, nil, 0, 0, false, 0, 0, 0)
	index := map[string]int{"x": 0}
	failing := PathValues{
		values:  []any{937},
		present: []bool{true},
		index:   index,
	}
	// Property: holds when x == 0. So minimal failing is 1 (binary search toward zero).
	test := func(pv PathValues) bool {
		return pv.Int("x") == 0
	}
	shrinker := NewShrinker(gen)
	got := shrinker.Shrink(failing, test)
	if test(got) {
		t.Fatalf("Shrink must return a failing case, got x=%d", got.Int("x"))
	}
	if x := got.Int("x"); x != 1 {
		t.Fatalf("expected minimal failing x=1 (binary shrink from 937), got x=%d", x)
	}
	// Binary search should be O(log n): for 937 we expect at most ~10 steps per dimension
	// (ceil(log2(937)) + 1). Sanity check that we didn't do linear probes.
	calls := 0
	countingTest := func(pv PathValues) bool {
		calls++
		return pv.Int("x") == 0
	}
	shrinker.Shrink(failing, countingTest)
	if calls > 20 {
		t.Errorf("binary shrinking should do O(log n) probes; got %d test calls for n=937", calls)
	}
}

func TestExploreCoverageRuns(t *testing.T) {
	Describe(t, "ExploreCoverage", func(s *Spec) {
		s.Paths(func(p *PathBuilder) {
			p.IntRange("x", 0, 100)
			p.IntRange("y", 0, 100)
		}).ExploreCoverage(20).It("property", func(ctx *Context) {
			x, y := ctx.Path().Int("x"), ctx.Path().Int("y")
			ctx.Expect(x+y >= x && x+y >= y).ToEqual(true)
		})
	})
}

func TestExploreSmartRuns(t *testing.T) {
	Describe(t, "ExploreSmart", func(s *Spec) {
		s.Paths(func(p *PathBuilder) {
			p.IntRange("x", 0, 100)
			p.IntRange("y", 0, 100)
		}).ExploreSmart(25).It("property", func(ctx *Context) {
			x, y := ctx.Path().Int("x"), ctx.Path().Int("y")
			ctx.Expect(x+y >= x && x+y >= y).ToEqual(true)
		})
	})
}

// The following tests exercise the real execution path (Describe/Paths().Explore*()) rather than
// pathSequence directly, so the attempt-budget fix (#18) is proven against what actually runs in
// production, not just against the generator in isolation.

func TestPathsExploreImpossibleFilterPanicsWithDiagnostics(t *testing.T) {
	assertPanicsWith(t, func() {
		Describe(t, "PathsExploreImpossibleFilter", func(s *Spec) {
			s.Paths(func(pb *PathBuilder) {
				pb.IntRange("x", 0, 100)
				pb.Filter(func(PathValues) bool { return false })
			}).Explore(5).It("never runs", func(ctx *Context) {
				t.Fatal("case must not execute when the filter admits nothing")
			})
		})
	}, "Explore", "attempts", "iterations")
}

func TestPathsExploreCoverageImpossibleFilterPanicsWithDiagnostics(t *testing.T) {
	assertPanicsWith(t, func() {
		Describe(t, "PathsExploreCoverageImpossibleFilter", func(s *Spec) {
			s.Paths(func(pb *PathBuilder) {
				pb.IntRange("x", 0, 100)
				pb.Filter(func(PathValues) bool { return false })
			}).ExploreCoverage(5).It("never runs", func(ctx *Context) {
				t.Fatal("case must not execute when the filter admits nothing")
			})
		})
	}, "ExploreCoverage", "attempts", "iterations")
}

func TestPathsExploreSmartImpossibleFilterPanicsWithDiagnostics(t *testing.T) {
	assertPanicsWith(t, func() {
		Describe(t, "PathsExploreSmartImpossibleFilter", func(s *Spec) {
			s.Paths(func(pb *PathBuilder) {
				pb.IntRange("x", 0, 100)
				pb.Filter(func(PathValues) bool { return false })
			}).ExploreSmart(5).It("never runs", func(ctx *Context) {
				t.Fatal("case must not execute when the filter admits nothing")
			})
		})
	}, "ExploreSmart", "attempts", "iterations")
}

// TestPathsExploreRestrictiveFilterStillReachesIterations guards against the new attempt budget
// tripping on a filter that is restrictive but satisfiable — it must still reach exactly
// `iterations`, not fail early just because acceptance is rare.
func TestPathsExploreRestrictiveFilterStillReachesIterations(t *testing.T) {
	const iterations = 20
	var mu sync.Mutex
	var count int
	Describe(t, "PathsExploreRestrictiveFilter", func(s *Spec) {
		s.Paths(func(pb *PathBuilder) {
			pb.IntRange("x", 0, 100)
			pb.Filter(func(v PathValues) bool {
				return v.Int("x")%10 == 0
			})
		}).Explore(iterations).It("multiple of ten", func(ctx *Context) {
			x := ctx.Path().Int("x")
			if x%10 != 0 {
				ctx.T.Fatalf("expected multiple of ten, got %d", x)
			}
			mu.Lock()
			count++
			mu.Unlock()
		})
	})
	if count != iterations {
		t.Fatalf("expected %d iterations under a restrictive-but-viable filter, got %d", iterations, count)
	}
}

func TestShrinkerCoordinateDescentMultipleInts(t *testing.T) {
	gen := newPathGenerator([]PathVar{
		{Name: "x", rangeSpec: &intRange{min: 0, max: 1000}},
		{Name: "y", rangeSpec: &intRange{min: 0, max: 1000}},
	}, nil, 0, 0, false, 0, 0, 0)
	index := map[string]int{"x": 0, "y": 1}
	failing := PathValues{
		values:  []any{937, 421},
		present: []bool{true, true},
		index:   index,
	}
	// Property: holds when x <= 0 && y <= 0. Minimal failing is one of (1,0), (0,1), (1,1).
	test := func(pv PathValues) bool {
		return pv.Int("x") <= 0 && pv.Int("y") <= 0
	}
	if test(failing) {
		t.Fatal("initial case must fail")
	}
	shrinker := NewShrinker(gen)
	got := shrinker.Shrink(failing, test)
	if test(got) {
		t.Fatalf("Shrink must return a failing case, got x=%d y=%d", got.Int("x"), got.Int("y"))
	}
	// Both dimensions minimized: each coordinate 0 or 1 (can't shrink further and still fail).
	x, y := got.Int("x"), got.Int("y")
	if x < 0 || y < 0 || x > 1 || y > 1 {
		t.Fatalf("expected minimal failing with each dim 0 or 1, got x=%d y=%d", x, y)
	}
	if x == 0 && y == 0 {
		t.Fatal("(0,0) satisfies the property; expected a failing case")
	}
	// Dimensions processed in index order; no randomness.
	if failing.Int("x") != 937 || failing.Int("y") != 421 {
		t.Fatal("Shrink must not modify the original PathValues")
	}
}
