// builder_test.go shows the original Builder/Program/Runner API.
//
// Before Describe existed, a suite was declared on a Builder, compiled into a Program and executed
// by a Runner. That API is still supported. Reach for it when you need explicit control over the
// compile and run steps: several runners over one program, a Runner.FailFast switch, or a suite
// assembled by generated code. New code should prefer specs.Describe (dsl_test.go).
//
//	NewBuilder -> Describe / BeforeEach / It ... -> Build -> NewRunner(prog).Run(t)
//
// As in the DSL, per-spec hooks must be declared before the first It of their scope.
package examples_test

import (
	"testing"

	"github.com/getsyntegrity/go-specs/report"
	"github.com/getsyntegrity/go-specs/specs"
)

func builderAdd(a, b int) int { return a + b }

// NewBuilder, Build and NewRunner: declare, compile, run.
func TestBuilder_declareBuildRun(t *testing.T) {
	b := specs.NewBuilder()
	b.Describe("math", func() {
		b.BeforeEach(func(ctx *specs.Context) {})
		b.It("adds numbers", func(ctx *specs.Context) {
			ctx.Expect(builderAdd(1, 1)).ToEqual(2)
		})
		b.ItParallel("adds more numbers", func(ctx *specs.Context) {
			ctx.Expect(builderAdd(2, 2)).ToEqual(4)
		})
	})
	prog := b.Build()
	specs.NewRunner(prog).Run(t)
}

// BuildProgram compiles in a single call, which reads better when nothing else needs the builder.
func TestBuilder_buildProgram(t *testing.T) {
	prog := specs.BuildProgram(func(b *specs.Builder) {
		b.Describe("suite", func() {
			b.It("runs", func(ctx *specs.Context) {
				ctx.Expect("ok").ToEqual("ok")
			})
		})
	})
	specs.NewRunner(prog).Run(t)
}

// NewRunnerWithReporter attaches a report.EventReporter, which receives suite and spec events as
// the program runs. Runner.FailFast (unset here) stops after the first failing step.
func TestBuilder_runnerWithReporter(t *testing.T) {
	collector := report.NewCollector()

	prog := specs.BuildProgram(func(b *specs.Builder) {
		b.Describe("reported", func() {
			b.It("passes", func(ctx *specs.Context) { ctx.Expect(1).ToEqual(1) })
			b.PendingIt("comes later", nil)
		})
	})
	specs.NewRunnerWithReporter(prog, "builder suite", collector).Run(t)

	totals := collector.Report().Execution
	if totals.Passed != 1 || totals.Pending != 1 {
		t.Fatalf("unexpected totals: %+v", totals)
	}
}
