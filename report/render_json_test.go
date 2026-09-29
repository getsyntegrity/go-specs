package report

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestRenderJSONGolden(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderJSON(&buf, sampleReport()); err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}
	compareGolden(t, "report.json", buf.Bytes())
}

func TestRenderJSONShapeAndArithmetic(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderJSON(&buf, sampleReport()); err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}

	var doc jsonReport
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if doc.SchemaVersion != SchemaVersion {
		t.Fatalf("schemaVersion = %q, want %q", doc.SchemaVersion, SchemaVersion)
	}
	if doc.Execution.Total != 6 || doc.Execution.Passed != 2 || doc.Execution.Failed != 1 ||
		doc.Execution.Error != 1 || doc.Execution.Skipped != 1 || doc.Execution.Filtered != 1 {
		t.Fatalf("execution = %+v", doc.Execution)
	}
	if len(doc.Suites) != 2 {
		t.Fatalf("got %d suites, want 2", len(doc.Suites))
	}

	// Aggregate coverage percentage must come from summed statements: 790/900*100 ≈ 87.78, not
	// an average of the two package percentages (90% and 87.5%, which would be ~88.75%).
	if doc.Coverage.Total.Covered != 790 || doc.Coverage.Total.Total != 900 {
		t.Fatalf("aggregate coverage = %+v", doc.Coverage.Total)
	}
	wantPct := roundPercentage(790.0 / 900.0 * 100)
	if doc.Coverage.Total.Percentage != wantPct {
		t.Fatalf("aggregate percentage = %v, want %v", doc.Coverage.Total.Percentage, wantPct)
	}
}

// TestRenderJSONNilPathRendersAsEmptyArray proves a case with no Path renders as "path":[],
// never "path":null — a consumer should never have to special-case a null array.
func TestRenderJSONNilPathRendersAsEmptyArray(t *testing.T) {
	r := NormalizedReport{
		SchemaVersion: SchemaVersion,
		Suites:        []Suite{{Name: "S", Cases: []Case{{Name: "c", Status: StatusPassed, Path: nil}}}},
	}
	var buf bytes.Buffer
	if err := RenderJSON(&buf, r); err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}
	if bytes.Contains(buf.Bytes(), []byte(`"path": null`)) {
		t.Fatalf("nil Path rendered as null:\n%s", buf.String())
	}
	if !bytes.Contains(buf.Bytes(), []byte(`"path": []`)) {
		t.Fatalf("expected \"path\": [] for a nil Path, got:\n%s", buf.String())
	}
}

// TestRenderJSONPendingStatus proves a pending case renders status:"pending" and is counted in
// the totals' pending field, distinct from skipped and filtered.
func TestRenderJSONPendingStatus(t *testing.T) {
	r := NormalizedReport{
		SchemaVersion: SchemaVersion,
		Execution:     Totals{Total: 1, Pending: 1},
		Suites: []Suite{{
			Name:   "S",
			Totals: Totals{Total: 1, Pending: 1},
			Cases:  []Case{{Name: "not implemented yet", Status: StatusPending}},
		}},
	}
	var buf bytes.Buffer
	if err := RenderJSON(&buf, r); err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}

	var doc jsonReport
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if doc.Execution.Pending != 1 {
		t.Fatalf("execution.pending = %d, want 1", doc.Execution.Pending)
	}
	if len(doc.Suites) != 1 || len(doc.Suites[0].Cases) != 1 {
		t.Fatalf("got %+v, want one suite with one case", doc.Suites)
	}
	if doc.Suites[0].Cases[0].Status != "pending" {
		t.Fatalf("case status = %q, want %q", doc.Suites[0].Cases[0].Status, "pending")
	}
	if doc.Suites[0].Totals.Pending != 1 {
		t.Fatalf("suite totals.pending = %d, want 1", doc.Suites[0].Totals.Pending)
	}
}

// TestRenderJSONPendingReportIsCurrentSchema pins that a report able to carry status:"pending"
// still declares the module's current SchemaVersion. Pending itself bumped the schema from "1" to
// "2" (#208); the schema has since moved on again to "3" for status:"unstarted" (#274), which
// changes the same closed vocabulary a v1/v2 consumer would switch exhaustively over — so a report
// carrying an older addition like pending must still report today's version, not linger on "2".
// The literal "3" is deliberate — comparing against SchemaVersion itself would pass whatever the
// constant held.
func TestRenderJSONPendingReportIsCurrentSchema(t *testing.T) {
	c := NewCollector()
	c.SuiteStarted(SuiteStartEvent{Name: "S"})
	c.SpecFinished(SpecResultEvent{SpecStartEvent: SpecStartEvent{Name: "not implemented yet"}, Pending: true})
	c.SuiteFinished(SuiteEndEvent{Name: "S", TotalSpecs: 1, PendingSpecs: 1})

	var buf bytes.Buffer
	if err := RenderJSON(&buf, c.Report()); err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}
	var doc jsonReport
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if doc.SchemaVersion != "3" {
		t.Fatalf("schemaVersion = %q, want %q", doc.SchemaVersion, "3")
	}
	if len(doc.Suites) != 1 || len(doc.Suites[0].Cases) != 1 || doc.Suites[0].Cases[0].Status != "pending" {
		t.Fatalf("got %+v, want one suite with one pending case", doc.Suites)
	}
}

// TestRenderJSONUnstartedStatus proves a spec fail-fast prevented from running (issue #274) renders
// status:"unstarted", is counted in the totals' unstarted field (never in total, per the maintainer
// decision), and carries its original SkipIt/PendingIt declaration in the new "declared" field when
// it was known to be one — omitted entirely for a plain unstarted spec.
func TestRenderJSONUnstartedStatus(t *testing.T) {
	r := NormalizedReport{
		SchemaVersion: SchemaVersion,
		Execution:     Totals{Total: 0, Unstarted: 2},
		Suites: []Suite{{
			Name:   "S",
			Totals: Totals{Total: 0, Unstarted: 2},
			Cases: []Case{
				{Name: "never reached", Status: StatusUnstarted},
				{Name: "never reached (declared pending)", Status: StatusUnstarted, Declared: "pending"},
			},
		}},
	}
	var buf bytes.Buffer
	if err := RenderJSON(&buf, r); err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}

	var doc jsonReport
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if doc.Execution.Unstarted != 2 {
		t.Fatalf("execution.unstarted = %d, want 2", doc.Execution.Unstarted)
	}
	if doc.Execution.Total != 0 {
		t.Fatalf("execution.total = %d, want 0 (an unstarted spec never counts toward total)", doc.Execution.Total)
	}
	if len(doc.Suites) != 1 || len(doc.Suites[0].Cases) != 2 {
		t.Fatalf("got %+v, want one suite with two cases", doc.Suites)
	}
	if got := doc.Suites[0].Cases[0].Status; got != "unstarted" {
		t.Fatalf("case status = %q, want %q", got, "unstarted")
	}
	if got := doc.Suites[0].Cases[1].Declared; got != "pending" {
		t.Fatalf("case declared = %q, want %q", got, "pending")
	}
	if got := doc.Suites[0].Cases[0].Declared; got != "" {
		t.Fatalf("plain unstarted case declared = %q, want empty", got)
	}
	if bytes.Contains(buf.Bytes(), []byte(`"declared": ""`)) {
		t.Fatalf("expected omitempty to drop an empty \"declared\" key, got:\n%s", buf.String())
	}
	if doc.Suites[0].Totals.Unstarted != 2 {
		t.Fatalf("suite totals.unstarted = %d, want 2", doc.Suites[0].Totals.Unstarted)
	}
}

// TestRenderJSONUnknownFieldsIgnorable proves a consumer decoding into a struct with only a
// subset of fields does not fail — the forward-compatibility promise the issue requires.
func TestRenderJSONUnknownFieldsIgnorable(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderJSON(&buf, sampleReport()); err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}
	var minimal struct {
		SchemaVersion string `json:"schemaVersion"`
	}
	if err := json.Unmarshal(buf.Bytes(), &minimal); err != nil {
		t.Fatalf("a minimal consumer struct failed to decode: %v", err)
	}
	if minimal.SchemaVersion != SchemaVersion {
		t.Fatalf("schemaVersion = %q", minimal.SchemaVersion)
	}
}
