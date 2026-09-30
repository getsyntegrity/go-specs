// hooks_test.go shows BeforeEach/AfterEach and BeforeAll/AfterAll.
//
// Hooks remove setup and teardown noise from spec bodies. Use BeforeEach/AfterEach when every
// spec needs a fresh state, and BeforeAll/AfterAll when a fixture is expensive and safe to share.
// The full contract for the once-per-group hooks is in docs/SUITE_HOOKS_CONTRACT.md.
package examples_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
)

// BeforeEach runs before every spec in its group, AfterEach after it. State that a hook resets is
// therefore private to each spec: both specs below see counter == 1.
func TestHooks_perSpecReset(t *testing.T) {
	var counter int

	specs.Describe(t, "lifecycle", func(s *specs.Spec) {
		s.BeforeEach(func(ctx *specs.Context) { counter++ })
		s.AfterEach(func(ctx *specs.Context) { counter = 0 })

		s.It("runs before each spec", func(ctx *specs.Context) {
			ctx.Expect(counter).ToEqual(1)
		})
		s.It("starts again from a clean state", func(ctx *specs.Context) {
			ctx.Expect(counter).ToEqual(1)
		})
	})
}

// Order across nested groups: BeforeEach hooks run outer to inner, AfterEach hooks run inner to
// outer (last registered runs first). For one spec the trace below is exactly
// A -> B -> spec -> C -> D.
func TestHooks_order(t *testing.T) {
	var trace []string
	mark := func(label string) func(*specs.Context) {
		return func(*specs.Context) { trace = append(trace, label) }
	}

	specs.Describe(t, "outer", func(s *specs.Spec) {
		s.BeforeEach(mark("A outer before"))
		s.AfterEach(mark("D outer after"))

		s.Describe("inner", func(s *specs.Spec) {
			s.BeforeEach(mark("B inner before"))
			s.AfterEach(mark("C inner after"))
			s.It("runs between the hooks", mark("spec"))
		})
	})

	got := strings.Join(trace, ", ")
	want := "A outer before, B inner before, spec, C inner after, D outer after"
	if got != want {
		t.Fatalf("hook order = %q, want %q", got, want)
	}
}

// Per-spec hooks apply only to specs declared after them, so declare them before the first
// It/When/Describe of their group. Registering one later is a programming mistake, and the DSL
// panics while the suite is built instead of running the spec without the hook.
func TestHooks_lateRegistrationPanicsAtBuildTime(t *testing.T) {
	defer func() {
		msg := fmt.Sprint(recover())
		if !strings.Contains(msg, "BeforeEach registered after a spec") {
			t.Fatalf("expected the late-hook panic, got %q", msg)
		}
	}()

	specs.BuildSuite(t, "late hook", func(s *specs.Spec) {
		s.It("first", func(ctx *specs.Context) {})
		s.BeforeEach(func(ctx *specs.Context) {}) // too late: panics
	})
	t.Fatal("BuildSuite should have panicked")
}

// testDatabase stands in for an expensive fixture, such as a container or a seeded database.
type testDatabase struct{ queries int }

func (d *testDatabase) query(name string) string {
	d.queries++
	return "row for " + name
}

// BeforeAll runs once, right before the first spec of its group, not once per spec. AfterAll runs
// once, right after the last one, and still runs when a spec fails. Both specs below share the
// same fixture, and the counter proves it was built only once.
func TestHooks_beforeAllAfterAll(t *testing.T) {
	var db *testDatabase
	setups := 0

	specs.Describe(t, "checkout", func(s *specs.Spec) {
		s.BeforeAll(func(ctx *specs.Context) {
			setups++
			db = &testDatabase{}
		})
		s.AfterAll(func(ctx *specs.Context) {
			// Real teardown would close a pool or stop a container.
			db = nil
		})

		s.When("the cart has items", func(s *specs.Spec) {
			s.It("charges the card", func(ctx *specs.Context) {
				ctx.Expect(db.query("card")).ToEqual("row for card")
			})
			s.It("emails a receipt", func(ctx *specs.Context) {
				ctx.Expect(db.query("receipt")).ToEqual("row for receipt")
				ctx.Expect(db.queries).ToEqual(2) // the same fixture served both specs
			})
		})
	})

	if setups != 1 {
		t.Fatalf("BeforeAll ran %d times, want 1", setups)
	}
}
