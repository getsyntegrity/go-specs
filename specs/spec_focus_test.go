// spec_focus_test.go pins issue #245's T2: FIt on *Spec must filter the whole Describe/BuildSuite
// tree the way Builder.FIt/finalize already does (builder.go): once any FIt is registered, only
// focused specs compile, every other It/SkipIt/PendingIt in the same call is dropped, and hooks
// (BeforeEach/AfterEach, BeforeAll/AfterAll) around a focused spec still run. Both Spec build paths
// are covered: the bytecode compiler and the Analyze/registry path.
package specs

import (
	"testing"
)

// TestSpecFItOnlyFocusedRuns_CompilerPath pins the core filter on the bytecode-compiler path: with
// one FIt present, only it runs; a plain It and a SkipIt/PendingIt in the same tree are dropped.
func TestSpecFItOnlyFocusedRuns_CompilerPath(t *testing.T) {
	var ran []string
	suite := BuildSuite(nil, "suite", func(s *Spec) {
		s.It("plain", func(*Context) { ran = append(ran, "plain") })
		s.FIt("focused", func(*Context) { ran = append(ran, "focused") })
		s.SkipIt("skip", func(*Context) { ran = append(ran, "skip") })
		s.PendingIt("pending", func(*Context) { ran = append(ran, "pending") })
	})
	suite.Run(t)
	if got := ran; len(got) != 1 || got[0] != "focused" {
		t.Fatalf("ran = %v, want only [focused]", got)
	}
}

// TestSpecFItOnlyFocusedRuns_RegistryPath is the Analyze/registry-path equivalent.
func TestSpecFItOnlyFocusedRuns_RegistryPath(t *testing.T) {
	var ran []string
	var suite *CompiledSuite
	Analyze(func() {
		suite = BuildSuite(nil, "suite", func(s *Spec) {
			s.It("plain", func(*Context) { ran = append(ran, "plain") })
			s.FIt("focused", func(*Context) { ran = append(ran, "focused") })
			s.SkipIt("skip", func(*Context) { ran = append(ran, "skip") })
			s.PendingIt("pending", func(*Context) { ran = append(ran, "pending") })
		})
	})
	suite.Run(t)
	if got := ran; len(got) != 1 || got[0] != "focused" {
		t.Fatalf("ran = %v, want only [focused] (registry path)", got)
	}
}

// TestSpecFItDropsUnfocusedFromReport pins that an unfocused spec is not just unrun but never
// reported at all — matching Builder's finalize (which removes it from items before groups are
// built), not merely skipped.
func TestSpecFItDropsUnfocusedFromReport(t *testing.T) {
	rep := &recordingReporter{}
	suite := BuildSuite(nil, "suite", func(s *Spec) {
		s.It("plain", func(*Context) {})
		s.FIt("focused", func(*Context) {})
	})
	suite.Reporter = rep
	suite.Run(t)

	if len(rep.specFinished) != 1 {
		t.Fatalf("expected exactly one reported spec, got %d: %+v", len(rep.specFinished), rep.specFinished)
	}
	if rep.specFinished[0].Name != "focused" {
		t.Errorf("Name = %q, want %q", rep.specFinished[0].Name, "focused")
	}
}

// TestSpecFItNilFnIsNoOp pins that FIt("x", nil) registers nothing at all (Builder.FIt parity), on
// both build paths.
func TestSpecFItNilFnIsNoOp(t *testing.T) {
	otherRan := false
	suite := BuildSuite(nil, "suite", func(s *Spec) {
		s.FIt("nil focus", nil)
		s.It("plain", func(*Context) { otherRan = true })
	})
	suite.Run(t)
	if !otherRan {
		t.Fatal("FIt(name, nil) must not suppress an unrelated plain It")
	}

	var analyzed *CompiledSuite
	analyzedRan := false
	Analyze(func() {
		analyzed = BuildSuite(nil, "suite", func(s *Spec) {
			s.FIt("nil focus", nil)
			s.It("plain", func(*Context) { analyzedRan = true })
		})
	})
	analyzed.Run(t)
	if !analyzedRan {
		t.Fatal("FIt(name, nil) must not suppress an unrelated plain It (registry path)")
	}
}

// TestSpecFItHooksAroundFocusedSpecStillRun pins that BeforeEach/AfterEach/BeforeAll/AfterAll
// enclosing a focused spec still run, on both build paths (issue #245's explicit requirement).
func TestSpecFItHooksAroundFocusedSpecStillRun(t *testing.T) {
	run := func(t *testing.T, build func(fn func(*Spec)) *CompiledSuite) {
		t.Helper()
		var order []string
		suite := build(func(s *Spec) {
			s.BeforeAll(func(*Context) { order = append(order, "beforeAll") })
			s.AfterAll(func(*Context) { order = append(order, "afterAll") })
			s.BeforeEach(func(*Context) { order = append(order, "beforeEach") })
			s.AfterEach(func(*Context) { order = append(order, "afterEach") })
			s.It("plain", func(*Context) { order = append(order, "plain") })
			s.FIt("focused", func(*Context) { order = append(order, "focused") })
		})
		suite.Run(t)
		want := []string{"beforeAll", "beforeEach", "focused", "afterEach", "afterAll"}
		if len(order) != len(want) {
			t.Fatalf("order = %v, want %v", order, want)
		}
		for i := range want {
			if order[i] != want[i] {
				t.Fatalf("order = %v, want %v", order, want)
			}
		}
	}

	t.Run("compiler path", func(t *testing.T) {
		run(t, func(fn func(*Spec)) *CompiledSuite { return BuildSuite(nil, "suite", fn) })
	})
	t.Run("registry path", func(t *testing.T) {
		var suite *CompiledSuite
		run(t, func(fn func(*Spec)) *CompiledSuite {
			Analyze(func() { suite = BuildSuite(nil, "suite", fn) })
			return suite
		})
	})
}

// TestSpecFItGroupWithZeroSpecsAfterFocusIsNeverEntered pins this feature's documented choice for
// a BeforeAll/AfterAll group whose only specs are filtered out by a focus elsewhere in the suite:
// it behaves exactly like a group declaring no It at all (H3, docs/SUITE_HOOKS_CONTRACT.md) — never
// entered, so neither its BeforeAll nor its AfterAll runs. See the feature doc's "semantics
// decision" note for why this, rather than some other behavior, was chosen.
func TestSpecFItGroupWithZeroSpecsAfterFocusIsNeverEntered(t *testing.T) {
	run := func(t *testing.T, build func(fn func(*Spec)) *CompiledSuite) {
		t.Helper()
		hookRan := false
		var focusedRan bool
		suite := build(func(s *Spec) {
			s.When("emptied by focus", func(w *Spec) {
				w.BeforeAll(func(*Context) { hookRan = true })
				w.AfterAll(func(*Context) { hookRan = true })
				w.It("filtered out", func(*Context) {})
			})
			s.FIt("elsewhere", func(*Context) { focusedRan = true })
		})
		suite.Run(t)
		if !focusedRan {
			t.Fatal("the focused spec outside the group must still run")
		}
		if hookRan {
			t.Fatal("a group left with zero runnable specs after focus filtering must never be entered (H3)")
		}
	}

	t.Run("compiler path", func(t *testing.T) {
		run(t, func(fn func(*Spec)) *CompiledSuite { return BuildSuite(nil, "suite", fn) })
	})
	t.Run("registry path", func(t *testing.T) {
		var suite *CompiledSuite
		run(t, func(fn func(*Spec)) *CompiledSuite {
			Analyze(func() { suite = BuildSuite(nil, "suite", fn) })
			return suite
		})
	})
}

// TestSpecFItGroupSurvivingFocusIsStillEntered is the converse of the test above: a group with a
// mix of a focused and an unfocused spec keeps at least one runnable spec after filtering, so its
// BeforeAll/AfterAll still run exactly once, around the surviving focused spec.
func TestSpecFItGroupSurvivingFocusIsStillEntered(t *testing.T) {
	run := func(t *testing.T, build func(fn func(*Spec)) *CompiledSuite) {
		t.Helper()
		var order []string
		suite := build(func(s *Spec) {
			s.When("partially focused", func(w *Spec) {
				w.BeforeAll(func(*Context) { order = append(order, "beforeAll") })
				w.AfterAll(func(*Context) { order = append(order, "afterAll") })
				w.It("dropped", func(*Context) { order = append(order, "dropped") })
				w.FIt("kept", func(*Context) { order = append(order, "kept") })
			})
		})
		suite.Run(t)
		want := []string{"beforeAll", "kept", "afterAll"}
		if len(order) != len(want) {
			t.Fatalf("order = %v, want %v", order, want)
		}
		for i := range want {
			if order[i] != want[i] {
				t.Fatalf("order = %v, want %v", order, want)
			}
		}
	}

	t.Run("compiler path", func(t *testing.T) {
		run(t, func(fn func(*Spec)) *CompiledSuite { return BuildSuite(nil, "suite", fn) })
	})
	t.Run("registry path", func(t *testing.T) {
		var suite *CompiledSuite
		run(t, func(fn func(*Spec)) *CompiledSuite {
			Analyze(func() { suite = BuildSuite(nil, "suite", fn) })
			return suite
		})
	})
}

// TestSpecFItScopedToOneDescribeCall pins that focus is scoped to one top-level Describe/BuildSuite
// call, not process-wide: a focus registered in one suite must not filter an unrelated suite built
// separately, on both build paths.
func TestSpecFItScopedToOneDescribeCall(t *testing.T) {
	var otherRan bool
	unrelated := BuildSuite(nil, "unrelated", func(s *Spec) {
		s.It("runs regardless", func(*Context) { otherRan = true })
	})
	focused := BuildSuite(nil, "focused suite", func(s *Spec) {
		s.It("dropped here", func(*Context) {})
		s.FIt("kept here", func(*Context) {})
	})
	unrelated.Run(t)
	focused.Run(t)
	if !otherRan {
		t.Fatal("an unrelated suite must not be filtered by another suite's FIt")
	}
}

// TestSpecFItOnSpecWithNoBuildTargetPanics mirrors Spec.It's requireBuildTarget contract.
func TestSpecFItOnSpecWithNoBuildTargetPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected FIt on a build-target-less Spec to panic")
		}
	}()
	(&Spec{}).FIt("x", func(*Context) {})
}
