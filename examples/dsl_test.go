// dsl_test.go shows the core DSL: Describe, When and It.
//
// The DSL solves the readability problem of flat Go tests. You describe a unit, group behaviors by
// context, and every It becomes its own Go subtest named by its full breadcrumb, for example
// "TestDSL_nested/calculator/addition/adds_two_numbers". That name is what you pass to
// `go test -run` to run a single spec.
//
// See docs/DSL.md for the full reference and docs/EXECUTION_ENGINES.md for the engines behind it.
package examples_test

import (
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
)

// The smallest useful suite: one Describe, one It, one assertion.
func TestDSL_minimal(t *testing.T) {
	specs.Describe(t, "math", func(s *specs.Spec) {
		s.It("adds numbers", func(ctx *specs.Context) {
			ctx.Expect(1 + 1).ToEqual(2)
		})
	})
}

// Describe and When both open a nested group. They behave identically; the two names exist so the
// breadcrumb reads naturally: "describe a thing", "when some condition holds", "it does X".
func TestDSL_nested(t *testing.T) {
	specs.Describe(t, "calculator", func(s *specs.Spec) {
		s.Describe("addition", func(s *specs.Spec) {
			s.It("adds two numbers", func(ctx *specs.Context) {
				ctx.Expect(2 + 2).ToEqual(4)
			})
			s.It("handles zero", func(ctx *specs.Context) {
				ctx.Expect(0 + 5).ToEqual(5)
			})
		})

		s.When("subtracting", func(s *specs.Spec) {
			s.It("subtracts a smaller number from a larger one", func(ctx *specs.Context) {
				ctx.Expect(10 - 3).ToEqual(7)
			})
		})
	})
}

// DescribeFlat and DescribeFast are compatibility aliases of Describe (docs/EXECUTION_ENGINES.md).
// They run the same engine, report the same subtests, and cost the same allocations. Prefer
// Describe in new code; you only meet the other two in older suites.
func TestDSL_describeAliases(t *testing.T) {
	body := func(s *specs.Spec) {
		s.It("behaves the same", func(ctx *specs.Context) {
			ctx.Expect("go").ToEqual("go")
		})
	}
	specs.DescribeFlat(t, "flat", body)
	specs.DescribeFast(t, "fast", body)
}

// Describe registers the specs and then runs them when the callback returns. BuildSuite splits
// those two steps: it builds and compiles the tree once and returns a *specs.CompiledSuite, which
// you can run later, several times, or shard (see sharding_test.go). This is the entry point for
// benchmarks that want to measure execution without paying the build cost each iteration.
//
// The suite runs on the plain t, so each run is a normal subtest. The spec checks only itself:
// it fails if building the suite had already executed it. Nothing counts executions across the
// runs, so the example passes however `go test -run` selects subtests.
func TestDSL_buildSuiteThenRun(t *testing.T) {
	running := false
	suite := specs.BuildSuite(t, "compiled", func(s *specs.Spec) {
		s.It("only runs once the suite is run", func(ctx *specs.Context) {
			ctx.Expect(running).ToEqual(true)
		})
	})

	// Nothing has run yet: building only compiles. Now run the same suite several times.
	running = true
	for range 3 {
		suite.Run(t)
	}
}

// PendingIt declares a spec you plan to write. Its body is never compiled or run, and reports show
// it as "pending", which is different from "skipped": pending means "specified but not
// implemented", so a pending list works as a to-do list. The body may be nil. See
// focus_skip_test.go for SkipIt and FIt.
func TestDSL_pendingIt(t *testing.T) {
	specs.Describe(t, "refunds", func(s *specs.Spec) {
		s.It("refunds a full payment", func(ctx *specs.Context) {
			refund, paid := 100, 100
			ctx.Expect(paid - refund).ToEqual(0)
		})
		s.PendingIt("refunds a partial payment", nil)
	})
}
