package producerb

import (
	"fmt"
	"os"
	"testing"

	"github.com/getsyntegrity/go-specs/report"
	"github.com/getsyntegrity/go-specs/report/coordination"
	"github.com/getsyntegrity/go-specs/report/coordination/internal/e2efixture/shared"
	"github.com/getsyntegrity/go-specs/specs"
)

const importPath = "github.com/getsyntegrity/go-specs/report/coordination/internal/e2efixture/producerb"

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

// TestProducerB calls shared.Compute with a non-positive n, exercising the "else" block. Both
// producera and producerb import shared, so `-coverpkg` links and instruments every block of
// shared into both test binaries regardless of which branch each one actually took — the real
// `-coverpkg` overlap this fixture exists to reproduce (contract v1.2.8 §9, finding F7).
func TestProducerB(t *testing.T) {
	specs.DescribeWithReporter(t, "producer-b", reporter, func(s *specs.Spec) {
		s.It("returns zero for a non-positive input through the shared dependency", func(ctx *specs.Context) {
			ctx.Expect(shared.Compute(-1)).ToEqual(0)
		})
		s.It("passes a second, ordinary case", func(ctx *specs.Context) {
			ctx.Expect(2 + 2).ToEqual(4)
		})
	})
}
