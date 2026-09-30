// table_test.go shows specs.Table and specs.TableParallel.
//
// Table removes the boilerplate of "for each row, register an It" and checks the row names up
// front. Use it when many cases run the same body with different data. Each row is still an
// ordinary It: its own Go subtest (`go test -run 'TestTable_table/add/negative'` runs one row),
// wrapped by the enclosing BeforeEach/AfterEach hooks, reported and failed on its own.
//
// Naming rules, checked for every row before any row is registered:
//   - the name must not be empty;
//   - names must be unique, and names that differ only by spaces versus underscores are
//     duplicates too, because go test rewrites spaces to underscores;
//   - name and body must not be nil.
//
// A table that breaks a rule registers nothing and panics with the offending row indexes, so a bad
// table can never pass by accident. Rows are copied when Table is called, and the name function
// runs at registration time, not while specs execute. Table adds no group segment: wrap it in
// s.When to add one.
package examples_test

import (
	"strings"
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
)

func tableAdd(a, b int) int { return a + b }

type tableAddCase struct {
	name string
	a, b int
	want int
}

var tableAddCases = []tableAddCase{
	{"positive", 1, 2, 3},
	{"negative", -1, -2, -3},
	{"zero", 0, 0, 0},
}

// Step 0: the ordinary Go table test that you might be migrating.
func TestTable_plainGo(t *testing.T) {
	for _, tc := range tableAddCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tableAdd(tc.a, tc.b); got != tc.want {
				t.Fatalf("add(%d, %d) = %d, want %d", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

// Step 1: the same rows with a plain loop and It. This stays fully supported.
func TestTable_forAndIt(t *testing.T) {
	specs.Describe(t, "add", func(s *specs.Spec) {
		for _, tc := range tableAddCases {
			s.It(tc.name, func(ctx *specs.Context) {
				ctx.Expect(tableAdd(tc.a, tc.b)).ToEqual(tc.want)
			})
		}
	})
}

// Step 2: specs.Table removes the loop and validates the row names.
func TestTable_table(t *testing.T) {
	specs.Describe(t, "add", func(s *specs.Spec) {
		specs.Table(s, tableAddCases, func(tc tableAddCase) string { return tc.name },
			func(ctx *specs.Context, tc tableAddCase) {
				ctx.Expect(tableAdd(tc.a, tc.b)).ToEqual(tc.want)
			})
	})
}

// Hooks wrap every row, and a When adds a grouping segment only because the caller asked for one.
//
// No spec asserts on what another one did, so each passes when selected alone with -run. A row
// only checks that its own BeforeEach has run; the exact count (one call per spec that ran) is
// checked once, in t.Cleanup, after the whole suite has finished.
func TestTable_withHooksAndGroup(t *testing.T) {
	var calls, ran int
	t.Cleanup(func() {
		if calls != ran {
			t.Errorf("BeforeEach must run once per spec: ran=%d calls=%d", ran, calls)
		}
	})

	specs.Describe(t, "add", func(s *specs.Spec) {
		s.BeforeEach(func(*specs.Context) { calls++ })
		s.When("integers", func(s *specs.Spec) {
			specs.Table(s, tableAddCases, func(tc tableAddCase) string { return tc.name },
				func(ctx *specs.Context, tc tableAddCase) {
					ran++
					ctx.Expect(calls > 0).ToEqual(true) // its own BeforeEach ran first
					ctx.Expect(tableAdd(tc.a, tc.b)).ToEqual(tc.want)
				})
		})
		s.It("runs the hook before a plain spec too", func(ctx *specs.Context) {
			ran++
			ctx.Expect(calls > 0).ToEqual(true)
		})
	})
}

// Rows that are independent can run concurrently with TableParallel. Each row still gets its own
// Context. An empty rows slice registers nothing.
func TestTable_parallel(t *testing.T) {
	type row struct{ in, want string }
	rows := []row{{"go", "GO"}, {"specs", "SPECS"}, {"table", "TABLE"}}

	specs.Describe(t, "upper", func(s *specs.Spec) {
		specs.TableParallel(s, rows, func(r row) string { return r.in },
			func(ctx *specs.Context, r row) {
				ctx.Expect(strings.ToUpper(r.in)).ToEqual(r.want)
			})
	})
}
