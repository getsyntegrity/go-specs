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
