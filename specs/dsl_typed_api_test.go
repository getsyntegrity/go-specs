// dsl_typed_api_test.go pins the typed DSL body signatures introduced for issue #210: Spec.When and
// Builder.It no longer take interface{}, and Builder.ItWith carries the Skip/Focus routing that
// Builder.It used to do dynamically.
package specs

import "testing"

// The declarations below are compile-time assertions: if any of these signatures ever drifts back
// toward interface{}/any, or away from the shape below, the package fails to build. There is
// nothing to run; the check is that this file compiles at all.
var (
	_ func(*Spec, string, func(*Spec))    = (*Spec).When
	_ func(*Spec, string, func(*Context)) = (*Spec).It
	_ func(*Spec, string, func(*Spec))    = (*Spec).Describe
	_ func(*Spec, func(*Context))         = (*Spec).BeforeEach
	_ func(*Spec, func(*Context))         = (*Spec).AfterEach

	_ func(*Builder, string, func(*Context)) = (*Builder).It
	_ func(*Builder, string, SpecFn)         = (*Builder).ItWith
)

// TestItWithPlainSpecFnRuns proves ItWith runs a SpecFn built with neither Skip nor Focus set, the
// same as It would with the bare func(*Context) inside it.
func TestItWithPlainSpecFnRuns(t *testing.T) {
	ran := false
	b := NewBuilder()
	b.ItWith("plain", SpecFn{Fn: func(*Context) { ran = true }})
	prog := b.Build()
	NewRunner(prog).Run(t)
	if !ran {
		t.Fatal("expected the plain SpecFn body to run")
	}
}

// TestItWithSkipDoesNotRunButKeepsIdentity models TestProgram_SkipWithHelper /
// TestProgram_SkipRemoval: ItWith("name", Skip(fn)) must not compile fn into a step (it never
// runs), but the spec's name must still be preserved as skipped so the compiled Program can report
// its identity (see builder.go's finalize).
func TestItWithSkipDoesNotRunButKeepsIdentity(t *testing.T) {
	ran := false
	b := NewBuilder()
	b.ItWith("skipped one", Skip(func(*Context) { ran = true }))
	b.It("runs", func(*Context) {})
	prog := b.Build()
	if len(prog.Groups) != 1 {
		t.Fatalf("expected one group; got %d", len(prog.Groups))
	}
	g := prog.Groups[0]
	if len(g.specs) != 1 {
		t.Fatalf("expected only the non-skipped spec to compile into a step; got %d", len(g.specs))
	}
	if len(g.skipped) != 1 || g.skipped[0] != "skipped one" {
		t.Fatalf("expected the skipped spec's identity to be preserved as %q, got %v", "skipped one", g.skipped)
	}
	NewRunner(prog).Run(t)
	if ran {
		t.Fatal("expected the skipped spec's body to never run")
	}
}

// TestItWithFocusBehavesLikeFIt mirrors TestProgram_FocusFiltering: ItWith("name", Focus(fn))
// must filter out every non-focused spec, exactly like FIt.
func TestItWithFocusBehavesLikeFIt(t *testing.T) {
	var order []string
	b := NewBuilder()
	b.It("A", func(*Context) { order = append(order, "A") })
	b.ItWith("B", Focus(func(*Context) { order = append(order, "B") }))
	b.It("C", func(*Context) { order = append(order, "C") })
	prog := b.Build()
	if len(prog.Groups) != 1 || len(prog.Groups[0].specs) != 1 {
		t.Fatalf("expected 1 group with 1 spec (B); got %d groups", len(prog.Groups))
	}
	NewRunner(prog).Run(t)
	if len(order) != 1 || order[0] != "B" {
		t.Errorf("order=%v, want [B]", order)
	}
}

// TestWhenBodyReceivesUsableChildSpec models TestNestedSpecInheritsTheBuildTarget: the *Spec a
// When body receives must be a fully usable build target, not a placeholder — specs and hooks
// registered on it must reach the same suite as anything registered on the parent.
func TestWhenBodyReceivesUsableChildSpec(t *testing.T) {
	suite := Analyze(func() {
		Describe(nil, "Calculator", func(s *Spec) {
			s.When("adding", func(child *Spec) {
				child.BeforeEach(func(ctx *Context) {})
				child.It("adds correctly", func(ctx *Context) {})
			})
		})
	})
	if got := suite.Tree(); got != "Calculator\n  adding\n    adds correctly" {
		t.Fatalf("nested registrations via When did not reach the arena: %q", got)
	}
}
