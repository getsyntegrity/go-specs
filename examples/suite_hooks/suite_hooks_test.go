// Package suite_hooks_test shows BeforeAll/AfterAll: once-per-group setup and teardown, for a
// fixture that would be wasteful to rebuild before every single spec — see
// docs/SUITE_HOOKS_CONTRACT.md for the full contract.
package suite_hooks_test

import (
	"fmt"
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
)

// testDatabase stands in for an expensive fixture — a real example might start a Postgres
// container, spin up a test HTTP server, or seed a large dataset.
type testDatabase struct {
	queries int
}

func (d *testDatabase) query(name string) string {
	d.queries++
	return fmt.Sprintf("row for %s", name)
}

func TestCheckoutWithSharedDatabase(t *testing.T) {
	var db *testDatabase

	specs.Describe(t, "checkout", func(s *specs.Spec) {
		// BeforeAll runs exactly once, right before the first spec below — not once per spec, the
		// way BeforeEach would. AfterAll runs exactly once, right after the last one.
		s.BeforeAll(func(ctx *specs.Context) {
			db = &testDatabase{}
		})
		s.AfterAll(func(ctx *specs.Context) {
			// Real teardown would close a connection pool or stop a container here.
			db = nil
		})

		s.When("the cart has items", func(w *specs.Spec) {
			w.It("charges the card", func(ctx *specs.Context) {
				ctx.Expect(db.query("card")).ToEqual("row for card")
			})

			w.It("emails a receipt", func(ctx *specs.Context) {
				ctx.Expect(db.query("receipt")).ToEqual("row for receipt")
			})
		})
	})
}
