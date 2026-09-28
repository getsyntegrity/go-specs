package report

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"strings"
	"testing"
)

// twoPackageReport models a module-wide merge (issue #308): two packages declare a suite and a case
// with identical names but different outcomes, so only Suite.Package tells them apart.
func twoPackageReport() NormalizedReport {
	return NormalizedReport{
		SchemaVersion: SchemaVersion,
		Execution:     Totals{Total: 2, Passed: 1, Failed: 1},
		Suites: []Suite{
			{
				Name: "Checkout", Package: "example.com/m/alpha",
				Totals: Totals{Total: 1, Passed: 1},
				Cases:  []Case{{Name: "works", Path: []string{"Checkout", "works"}, Status: StatusPassed}},
			},
			{
				Name: "Checkout", Package: "example.com/m/beta",
				Totals: Totals{Total: 1, Failed: 1},
				Cases:  []Case{{Name: "works", Path: []string{"Checkout", "works"}, Status: StatusFailed, Message: "beta boom"}},
			},
		},
	}
}

func TestPackageProvenanceInJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderJSON(&buf, twoPackageReport()); err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}
	var doc struct {
		SchemaVersion string `json:"schemaVersion"`
		Suites        []struct {
			Name    string `json:"name"`
			Package string `json:"package"`
			Cases   []struct {
				Status string `json:"status"`
			} `json:"cases"`
		} `json:"suites"`
	}
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(doc.Suites) != 2 || doc.Suites[0].Package != "example.com/m/alpha" || doc.Suites[1].Package != "example.com/m/beta" {
		t.Fatalf("suites = %+v, want packages alpha then beta", doc.Suites)
	}
	if doc.Suites[0].Cases[0].Status != "passed" || doc.Suites[1].Cases[0].Status != "failed" {
		t.Fatalf("outcomes were not kept per package: %+v", doc.Suites)
	}
}

func TestPackageProvenanceInTXT(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderTXT(&buf, twoPackageReport()); err != nil {
		t.Fatalf("RenderTXT: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"Suite: Checkout (package example.com/m/alpha)", "Suite: Checkout (package example.com/m/beta)"} {
		if !strings.Contains(out, want) {
			t.Errorf("TXT lacks %q:\n%s", want, out)
		}
	}
}

func TestPackageProvenanceInHTML(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderHTML(&buf, twoPackageReport()); err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"example.com/m/alpha", "example.com/m/beta"} {
		if !strings.Contains(out, want) {
			t.Errorf("HTML lacks package %q:\n%s", want, out)
		}
	}
}

func TestPackageProvenanceInJUnit(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderXML(&buf, twoPackageReport()); err != nil {
		t.Fatalf("RenderXML: %v", err)
	}
	var doc junitTestSuites
	if err := xml.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(doc.Suites) != 2 {
		t.Fatalf("got %d testsuites, want 2", len(doc.Suites))
	}
	if doc.Suites[0].Package != "example.com/m/alpha" || doc.Suites[1].Package != "example.com/m/beta" {
		t.Fatalf("testsuite package attrs = %q, %q", doc.Suites[0].Package, doc.Suites[1].Package)
	}
	a, b := doc.Suites[0].TestCases[0], doc.Suites[1].TestCases[0]
	if a.Name != b.Name {
		t.Fatalf("fixture must share the case name, got %q and %q", a.Name, b.Name)
	}
	if a.ClassName == b.ClassName {
		t.Fatalf("both packages' cases got classname %q, so (classname, name) is ambiguous", a.ClassName)
	}
	if !strings.HasPrefix(a.ClassName, "example.com/m/alpha") || !strings.HasPrefix(b.ClassName, "example.com/m/beta") {
		t.Fatalf("classnames = %q, %q; want package-prefixed", a.ClassName, b.ClassName)
	}
	if b.Failure == nil || a.Failure != nil {
		t.Fatalf("outcomes were not kept per package: alpha=%+v beta=%+v", a, b)
	}
}

// TestEmptyPackageIsOmittedEverywhere proves a single-package (non-merged) report stays exactly as
// before: no package key, attribute, or annotation in any format.
func TestEmptyPackageIsOmittedEverywhere(t *testing.T) {
	r := sampleReport()
	var js, xm, tx bytes.Buffer
	if err := RenderJSON(&js, r); err != nil {
		t.Fatal(err)
	}
	if err := RenderXML(&xm, r); err != nil {
		t.Fatal(err)
	}
	if err := RenderTXT(&tx, r); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(js.String(), `"package"`) {
		t.Errorf("JSON carries a package key for an empty package:\n%s", js.String())
	}
	if strings.Contains(xm.String(), "package=") {
		t.Errorf("XML carries a package attribute for an empty package:\n%s", xm.String())
	}
	if strings.Contains(tx.String(), "(package") {
		t.Errorf("TXT carries a package annotation for an empty package:\n%s", tx.String())
	}
}
