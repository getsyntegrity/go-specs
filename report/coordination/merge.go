package coordination

import (
	"sort"

	"github.com/getsyntegrity/go-specs/report"
)

// mergeReports combines accepted shard envelopes' NormalizedReports into one module-wide report,
// deterministically (contract v1.2.8 §7).
//
// Shards are always merged in PackagePath order, regardless of the order envelopes are passed in:
// this function sorts its own input rather than trusting the caller, so the merge order is a pure
// function of package identity and never of which producer happened to finish first, or which the
// OS happened to glob first (contract v1.2.8 §7, "Output ordering is stable across identical runs
// regardless of package completion order"). Within one shard's own report, suite and case order is
// preserved exactly as the producer's collector built it — nothing here reorders a suite's cases.
//
// Execution totals and Duration are summed across shards. A synthetic BeforeAll/AfterAll hook case
// (#207/#228, Case.Hook) is carried through unchanged: it is not treated specially here, because
// the producer's own report.Collector already counted it into Execution/the suite's Totals exactly
// like any other case, so summing those pre-computed totals is what "counts it like the collector
// does" reduces to.
func mergeReports(envelopes []ShardEnvelope) report.NormalizedReport {
	sorted := make([]ShardEnvelope, len(envelopes))
	copy(sorted, envelopes)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].PackagePath < sorted[j].PackagePath })

	merged := report.NormalizedReport{SchemaVersion: report.SchemaVersion}
	for _, env := range sorted {
		rep := env.Report
		merged.Suites = append(merged.Suites, rep.Suites...)
		merged.Execution = sumTotals(merged.Execution, rep.Execution)
		merged.Duration += rep.Duration
	}
	return merged
}

// sumTotals adds two Totals field by field. Total is always the sum of the other six fields on
// each side, so summing every field independently keeps that invariant on the result.
func sumTotals(a, b report.Totals) report.Totals {
	return report.Totals{
		Total:    a.Total + b.Total,
		Passed:   a.Passed + b.Passed,
		Failed:   a.Failed + b.Failed,
		Error:    a.Error + b.Error,
		Skipped:  a.Skipped + b.Skipped,
		Filtered: a.Filtered + b.Filtered,
		Pending:  a.Pending + b.Pending,
	}
}
