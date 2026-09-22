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

// TestRenderJSONPendingReportIsSchemaV2 pins that a report able to carry status:"pending" says
// so: "pending" is a new value in the existing status field, not a new field, so a v1 consumer
// with an exhaustive status switch would misread it. The literal "2" is deliberate — comparing
// against SchemaVersion itself would pass whatever the constant held.
func TestRenderJSONPendingReportIsSchemaV2(t *testing.T) {
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
	if doc.SchemaVersion != "2" {
		t.Fatalf("schemaVersion = %q, want %q", doc.SchemaVersion, "2")
	}
	if len(doc.Suites) != 1 || len(doc.Suites[0].Cases) != 1 || doc.Suites[0].Cases[0].Status != "pending" {
		t.Fatalf("got %+v, want one suite with one pending case", doc.Suites)
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
