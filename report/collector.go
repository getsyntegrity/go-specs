package report

import "time"

// Collector implements EventReporter and builds a NormalizedReport from the events it receives.
//
// Collector assumes suites do not interleave: it attributes each finished spec to whichever
// suite's SuiteStarted was most recently observed without a matching SuiteFinished yet. That
// holds for every reporting setup this package documents (one DescribeWithReporter /
// RunShardWithReporter call feeding one Collector) but would misattribute specs if two top-level
// Describe blocks ran their bodies concurrently against the same Collector — the SpecStartEvent /
// SpecResultEvent stream carries no suite identity of its own to disambiguate that case. A future
// change wiring reporting into concurrent top-level suites needs a suite identity on the event
// itself before this assumption can be lifted.
//
// A *Collector is not safe for concurrent use; the runner already serializes EventReporter calls
// on a Collector's behalf (see specs.reporterObserver), so no locking is done here.
type Collector struct {
	report NormalizedReport
	openAt int // index into report.Suites of the currently open suite, or -1
}

// NewCollector returns an empty Collector ready to receive events.
func NewCollector() *Collector {
	return &Collector{report: NormalizedReport{SchemaVersion: SchemaVersion}, openAt: -1}
}

// SuiteStarted implements EventReporter.
func (c *Collector) SuiteStarted(e SuiteStartEvent) {
	c.report.Suites = append(c.report.Suites, Suite{Name: e.Name})
	c.openAt = len(c.report.Suites) - 1
}

// SuiteFinished implements EventReporter.
func (c *Collector) SuiteFinished(e SuiteEndEvent) {
	if c.openAt < 0 || c.openAt >= len(c.report.Suites) {
		return
	}
	c.report.Suites[c.openAt].Duration = e.Duration
	c.openAt = -1
}

// SpecStarted implements EventReporter. Collector does not need it: every field it would carry
// is reproduced verbatim on the matching SpecResultEvent (see events.go's SpecStartEvent doc).
func (c *Collector) SpecStarted(SpecStartEvent) {}

// SpecFinished implements EventReporter.
func (c *Collector) SpecFinished(e SpecResultEvent) {
	if c.openAt < 0 || c.openAt >= len(c.report.Suites) {
		return
	}
	status := classifyStatus(e)
	c.report.Suites[c.openAt].Cases = append(c.report.Suites[c.openAt].Cases, Case{
		Name:     e.Name,
		Path:     e.Path,
		Status:   status,
		Duration: e.Duration,
		Message:  e.Message,
		Output:   e.Output,
	})
	c.report.Suites[c.openAt].Totals.add(status)
	c.report.Execution.add(status)
}

// classifyStatus derives a Status from a SpecResultEvent. Filtered, Skipped and Failed are
// mutually exclusive on the source event (see events.go). Within Failed, an event with non-empty
// Output carries a recovered panic's stack trace — the only case that produces one today, per
// events.go's Output doc — so it is classified as Error rather than Failed; an ordinary assertion
// failure has no Output and is classified as Failed. This heuristic needs revisiting if a future
// event producer starts attaching Output to non-panic failures too.
func classifyStatus(e SpecResultEvent) Status {
	switch {
	case e.Filtered:
		return StatusFiltered
	case e.Skipped:
		return StatusSkipped
	case e.Failed && e.Output != "":
		return StatusError
	case e.Failed:
		return StatusFailed
	default:
		return StatusPassed
	}
}

// SetCoverage attaches parsed coverage data (see ParseCoverageProfile) to the report being built.
func (c *Collector) SetCoverage(cov Coverage) {
	c.report.Coverage = cov
}

// Report returns the NormalizedReport built so far, with Duration set to the sum of every
// finished suite's Duration. Call it only after every suite the caller cares about has finished;
// a still-open suite contributes 0 to Duration and keeps whatever cases it had received so far.
func (c *Collector) Report() NormalizedReport {
	r := c.report
	r.Suites = append([]Suite(nil), c.report.Suites...)
	var total time.Duration
	for _, s := range r.Suites {
		total += s.Duration
	}
	r.Duration = total
	return r
}

var _ EventReporter = (*Collector)(nil)
