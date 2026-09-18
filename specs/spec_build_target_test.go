package specs

import (
	"strings"
	"testing"
)

// Spec is exported with unexported fields, so nothing stops external code from writing
// &specs.Spec{}. Such a Spec carries neither a compiler nor a registry, so every registration made
// against it used to be accepted and dropped: a suite could declare specs and hooks, register none
// of them, and still report green. Each method now fails closed instead (issue #151).
func TestZeroValueSpecPanicsOnEveryRegistration(t *testing.T) {
	cases := []struct {
		method string
		call   func(s *Spec)
	}{
		{"It", func(s *Spec) { s.It("adds correctly", func(ctx *Context) {}) }},
		{"Describe", func(s *Spec) { s.Describe("Calculator", func(*Spec) {}) }},
		{"When", func(s *Spec) { s.When("adding numbers", func(*Spec) {}) }},
		{"BeforeEach", func(s *Spec) { s.BeforeEach(func(ctx *Context) {}) }},
		{"AfterEach", func(s *Spec) { s.AfterEach(func(ctx *Context) {}) }},
		{"Paths", func(s *Spec) { s.runPathWithContext("explores", &PathGenerator{}, nil, func(ctx *Context) {}) }},
	}
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			msg := recoverMessage(t, func() { tc.call(&Spec{}) })
			if !strings.Contains(msg, "Spec."+tc.method) {
				t.Fatalf("panic must name the method that had nowhere to register; got %q", msg)
			}
			if !strings.Contains(msg, "no build target") {
				t.Fatalf("panic must explain the missing build target; got %q", msg)
			}
		})
	}
}

// When rejects a legacy func() scope on a target-less Spec for the same reason it rejects
// func(*Spec): the block would run and every registration inside it would be discarded.
func TestZeroValueSpecPanicsOnLegacyWhenScope(t *testing.T) {
	ran := false
	msg := recoverMessage(t, func() {
		(&Spec{}).When("adding numbers", func() { ran = true })
	})
	if !strings.Contains(msg, "Spec.When") {
		t.Fatalf("expected a panic naming Spec.When; got %q", msg)
	}
	if ran {
		t.Fatal("the legacy scope must not run when there is nowhere to register what it declares")
	}
}

// The child Spec handed to a nested block inherits the parent's registry, so a nested registration
// reaches the same arena rather than falling into the target-less path.
func TestNestedSpecInheritsTheBuildTarget(t *testing.T) {
	suite := Analyze(func() {
		Describe(nil, "Calculator", func(s *Spec) {
			s.Describe("addition", func(child *Spec) {
				child.BeforeEach(func(ctx *Context) {})
				child.It("adds correctly", func(ctx *Context) {})
			})
		})
	})
	if got := suite.Tree(); got != "Calculator\n  addition\n    adds correctly" {
		t.Fatalf("nested registrations did not reach the arena: %q", got)
	}
}

// A nil *Spec stays a no-op: that is the documented nil-receiver tolerance the DSL already had, and
// it is not the silent-discard case — there is no Spec to have registered anything into.
func TestNilSpecRemainsNoOp(t *testing.T) {
	var s *Spec
	s.It("adds correctly", func(ctx *Context) {})
	s.Describe("Calculator", func(*Spec) {})
	s.When("adding numbers", func(*Spec) {})
	s.BeforeEach(func(ctx *Context) {})
	s.AfterEach(func(ctx *Context) {})
}
