package report

import "time"

// SchemaVersion is the current version of NormalizedReport's JSON shape (render_json.go).
// A consumer must ignore unknown fields and may key behavior off this value; it changes when a
// field's meaning changes incompatibly — which includes a new value in a closed vocabulary such
// as Status, since a consumer switching exhaustively over it would misread the document — but
// never for a new field.
//
// History: "1" — initial shape. "2" — adds StatusPending ("pending") to the status vocabulary and
// the pending totals field (issue #208).
const SchemaVersion = "2"

// Status is a case's normalized outcome. It is derived from SpecResultEvent (see
// classifyStatus in collector.go) and is the single vocabulary every renderer maps from —
// no renderer recomputes it from Failed/Skipped/Filtered itself.
type Status string

const (
	StatusPassed   Status = "passed"
	StatusFailed   Status = "failed"  // assertion or hook failure
	StatusError    Status = "error"   // recovered panic or other infrastructure failure
	StatusSkipped  Status = "skipped" // compile-time XIt/Skip; body never ran
	StatusFiltered Status = "filtered"
	// StatusPending is a compile-time PendingIt/Pending spec: the specification exists but its
	// implementation does not, distinct from Skipped (intentionally not executed). Body never ran,
	// exactly like Skipped and Filtered; see events.go's SpecResultEvent.Pending.
	StatusPending Status = "pending"
)

// Case is one normalized spec result: an executed It, or a generated candidate.
//
// Name and Path are exactly SpecStartEvent.Name/Path (see report/events.go) — for a generated
// candidate, Name already carries its case ordinal, values and (when seeded) its seed, because
// that identity is baked into the name string by the execution core (specs.generatedCaseName /
// generatedCaseReportName) rather than carried as separate structured fields on the event today.
// A renderer that wants seed/candidate-value columns must parse them out of Name; the normalized
// model does not split them out, so nothing here can silently drift from what the core actually
// named the candidate.
type Case struct {
	Name     string
	Path     []string
	Status   Status
	Duration time.Duration
	Message  string // failure/error summary; empty unless Status is Failed or Error
	Output   string // full output/stack trace, when the source event carried one
	// Hook is SpecResultEvent.Hook.String() (see report/events.go's HookKind): "BeforeAll"/"AfterAll"
	// for a synthetic group hook case, empty for a real spec. Collector converts the compact
	// HookKind enum to this plain string once per case, so every renderer and the JSON/XML output
	// keep working with a string exactly as before HookKind existed. This is additive, not a
	// schema-version change — see SchemaVersion's doc comment: a new field never bumps it, only a
	// new value in a closed vocabulary such as Status would. A report with no group hooks never sets
	// it, and every renderer keeps its existing byte-for-byte output for that case (issue #207 H10).
	Hook string
}

// Totals summarizes a set of cases. Total is always Passed+Failed+Error+Skipped+Filtered+Pending.
type Totals struct {
	Total    int
	Passed   int
	Failed   int
	Error    int
	Skipped  int
	Filtered int
	Pending  int
}

func (t *Totals) add(s Status) {
	t.Total++
	switch s {
	case StatusPassed:
		t.Passed++
	case StatusFailed:
		t.Failed++
	case StatusError:
		t.Error++
	case StatusSkipped:
		t.Skipped++
	case StatusFiltered:
		t.Filtered++
	case StatusPending:
		t.Pending++
	}
}

// Suite is one top-level Describe/DescribeFlat run, in SuiteStarted order.
type Suite struct {
	Name     string
	Cases    []Case
	Duration time.Duration
	Totals   Totals
}

// PackageCoverage is one Go package's statement coverage, read from a `go test -coverprofile`
// profile (see coverage.go). Percentage is Covered/Total*100, or 0 when Total is 0 — a package
// with no executable statements contributes 0/0 to the aggregate, never NaN or a distorting 100.
type PackageCoverage struct {
	ImportPath string
	Covered    int
	Total      int
}

// Percentage returns this package's coverage percentage, or 0 when Total is 0.
func (p PackageCoverage) Percentage() float64 {
	if p.Total == 0 {
		return 0
	}
	return float64(p.Covered) / float64(p.Total) * 100
}

// AggregateCoverage is the module-wide total, always summed from package statement counts —
// never averaged from package percentages (see Coverage.Total in coverage.go).
type AggregateCoverage struct {
	Covered int
	Total   int
}

// Percentage returns the aggregate coverage percentage, or 0 when Total is 0.
func (a AggregateCoverage) Percentage() float64 {
	if a.Total == 0 {
		return 0
	}
	return float64(a.Covered) / float64(a.Total) * 100
}

// Coverage is the full coverage section of a NormalizedReport: every package in the profile,
// ordered by ImportPath, plus the module-wide aggregate.
type Coverage struct {
	Packages []PackageCoverage
	Total    AggregateCoverage
}

// NormalizedReport is the one model every renderer (XML, HTML, TXT, JSON) consumes. It combines
// SpecResultEvent execution data with a Go coverage profile; renderers must format this data,
// never recalculate it.
type NormalizedReport struct {
	SchemaVersion string
	Execution     Totals
	Duration      time.Duration
	Suites        []Suite
	Coverage      Coverage
}
