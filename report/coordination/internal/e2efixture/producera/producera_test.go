package producera

import (
	"fmt"
	"os"
	"testing"

	"github.com/getsyntegrity/go-specs/report"
	"github.com/getsyntegrity/go-specs/report/coordination"
	"github.com/getsyntegrity/go-specs/report/coordination/internal/e2efixture/shared"
	"github.com/getsyntegrity/go-specs/specs"
)

const importPath = "github.com/getsyntegrity/go-specs/report/coordination/internal/e2efixture/producera"

var reporter = report.NewMultiFormat()

// TestMain is the complete per-package integration contract v1.2.8 §3 step 3 wiring, exactly as
// shardfixture/alpha and shardfixture/beta reproduce it.
func TestMain(m *testing.M) {
	writer, err := coordination.ShardWriterFromEnv(importPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	code := m.Run()

	if err := writer.Write(reporter.Report()); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	os.Exit(code)
}

// TestProducerA registers a BeforeAll/AfterAll group hook and calls shared.Compute with a
// positive n, exercising the "n > 0" block.
//
// GO_SPECS_FIXTURE_HOOK_FAIL is the same env-gated-failure convention shardfixture/alpha and
// shardfixture/beta already use for GO_SPECS_FIXTURE_FAIL: with the gate off, BeforeAll passes and
// this package behaves like any ordinary suite, so the repository's own `go test ./...` stays
// green. With the gate on, BeforeAll deliberately fails, which is the only way a group hook
// produces a report.Case at all (docs/SUITE_HOOKS_CONTRACT.md H8: "only a failed hook produces a
// case"). The e2e test in report/coordination sets the gate to prove Finalize's merge preserves
// that synthetic [BeforeAll] hook case (#207/#228) unchanged.
func TestProducerA(t *testing.T) {
	specs.DescribeWithReporter(t, "producer-a", reporter, func(s *specs.Spec) {
		s.BeforeAll(func(ctx *specs.Context) {
			got := shared.Compute(3)
			if os.Getenv("GO_SPECS_FIXTURE_HOOK_FAIL") == "1" {
				ctx.Expect(got).ToEqual(-1) // 6 != -1: deliberate failure
				return
			}
			ctx.Expect(got).ToEqual(6)
		})
		s.AfterAll(func(ctx *specs.Context) {
			_ = shared.Compute(3)
		})

		s.It("passes when the hook-failure gate is off; skipped by the failed BeforeAll otherwise", func(ctx *specs.Context) {
			ctx.Expect(1 + 1).ToEqual(2)
		})
	})
}
