package report

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// siblingScopesReport is one suite whose two failing "fails" specs live under sibling When blocks,
// so their leaf names are identical and only the enclosing scopes tell them apart (issue #313).
func siblingScopesReport() NormalizedReport {
	return NormalizedReport{
		SchemaVersion: SchemaVersion,
		Execution:     Totals{Total: 2, Failed: 2},
		Duration:      2 * time.Millisecond,
		Suites: []Suite{{
			Name:     "Checkout",
			Totals:   Totals{Total: 2, Failed: 2},
			Duration: 2 * time.Millisecond,
			Cases: []Case{
				{Name: "fails", Path: []string{"Checkout", "when the cart is empty", "fails"}, Status: StatusFailed, Duration: time.Millisecond, Message: "empty boom"},
				{Name: "fails", Path: []string{"Checkout", "when the cart has items", "fails"}, Status: StatusFailed, Duration: time.Millisecond, Message: "items boom"},
			},
		}},
	}
}

func TestSiblingScopesRenderDistinctPathsInTXTAndHTML(t *testing.T) {
	want := []string{
		"Checkout/when the cart is empty/fails",
		"Checkout/when the cart has items/fails",
	}
	var txt, html bytes.Buffer
	if err := RenderTXT(&txt, siblingScopesReport()); err != nil {
		t.Fatalf("RenderTXT: %v", err)
	}
	if err := RenderHTML(&html, siblingScopesReport()); err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}
	for _, w := range want {
		if !strings.Contains(txt.String(), "FAIL  "+w) {
			t.Errorf("TXT lacks %q:\n%s", w, txt.String())
		}
		if !strings.Contains(html.String(), w) {
			t.Errorf("HTML lacks %q:\n%s", w, html.String())
		}
	}
}

func TestSiblingScopesGoldenTXT(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderTXT(&buf, siblingScopesReport()); err != nil {
		t.Fatalf("RenderTXT: %v", err)
	}
	compareGolden(t, "sibling-scopes.txt", buf.Bytes())
}

func TestSiblingScopesGoldenHTML(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderHTML(&buf, siblingScopesReport()); err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}
	compareGolden(t, "sibling-scopes.html", buf.Bytes())
}

func TestCaseDisplayNameIsFullPathForOrdinarySpecs(t *testing.T) {
	cases := []struct {
		name string
		in   Case
		want string
	}{
		{"nested spec", Case{Name: "charges the card", Path: []string{"Checkout", "when cart has items", "charges the card"}}, "Checkout/when cart has items/charges the card"},
		{"top-level spec", Case{Name: "works", Path: []string{"Suite", "works"}}, "Suite/works"},
		{"no path falls back to name", Case{Name: "orphan"}, "orphan"},
		{"name-only path", Case{Name: "solo", Path: []string{"solo"}}, "solo"},
		{"scope containing slash stays one element", Case{Name: "b", Path: []string{"a/b", "b"}}, "a/b/b"},
		{"group hook keeps bracket label", Case{Name: "[BeforeAll]", Hook: "BeforeAll", Path: []string{"Checkout", "when x"}}, "Checkout/when x [BeforeAll]"},
	}
	for _, tc := range cases {
		if got := caseDisplayName(tc.in); got != tc.want {
			t.Errorf("%s: caseDisplayName = %q, want %q", tc.name, got, tc.want)
		}
	}
}
