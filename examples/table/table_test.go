// Package table_test shows how an ordinary Go table test migrates to specs.Table.
package table_test

import (
	"strings"
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
)

func add(a, b int) int { return a + b }

type addCase struct {
	name string
	a, b int
	want int
}

var addCases = []addCase{
	{"positive", 1, 2, 3},
	{"negative", -1, -2, -3},
	{"zero", 0, 0, 0},
}

// Step 0: the ordinary Go table test being migrated.
func TestAdd_plainGo(t *testing.T) {
	for _, tc := range addCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := add(tc.a, tc.b); got != tc.want {
				t.Fatalf("add(%d, %d) = %d, want %d", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

// Step 1: the same rows with a plain loop and It. This stays fully supported.
func TestAdd_forAndIt(t *testing.T) {
	specs.Describe(t, "add", func(s *specs.Spec) {
		for _, tc := range addCases {
			s.It(tc.name, func(ctx *specs.Context) {
				ctx.Expect(add(tc.a, tc.b)).ToEqual(tc.want)
			})
		}
	})
}

// Step 2: specs.Table removes the loop, and rejects empty or duplicate row names up front. Each row
// is still its own subtest: `go test -run 'TestAdd_table/add/negative'` runs just that row.
func TestAdd_table(t *testing.T) {
	specs.Describe(t, "add", func(s *specs.Spec) {
		specs.Table(s, addCases, func(tc addCase) string { return tc.name },
			func(ctx *specs.Context, tc addCase) {
				ctx.Expect(add(tc.a, tc.b)).ToEqual(tc.want)
			})
	})
}

// Hooks wrap every row, and When adds a grouping segment only because the caller asked for one.
func TestAdd_tableWithHooks(t *testing.T) {
	specs.Describe(t, "add", func(s *specs.Spec) {
		var calls int
		s.BeforeEach(func(*specs.Context) { calls++ })
		s.When("integers", func(s *specs.Spec) {
			specs.Table(s, addCases, func(tc addCase) string { return tc.name },
				func(ctx *specs.Context, tc addCase) {
					ctx.Expect(add(tc.a, tc.b)).ToEqual(tc.want)
				})
		})
		s.It("ran the hook once per row", func(ctx *specs.Context) {
			ctx.Expect(calls).ToEqual(len(addCases) + 1)
		})
	})
}

// Rows that are independent can run concurrently with TableParallel.
func TestUpper_tableParallel(t *testing.T) {
	type row struct{ in, want string }
	rows := []row{{"go", "GO"}, {"specs", "SPECS"}, {"table", "TABLE"}}
	specs.Describe(t, "upper", func(s *specs.Spec) {
		specs.TableParallel(s, rows, func(r row) string { return r.in },
			func(ctx *specs.Context, r row) {
				ctx.Expect(strings.ToUpper(r.in)).ToEqual(r.want)
			})
	})
}
