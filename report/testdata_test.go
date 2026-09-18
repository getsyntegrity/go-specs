package report

import "time"

// sampleReport is the one representative NormalizedReport every renderer test (unit + golden)
// and the cross-format integration test render from, so a mismatch between formats always traces
// back to the same input instead of each test inventing slightly different data.
func sampleReport() NormalizedReport {
	c := NewCollector()

	c.SuiteStarted(SuiteStartEvent{Name: "Checkout"})
	finishCase(c, "Checkout pays with a valid card", []string{"Checkout", "pays with a valid card"}, 5*time.Millisecond, false, false, false, "", "")
	finishCase(c, "Checkout rejects an expired card", []string{"Checkout", "rejects an expired card"}, 3*time.Millisecond, true, false, false, "expected status 402, got 200", "")
	finishCase(c, "Checkout crashes on a nil cart", []string{"Checkout", "crashes on a nil cart"}, 2*time.Millisecond, true, false, false,
		"recovered panic: nil pointer dereference", "goroutine 1 [running]:\nmain.crash()\n\t/x/y.go:10 +0x1a")
	finishCase(c, "Checkout skips legacy flow", []string{"Checkout", "skips legacy flow"}, 0, false, true, false, "", "")
	c.SuiteFinished(SuiteEndEvent{Name: "Checkout", Duration: 15 * time.Millisecond})

	c.SuiteStarted(SuiteStartEvent{Name: "Cart"})
	finishCase(c, "Cart totals items", []string{"Cart", "totals items"}, 4*time.Millisecond, false, false, false, "", "")
	finishCase(c, "Cart applies a coupon", []string{"Cart", "applies a coupon"}, 0, false, false, true, "", "")
	c.SuiteFinished(SuiteEndEvent{Name: "Cart", Duration: 4 * time.Millisecond})

	c.SetCoverage(Coverage{
		Packages: []PackageCoverage{
			{ImportPath: "github.com/getsyntegrity/go-specs/report", Covered: 90, Total: 100},
			{ImportPath: "github.com/getsyntegrity/go-specs/specs", Covered: 700, Total: 800},
		},
		Total: AggregateCoverage{Covered: 790, Total: 900},
	})

	return c.Report()
}

func finishCase(c *Collector, name string, path []string, d time.Duration, failed, skipped, filtered bool, message, output string) {
	c.SpecFinished(SpecResultEvent{
		SpecStartEvent: SpecStartEvent{Name: name, Path: path},
		Failed:         failed,
		Skipped:        skipped,
		Filtered:       filtered,
		Duration:       d,
		Message:        message,
		Output:         output,
	})
}
