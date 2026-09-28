package report

import "time"

// SchemaVersion is the current version of NormalizedReport's JSON shape (render_json.go).
// A consumer must ignore unknown fields and may key behavior off this value; it changes when a
// field's meaning changes incompatibly — which includes a new value in a closed vocabulary such
// as Status, since a consumer switching exhaustively over it would misread the document — but
// never for a new field.
//
// History: "1" — initial shape. "2" — adds StatusPending ("pending") to the status vocabulary and
// the pending totals field (issue #208). "3" — adds StatusUnstarted ("unstarted") to the status
// vocabulary, the unstarted totals field, and Case.Declared, for a spec CompiledSuite/Runner
// fail-fast prevented from ever running (issue #274).
const SchemaVersion = "3"

// Status is a case's normalized outcome. It is derived from SpecResultEvent (see
// classifyStatus in collector.go) and is the single vocabulary every renderer maps from —
// no renderer recomputes it from Failed/Skipped/Filtered itself.
type Status string

const (
	StatusPassed Status = "passed"
	StatusFailed Status = "failed" // assertion or hook failure
	StatusError  Status = "error"  // recovered panic or other infrastructure failure
	// StatusSkipped is a compile-time XIt/Skip spec (body never ran) or a spec whose own subtest
	// skipped at runtime via ctx.T.Skip/Skipf/SkipNow (issue #254), where the body did start running.
	StatusSkipped  Status = "skipped"
	StatusFiltered Status = "filtered"
	// StatusPending is a compile-time PendingIt/Pending spec: the specification exists but its
	// implementation does not, distinct from Skipped (intentionally not executed). Body never ran,
	// exactly like Skipped and Filtered; see events.go's SpecResultEvent.Pending.
	StatusPending Status = "pending"
	// StatusUnstarted is a spec that CompiledSuite.SetFailFast(true) or Runner.FailFast prevented
	// from ever being reached, after an earlier spec already failed (issue #274). It is distinct
	// from every other "did not run" status: Skipped/Filtered/Pending are all deliberate outcomes
	// the suite (or an external selector) reached and decided on; Unstarted means the suite never
	// got that far at all. Body never ran, Duration is always 0, and Failed is always false — same
	// shape as Skipped/Filtered/Pending — but an Unstarted spec never counts toward Totals.Total
	// (see Totals.add), unlike every other status: Total keeps its pre-existing meaning of "specs
	// that entered execution", and fail-fast-prevented specs never did. When the unreached spec was
	// itself a compile-time SkipIt/PendingIt, its original declaration survives as Case.Declared.
	StatusUnstarted Status = "unstarted"
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
	//
	// Tagged omitempty because Case itself gets serialized directly (not through a renderer's own
	// DTO) by report/coordination/writer.go's shard envelope: without the tag, every ordinary case
	// in every shard would carry a spurious `"Hook": ""`, which is exactly the per-suite cost H10
	// forbids for a suite that never registers a group hook.
	Hook string `json:"Hook,omitempty"`
	// Declared preserves an Unstarted case's original compile-time declaration — "skip" for a
	// SkipIt/Skip spec, "pending" for a PendingIt/Pending spec — that a fail-fast stop prevented
	// from ever being processed (issue #274). Empty for an ordinary Unstarted spec (declared as
	// neither) and for every non-Unstarted status; it is metadata about what the spec would have
	// been reported as, not a second status field, so renderers still switch on Status alone.
	// Tagged omitempty for the same reason Hook is: most cases and most shards never carry one.
	Declared string `json:"declared,omitempty"`
}

// Totals summarizes a set of cases. Total is always
// Passed+Failed+Error+Skipped+Filtered+Pending — deliberately excluding Unstarted, which counts
// specs a fail-fast stop prevented from ever entering execution (issue #274); Total keeps its
// pre-existing meaning of "specs that entered execution, plus declared SkipIt/PendingIt that were
// actually processed".
type Totals struct {
	Total     int
	Passed    int
	Failed    int
	Error     int
	Skipped   int
	Filtered  int
	Pending   int
	Unstarted int
}

func (t *Totals) add(s Status) {
	if s != StatusUnstarted {
		t.Total++
	}
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
	case StatusUnstarted:
		t.Unstarted++
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
