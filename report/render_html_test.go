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
