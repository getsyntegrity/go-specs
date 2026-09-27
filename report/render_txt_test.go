package report

import (
	"bytes"
	"strings"
	"testing"
)

func TestRenderTXTGolden(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderTXT(&buf, sampleReport()); err != nil {
		t.Fatalf("RenderTXT: %v", err)
	}
	compareGolden(t, "report.txt", buf.Bytes())
}

func TestRenderTXTNoANSI(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderTXT(&buf, sampleReport()); err != nil {
		t.Fatalf("RenderTXT: %v", err)
	}
	if bytes.ContainsRune(buf.Bytes(), 0x1b) {
		t.Fatalf("output contains an ANSI escape byte (0x1b), want none:\n%q", buf.String())
	}
}

func TestRenderTXTIncludesDiagnosticsForFailuresOnly(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderTXT(&buf, sampleReport()); err != nil {
		t.Fatalf("RenderTXT: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "expected status 402, got 200") {
		t.Fatalf("missing failure message in output:\n%s", out)
	}
	if !strings.Contains(out, "goroutine 1") {
		t.Fatalf("missing error stack trace in output:\n%s", out)
	}
	if !strings.Contains(out, "Aggregate") {
		t.Fatalf("missing aggregate coverage row in output:\n%s", out)
	}
}

// TestRenderTXTIncludesPendingInTotals proves pending specs surface in the totals lines (overall
// and per-suite), same as skipped/filtered — no per-case detail line, since a pending spec never
// ran and has no diagnostics to show.
func TestRenderTXTIncludesPendingInTotals(t *testing.T) {
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
	if err := RenderTXT(&buf, r); err != nil {
		t.Fatalf("RenderTXT: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Pending: 1") {
		t.Fatalf("expected overall totals line to include \"Pending: 1\", got:\n%s", out)
	}
	if !strings.Contains(out, "pending=1") {
		t.Fatalf("expected suite totals line to include \"pending=1\", got:\n%s", out)
	}
	if strings.Contains(out, "not implemented yet") {
		t.Fatalf("expected no per-case detail line for a pending spec, got:\n%s", out)
	}
}

func TestRenderTXTDeterministic(t *testing.T) {
	r := sampleReport()
	var a, b bytes.Buffer
	if err := RenderTXT(&a, r); err != nil {
		t.Fatalf("RenderTXT: %v", err)
	}
	if err := RenderTXT(&b, r); err != nil {
		t.Fatalf("RenderTXT: %v", err)
	}
	if a.String() != b.String() {
		t.Fatal("RenderTXT produced different output for identical input")
	}
}
