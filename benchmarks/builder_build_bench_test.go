//go:build !js && !wasm && !lint
// +build !js,!wasm,!lint

package benchmarks

import (
	"testing"

	specs "github.com/getsyntegrity/go-specs/specs"
)

// Build-phase sizes for the Builder breadcrumb benchmarks: deep enough that a per-spec breadcrumb
// join has several segments to concatenate, wide enough that its cost is not lost in noise.
const (
	builderBuildDepth = 5
	builderBuildSpecs = 200
)

// BenchmarkBuilder_Build measures the Builder/Program *build* phase, which the existing benchmarks
// deliberately exclude: BenchmarkRunner_GoSpecs_BuildSuite compiles its suite before ResetTimer and
// exercises the ExecutionPlan model, and BenchmarkHooks_GoSpecs calls BuildSpecsProgramWithHooks
// before ResetTimer too. Neither can see what building a Program costs, which is exactly where the
// per-spec Describe breadcrumb (#102) is computed — one joined string per It, plus the scope-name
// stack and the per-group fullNames slice. Nesting is what makes that visible: a flat suite joins
// nothing.
func BenchmarkBuilder_Build(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		builderBuildNested(builderBuildDepth, builderBuildSpecs)
	}
}

// builderBuildNested declares builderBuildSpecs specs at the bottom of a depth-deep Describe nest,
// so every spec's breadcrumb has depth+1 segments to join.
func builderBuildNested(depth, n int) *specs.Program {
	b := specs.NewBuilder()
	var nest func(level int)
	nest = func(level int) {
		if level == depth {
			for i := 0; i < n; i++ {
				b.It("does a thing", func(ctx *specs.Context) {
					specs.EqualTo(ctx, 1, 1)
				})
			}
			return
		}
		b.Describe("scope", func() { nest(level + 1) })
	}
	nest(0)
	return b.Build()
}
