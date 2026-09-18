package beta

import (
	"fmt"
	"os"
	"strconv"
	"testing"

	"github.com/getsyntegrity/go-specs/report"
	"github.com/getsyntegrity/go-specs/report/coordination"
	"github.com/getsyntegrity/go-specs/specs"
)

const importPath = "github.com/getsyntegrity/go-specs/report/coordination/internal/shardfixture/beta"

var reporter = report.NewMultiFormat()

// TestMain is the complete per-package integration contract v1.2.6 §3 step 3 asks for. It is
// reproduced verbatim in docs/REPORTING.md, so keep the two in step.
func TestMain(m *testing.M) {
	writer, err := coordination.ShardWriterFromEnv(importPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	code := m.Run()

	if err := writer.Write(reporter.Report()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	// GO_SPECS_FIXTURE_EXIT lets the integration test prove that cmd/go does not propagate a test
	// binary's exit code. It is read after m.Run() so it cannot affect the cache key.
	if raw := os.Getenv("GO_SPECS_FIXTURE_EXIT"); raw != "" {
		forced, err := strconv.Atoi(raw)
		if err != nil {
			fmt.Fprintln(os.Stderr, "fixture: bad GO_SPECS_FIXTURE_EXIT:", err)
			os.Exit(1)
		}
		os.Exit(forced)
	}
	os.Exit(code)
}

func TestBeta(t *testing.T) {
	specs.DescribeWithReporter(t, "beta", reporter, func(s *specs.Spec) {
		s.It("passes", func(ctx *specs.Context) {
			ctx.Expect(1 + 1).ToEqual(2)
		})

		// Failure and panic are env-gated so the repository's own suite stays green while the
		// integration test can still prove that a red package publishes a complete shard.
		if os.Getenv("GO_SPECS_FIXTURE_FAIL") == "1" {
			s.It("fails on purpose", func(ctx *specs.Context) {
				ctx.Expect(1 + 1).ToEqual(3)
			})
		}
		if os.Getenv("GO_SPECS_FIXTURE_PANIC") == "1" {
			s.It("panics on purpose", func(ctx *specs.Context) {
				panic("fixture panic")
			})
		}
	})
}
