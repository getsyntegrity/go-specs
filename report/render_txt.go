package report

import (
	"fmt"
	"io"
	"text/tabwriter"
)

// RenderTXT writes r as a deterministic, human-readable plain-text report suitable for terminal
// output and archived CI logs: execution totals, every failed/errored case with its diagnostics,
// skipped/filtered counts, a package coverage table, and the aggregate.
//
// RenderTXT never writes ANSI escape sequences — there is no color mode to disable here, so a
// file this renders to can never carry accidental color codes. A terminal-only colored view, if
// ever added, belongs in a separate writer that wraps this one rather than a flag on it.
func RenderTXT(w io.Writer, r NormalizedReport) error {
	bw := &errWriter{w: w}

	bw.printf("go-specs report\n")
	bw.printf("Total: %d  Passed: %d  Failed: %d  Error: %d  Skipped: %d  Filtered: %d  Pending: %d\n",
		r.Execution.Total, r.Execution.Passed, r.Execution.Failed, r.Execution.Error,
		r.Execution.Skipped, r.Execution.Filtered, r.Execution.Pending)
	bw.printf("Duration: %ss\n", formatSeconds(r.Duration))

	for _, s := range r.Suites {
		bw.printf("\nSuite: %s\n", s.Name)
		for _, c := range s.Cases {
			renderTXTCase(bw, c)
		}
		bw.printf("  Totals: total=%d passed=%d failed=%d error=%d skipped=%d filtered=%d pending=%d\n",
			s.Totals.Total, s.Totals.Passed, s.Totals.Failed, s.Totals.Error,
			s.Totals.Skipped, s.Totals.Filtered, s.Totals.Pending)
	}

	if len(r.Coverage.Packages) > 0 {
		bw.printf("\nCoverage\n")
		tw := tabwriter.NewWriter(bw, 0, 2, 2, ' ', 0)
		tabPrintf(bw, tw, "Package\tCovered\tTotal\tPercent\n")
		for _, p := range r.Coverage.Packages {
			tabPrintf(bw, tw, "%s\t%d\t%d\t%s%%\n", p.ImportPath, p.Covered, p.Total, formatPercentage(p.Percentage()))
		}
		tabPrintf(bw, tw, "Aggregate\t%d\t%d\t%s%%\n", r.Coverage.Total.Covered, r.Coverage.Total.Total, formatPercentage(r.Coverage.Total.Percentage()))
		if err := tw.Flush(); err != nil && bw.err == nil {
			bw.err = err
		}
	}

	return bw.err
}

func renderTXTCase(bw *errWriter, c Case) {
	switch c.Status {
	case StatusFailed:
		bw.printf("  FAIL  %s (%ss)\n", c.Name, formatSeconds(c.Duration))
	case StatusError:
		bw.printf("  ERROR %s (%ss)\n", c.Name, formatSeconds(c.Duration))
	default:
		return // passed/skipped/filtered/pending cases are covered by the suite totals line only
	}
	if c.Message != "" {
		bw.printf("        message: %s\n", c.Message)
	}
	if c.Output != "" {
		bw.printf("        output: %s\n", c.Output)
	}
}

// errWriter accumulates the first write error instead of threading one through every printf
// call; RenderTXT surfaces it once at the end via bw.err.
type errWriter struct {
	w   io.Writer
	err error
}

func (e *errWriter) Write(p []byte) (int, error) {
	if e.err != nil {
		return 0, e.err
	}
	n, err := e.w.Write(p)
	if err != nil {
		e.err = err
	}
	return n, err
}

func (e *errWriter) printf(format string, args ...any) {
	if e.err != nil {
		return
	}
	_, err := fmt.Fprintf(e.w, format, args...)
	if err != nil {
		e.err = err
	}
}

// tabPrintf writes a formatted row to tw (a *tabwriter.Writer over bw) and records the first
// write error on bw, matching errWriter.printf's convention — tabwriter's own Write can fail
// once it flushes buffered rows to the underlying writer, so this error can't be ignored either.
func tabPrintf(bw *errWriter, tw io.Writer, format string, args ...any) {
	if bw.err != nil {
		return
	}
	if _, err := fmt.Fprintf(tw, format, args...); err != nil {
		bw.err = err
	}
}
