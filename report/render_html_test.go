package report

import (
	"bytes"
	"strings"
	"testing"
)

func TestRenderHTMLGolden(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderHTML(&buf, sampleReport()); err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}
	compareGolden(t, "report.html", buf.Bytes())
}

func TestRenderHTMLSelfContained(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderHTML(&buf, sampleReport()); err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}
	out := buf.String()
	for _, needle := range []string{"http://", "https://", "<link ", "<script src"} {
		if strings.Contains(out, needle) {
			t.Fatalf("HTML report references an external resource (%q), want fully self-contained:\n%s", needle, out)
		}
	}
}

func TestRenderHTMLEscapesUnsafeContent(t *testing.T) {
	r := NormalizedReport{
		SchemaVersion: SchemaVersion,
		Suites: []Suite{{
			Name: "S",
			Cases: []Case{{
				Name:    "spec",
				Status:  StatusFailed,
				Message: `<script>alert(1)</script>`,
			}},
		}},
	}
	var buf bytes.Buffer
	if err := RenderHTML(&buf, r); err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}
	if strings.Contains(buf.String(), "<script>alert(1)</script>") {
		t.Fatalf("unescaped script tag leaked into HTML output:\n%s", buf.String())
	}
}

// TestRenderHTMLShowsPendingStatus proves a pending case gets its own status-pending CSS class and
// its count surfaces in the summary totals.
func TestRenderHTMLShowsPendingStatus(t *testing.T) {
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
	if err := RenderHTML(&buf, r); err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "status-pending") {
		t.Fatalf("expected a status-pending CSS class, got:\n%s", out)
	}
	if !strings.Contains(out, "Pending: <strong>1</strong>") {
		t.Fatalf("expected the summary to show Pending: 1, got:\n%s", out)
	}
}

func TestRenderHTMLShowsSummaryAndCoverage(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderHTML(&buf, sampleReport()); err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}
	out := buf.String()
	for _, needle := range []string{"Checkout", "Cart", "Coverage", "Aggregate", "github.com/getsyntegrity/go-specs/specs"} {
		if !strings.Contains(out, needle) {
			t.Fatalf("HTML output missing %q:\n%s", needle, out)
		}
	}
}
