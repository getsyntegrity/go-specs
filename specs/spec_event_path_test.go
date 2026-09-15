package specs

import (
	"slices"
	"testing"
)

// TestSpecStartEventPathKeepsSeparatorInsideDeclaredName proves SpecStartEvent.Path reports the
// scopes that were actually declared, even when one declared name contains the same "/" that
// FullNames uses as its scope separator.
//
// Path used to be rebuilt by splitting the joined breadcrumb, so "slash/inside" arrived as two
// segments and invented a scope nobody wrote — a reporter deriving a tree, or a JUnit classname
// from all but the last segment, mislabelled the spec.
func TestSpecStartEventPathKeepsSeparatorInsideDeclaredName(t *testing.T) {
	rep := &recordingReporter{}
	DescribeWithReporter(t, "suite", rep, func(s *Spec) {
		s.When("when b", func(s *Spec) {
			s.It("slash/inside", func(*Context) {})
		})
	})

	if len(rep.specStarted) != 1 {
		t.Fatalf("expected one SpecStarted, got %+v", rep.specStarted)
	}
	got := rep.specStarted[0]
	want := []string{"suite", "when b", "slash/inside"}
	if !slices.Equal(got.Path, want) {
		t.Fatalf("expected Path %q, got %q", want, got.Path)
	}
	if got.Path[len(got.Path)-1] != got.Name {
		t.Fatalf("expected last Path segment to be Name %q, got %q", got.Name, got.Path[len(got.Path)-1])
	}
}

// TestSpecStartEventPathKeepsSeparatorInsideDeclaredScope proves the same holds for a separator
// inside an enclosing Describe/When name, not only inside the spec's own name.
func TestSpecStartEventPathKeepsSeparatorInsideDeclaredScope(t *testing.T) {
	rep := &recordingReporter{}
	DescribeWithReporter(t, "suite", rep, func(s *Spec) {
		s.When("GET /users", func(s *Spec) {
			s.It("returns the list", func(*Context) {})
		})
	})

	if len(rep.specStarted) != 1 {
		t.Fatalf("expected one SpecStarted, got %+v", rep.specStarted)
	}
	want := []string{"suite", "GET /users", "returns the list"}
	if got := rep.specStarted[0].Path; !slices.Equal(got, want) {
		t.Fatalf("expected Path %q, got %q", want, got)
	}
}

// TestSpecStartEventPathMatchesDeclaredScopesForEverySpec pins the ordinary case alongside the
// separator one: every spec reports exactly its own declared breadcrumb, in declaration order, and
// carrying the segments must not disturb sibling or nesting-depth boundaries.
func TestSpecStartEventPathMatchesDeclaredScopesForEverySpec(t *testing.T) {
	rep := &recordingReporter{}
	DescribeWithReporter(t, "suite", rep, func(s *Spec) {
		s.It("at the root", func(*Context) {})
		s.When("when b", func(s *Spec) {
			s.It("does a thing", func(*Context) {})
			s.Describe("nested c", func(s *Spec) {
				s.It("goes deeper", func(*Context) {})
			})
		})
		s.It("back at the root", func(*Context) {})
	})

	want := [][]string{
		{"suite", "at the root"},
		{"suite", "when b", "does a thing"},
		{"suite", "when b", "nested c", "goes deeper"},
		{"suite", "back at the root"},
	}
	if len(rep.specStarted) != len(want) {
		t.Fatalf("expected %d SpecStarted events, got %+v", len(want), rep.specStarted)
	}
	for i, w := range want {
		if got := rep.specStarted[i].Path; !slices.Equal(got, w) {
			t.Fatalf("spec %d: expected Path %q, got %q", i, w, got)
		}
	}
}

// TestSpecStartEventPathIsNotSharedBetweenSpecs proves each event gets its own slice. Path reaches
// arbitrary report.EventReporter implementations, so a reporter that sorts or truncates it in place
// must not be able to corrupt the plan for the specs that run after it.
func TestSpecStartEventPathIsNotSharedBetweenSpecs(t *testing.T) {
	rep := &recordingReporter{}
	DescribeWithReporter(t, "suite", rep, func(s *Spec) {
		s.When("when b", func(s *Spec) {
			s.It("first", func(*Context) {})
			s.It("second", func(*Context) {})
		})
	})

	if len(rep.specStarted) != 2 {
		t.Fatalf("expected two SpecStarted events, got %+v", rep.specStarted)
	}
	for i := range rep.specStarted[0].Path {
		rep.specStarted[0].Path[i] = "clobbered"
	}
	want := []string{"suite", "when b", "second"}
	if got := rep.specStarted[1].Path; !slices.Equal(got, want) {
		t.Fatalf("expected the second spec's Path to survive mutation of the first as %q, got %q", want, got)
	}
}

// TestSpecPlanSharesScopeWindowBetweenSiblingSpecs pins the representation, not just the reported
// value. Every spec declared in the same block has the same enclosing scopes, so the plan stores
// that chain once and points every sibling at it.
//
// Giving each spec a private copy reports the same Path, so no Path assertion can catch that
// regression — but it costs O(specs × depth) string headers, and because Go grows a large slice by
// ~1.25x the discarded intermediate arrays cost several times the final size again. Measured over
// 50k sibling specs, the per-spec copy cost +91% build memory and +43% build time.
func TestSpecPlanSharesScopeWindowBetweenSiblingSpecs(t *testing.T) {
	const siblings = 500
	suite := BuildSuite(t, "suite", func(s *Spec) {
		s.Describe("outer scope", func(s *Spec) {
			s.When("inner condition", func(s *Spec) {
				for i := 0; i < siblings; i++ {
					s.It("does the thing", func(*Context) {})
				}
			})
		})
	})
	if suite == nil || suite.Plan == nil {
		t.Fatal("BuildSuite returned no plan")
	}
	plan := suite.Plan
	if len(plan.PathScopeStart) != siblings {
		t.Fatalf("expected %d per-spec windows, got %d", siblings, len(plan.PathScopeStart))
	}
	// One chain declared, so one chain stored: "suite", "outer scope", "inner condition".
	if want := 3; len(plan.PathScopes) != want {
		t.Fatalf("expected %d shared scope segments for %d sibling specs, got %d: %q",
			want, siblings, len(plan.PathScopes), plan.PathScopes)
	}
	for i := range plan.PathScopeStart {
		if plan.PathScopeStart[i] != 0 || plan.PathScopeLen[i] != 3 {
			t.Fatalf("spec %d: expected every sibling to share the window {0,3}, got {%d,%d}",
				i, plan.PathScopeStart[i], plan.PathScopeLen[i])
		}
	}
	// Sharing the window must not change what a reporter sees.
	want := []string{"suite", "outer scope", "inner condition", "does the thing"}
	if got := specEventPath(plan, siblings-1); !slices.Equal(got, want) {
		t.Fatalf("expected the last sibling's Path %q, got %q", want, got)
	}
}

// TestSpecPlanFromArenaReportsDeclaredScopes covers the other plan producer. The arena builder is
// only reached through the registry-backed analyze path, which no test drives, so the plan is built
// from a hand-assembled arena here — the cheapest way to prove both producers agree on which
// segments are scopes and which one is the leaf.
func TestSpecPlanFromArenaReportsDeclaredScopes(t *testing.T) {
	noop := func(*Context) {}
	arena := &NodeArena{
		Nodes: []ArenaNode{
			{Name: "suite", Parent: -1, Type: SuiteNode},
			{Name: "when b", Parent: 0, Type: DescribeNode},
			{Name: "slash/inside", Parent: 1, Type: ItNode, Fn: noop},
			{Name: "sibling", Parent: 1, Type: ItNode, Fn: noop},
		},
		Children:    [][]int{{1}, {2, 3}, nil, nil},
		BeforeHooks: make([][]func(*Context), 4),
		AfterHooks:  make([][]func(*Context), 4),
	}
	plan := newExecutionPlan(2)
	scratch := planScratchPool.Get().(*planScratch)
	defer planScratchPool.Put(scratch)
	buildExecutionPlanFromArena(arena, 0, plan, scratch)

	if len(plan.Names) != 2 {
		t.Fatalf("expected two specs in the plan, got %q", plan.Names)
	}
	// This producer does not treat the suite name as a path scope, so the breadcrumb starts at
	// "when b". The separator inside "slash/inside" stays in one segment either way.
	wants := [][]string{
		{"when b", "slash/inside"},
		{"when b", "sibling"},
	}
	for i, want := range wants {
		if got := specEventPath(plan, i); !slices.Equal(got, want) {
			t.Fatalf("spec %d: expected Path %q, got %q", i, want, got)
		}
	}
	if want := 1; len(plan.PathScopes) != want {
		t.Fatalf("expected the two siblings to share %d scope segment, got %q", want, plan.PathScopes)
	}
}
