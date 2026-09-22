package report

import (
	"bytes"
	"encoding/xml"
	"strings"
	"testing"
)

func TestRenderXMLGolden(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderXML(&buf, sampleReport()); err != nil {
		t.Fatalf("RenderXML: %v", err)
	}
	compareGolden(t, "report.xml", buf.Bytes())
}

func TestRenderXMLIsValidJUnit(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderXML(&buf, sampleReport()); err != nil {
		t.Fatalf("RenderXML: %v", err)
	}

	var doc junitTestSuites
	if err := xml.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("a standard xml.Unmarshal could not parse RenderXML's output: %v", err)
	}
	if doc.Tests != 6 || doc.Failures != 1 || doc.Errors != 1 || doc.Skipped != 2 {
		t.Fatalf("testsuites totals = %+v, want tests=6 failures=1 errors=1 skipped=2", doc)
	}
	if len(doc.Suites) != 2 {
		t.Fatalf("got %d testsuite elements, want 2", len(doc.Suites))
	}
	checkout := doc.Suites[0]
	if checkout.Name != "Checkout" || checkout.Tests != 4 {
		t.Fatalf("Checkout testsuite = %+v", checkout)
	}
	var sawFailure, sawError, sawSkipped bool
	for _, tc := range checkout.TestCases {
		switch {
		case tc.Failure != nil:
			sawFailure = true
			if tc.Failure.Message != "expected status 402, got 200" {
				t.Fatalf("failure message = %q", tc.Failure.Message)
			}
		case tc.Error != nil:
			sawError = true
			if !strings.Contains(tc.Error.Body, "goroutine 1") {
				t.Fatalf("error body missing stack trace: %q", tc.Error.Body)
			}
		case tc.Skipped != nil:
			sawSkipped = true
		}
	}
	if !sawFailure || !sawError || !sawSkipped {
		t.Fatalf("missing expected case kinds: failure=%v error=%v skipped=%v", sawFailure, sawError, sawSkipped)
	}

	// Coverage properties are present and carry the aggregate arithmetic, not per-package averages.
	found := map[string]string{}
	for _, p := range checkout.Properties {
		found[p.Name] = p.Value
	}
	if found["go-specs.coverage.aggregate.covered"] != "790" || found["go-specs.coverage.aggregate.total"] != "900" {
		t.Fatalf("aggregate coverage properties = %+v", found)
	}
}

// TestRenderXMLNestedScopesDisambiguateIdentity proves that two cases sharing the same leaf name
// under different When branches of the same suite get different (classname, name) pairs: the
// classname is derived from each case's own enclosing scopes (Case.Path), not just the suite name,
// so a JUnit consumer can tell which branch failed instead of merging both cases' history.
func TestRenderXMLNestedScopesDisambiguateIdentity(t *testing.T) {
	r := NormalizedReport{
		SchemaVersion: SchemaVersion,
		Suites: []Suite{{
			Name: "Checkout",
			Cases: []Case{
				{Name: "is valid", Path: []string{"Checkout", "when the cart is empty", "is valid"}, Status: StatusPassed},
				{Name: "is valid", Path: []string{"Checkout", "when the cart has items", "is valid"}, Status: StatusFailed, Message: "boom"},
			},
		}},
	}
	var buf bytes.Buffer
	if err := RenderXML(&buf, r); err != nil {
		t.Fatalf("RenderXML: %v", err)
	}
	var doc junitTestSuites
	if err := xml.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("xml.Unmarshal: %v", err)
	}
	cases := doc.Suites[0].TestCases
	if len(cases) != 2 {
		t.Fatalf("got %d testcases, want 2", len(cases))
	}
	if cases[0].Name != cases[1].Name {
		t.Fatalf("expected both cases to share the same leaf name, got %q and %q", cases[0].Name, cases[1].Name)
	}
	if cases[0].ClassName == cases[1].ClassName {
		t.Fatalf("both cases got the same classname %q, so a JUnit consumer cannot distinguish them", cases[0].ClassName)
	}
	wantA := "Checkout/when the cart is empty"
	wantB := "Checkout/when the cart has items"
	if cases[0].ClassName != wantA || cases[1].ClassName != wantB {
		t.Fatalf("classnames = %q, %q; want %q, %q", cases[0].ClassName, cases[1].ClassName, wantA, wantB)
	}
}

// TestRenderXMLPendingRendersAsSkippedWithMessage proves a pending case renders as
// <skipped message="pending"/> (JUnit has no pending state) and is counted in the skipped
// attribute at both the <testsuites> and <testsuite> level, exactly as Filtered is today.
func TestRenderXMLPendingRendersAsSkippedWithMessage(t *testing.T) {
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
	if err := RenderXML(&buf, r); err != nil {
		t.Fatalf("RenderXML: %v", err)
	}
	var doc junitTestSuites
	if err := xml.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("xml.Unmarshal: %v", err)
	}
	if doc.Skipped != 1 {
		t.Fatalf("testsuites skipped attr = %d, want 1", doc.Skipped)
	}
	if len(doc.Suites) != 1 || doc.Suites[0].Skipped != 1 {
		t.Fatalf("testsuite skipped attr = %+v, want 1", doc.Suites)
	}
	cases := doc.Suites[0].TestCases
	if len(cases) != 1 || cases[0].Skipped == nil {
		t.Fatalf("expected one <skipped> testcase, got %+v", cases)
	}
	if cases[0].Skipped.Message != "pending" {
		t.Fatalf("skipped message = %q, want %q", cases[0].Skipped.Message, "pending")
	}
}

func TestRenderXMLEscapesUnsafeContent(t *testing.T) {
	r := NormalizedReport{
		SchemaVersion: SchemaVersion,
		Suites: []Suite{{
			Name: "S",
			Cases: []Case{{
				Name:    `spec with <tag> & "quotes"`,
				Status:  StatusFailed,
				Message: `<script>alert(1)</script>`,
			}},
		}},
	}
	var buf bytes.Buffer
	if err := RenderXML(&buf, r); err != nil {
		t.Fatalf("RenderXML: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "<script>") || strings.Contains(out, "<tag>") {
		t.Fatalf("unescaped markup leaked into XML output:\n%s", out)
	}
	var doc junitTestSuites
	if err := xml.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("xml.Unmarshal: %v", err)
	}
}
