package specs

// explore_mode_selection_test.go covers #16: ExploreCoverage(n)/ExploreSmart(n) must select
// ExplorationGuided mode and use CoverageExplorer/SmartExplorer, on their own — without also
// calling .Explore(). Uses newPathGenerator + ForEach directly rather than a top-level Describe,
// since path-combinatorial execution isn't wired into the top-level Describe flow yet (the 8 tests
// skipped "paths combinatorial execution with top-level Describe deferred to post-v1.0.0" in
// paths_test.go); that's a separate, larger, already-acknowledged gap, tracked independently.

import "testing"

func TestExploreCoverage_SelectsGuidedModeAndCoverageExplorerAlone(t *testing.T) {
	const iterations = 12
	gen := newPathGenerator([]PathVar{{rangeSpec: &intRange{min: 0, max: 100}, Name: "x"}}, nil, 0, 1, true, 0, iterations, 0)

	if gen.mode != ExplorationGuided {
		t.Fatalf("expected ExplorationGuided mode from ExploreCoverage alone, got %v", gen.mode)
	}
	if gen.iterations != iterations {
		t.Fatalf("expected %d iterations, got %d", iterations, gen.iterations)
	}
	if gen.strategy != strategyCoverage {
		t.Fatalf("expected strategyCoverage, got %v", gen.strategy)
	}
	if gen.coverageExplorer == nil {
		t.Fatal("expected a non-nil CoverageExplorer")
	}

	var runs int
	gen.ForEach(func(PathValues) { runs++ })
	if runs != iterations {
		t.Errorf("expected ForEach to invoke fn %d times, got %d", iterations, runs)
	}
	if gen.coverageExplorer.CorpusLen() == 0 {
		t.Error("expected the CoverageExplorer's corpus to have grown during exploration")
	}
}

func TestExploreSmart_SelectsGuidedModeAndSmartExplorerAlone(t *testing.T) {
	const iterations = 12
	gen := newPathGenerator([]PathVar{{rangeSpec: &intRange{min: 0, max: 100}, Name: "x"}}, nil, 0, 1, true, 0, 0, iterations)

	if gen.mode != ExplorationGuided {
		t.Fatalf("expected ExplorationGuided mode from ExploreSmart alone, got %v", gen.mode)
	}
	if gen.iterations != iterations {
		t.Fatalf("expected %d iterations, got %d", iterations, gen.iterations)
	}
	if gen.strategy != strategySmart {
		t.Fatalf("expected strategySmart, got %v", gen.strategy)
	}
	if gen.smartExplorer == nil {
		t.Fatal("expected a non-nil SmartExplorer")
	}

	var runs int
	gen.ForEach(func(PathValues) { runs++ })
	if runs != iterations {
		t.Errorf("expected ForEach to invoke fn %d times, got %d", iterations, runs)
	}
}

// TestExplore_StillSelectsPlainStrategy guards against the fix regressing plain .Explore(n).
func TestExplore_StillSelectsPlainStrategy(t *testing.T) {
	const iterations = 8
	gen := newPathGenerator([]PathVar{{rangeSpec: &intRange{min: 0, max: 100}, Name: "x"}}, nil, 0, 1, true, iterations, 0, 0)

	if gen.mode != ExplorationGuided {
		t.Fatalf("expected ExplorationGuided mode from Explore, got %v", gen.mode)
	}
	if gen.strategy != strategyPlain {
		t.Fatalf("expected strategyPlain, got %v", gen.strategy)
	}
	if gen.coverageExplorer != nil || gen.smartExplorer != nil {
		t.Error("plain Explore should not construct a CoverageExplorer/SmartExplorer")
	}

	var runs int
	gen.ForEach(func(PathValues) { runs++ })
	if runs != iterations {
		t.Errorf("expected ForEach to invoke fn %d times, got %d", iterations, runs)
	}
}
