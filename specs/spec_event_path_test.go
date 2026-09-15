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
