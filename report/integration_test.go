package report_test

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/getsyntegrity/go-specs/report"
	"github.com/getsyntegrity/go-specs/specs"
)

// integrationCoverageProfile is a hand-written Go coverage profile spanning two packages, fed
// through report.ParseCoverageProfile exactly as a real `go test -coverprofile` file would be.
// checkout: 3 covered + 2 uncovered = 5 statements. cart: 4 covered = 4 statements.
// Aggregate: 7/9 statements (~77.78%) — from summed statement counts, never averaged percentages.
const integrationCoverageProfile = `mode: set
github.com/getsyntegrity/go-specs/examplepkg/checkout/checkout.go:10.2,12.3 3 1
github.com/getsyntegrity/go-specs/examplepkg/checkout/checkout.go:14.2,16.3 2 0
github.com/getsyntegrity/go-specs/examplepkg/cart/cart.go:5.2,9.3 4 1
`

// TestIntegrationAllFormatsAgree runs a representative suite — passing, failing, and panicking
// specs across two suites, plus a real coverage profile — through the actual
// specs.DescribeWithReporter -> MultiFormatReporter pipeline, then reads all four rendered
// reports back with independent decoders and checks their totals and coverage agree exactly
// (issue #141's acceptance criterion for this PR's scope).
//
// The suite runs in a subprocess (mirrors specs' own recovery-test pattern, e.g.
// block_runner_recovery_test.go): the failing and panicking specs are expected to fail their
// *testing.T subtests, and Go always propagates a subtest failure to its parent — there is no way
// to exercise that path through the real public DescribeWithReporter entry point without also
// failing this test.
//
// Skipped and Filtered are exercised exhaustively elsewhere (collector_test.go, the golden
// fixtures) via classifyStatus directly: the public top-level Describe DSL has no supported way
// to produce them today (Skip/SkipIt exist only on the lower-level Builder type), so this suite's
// "representative" statuses are Passed, Failed and Error — everything DescribeWithReporter can
// produce through its public API.
func TestIntegrationAllFormatsAgree(t *testing.T) {
	if dir := os.Getenv("GO_SPECS_INTEGRATION_DIR"); dir != "" {
		runIntegrationSuite(t, dir)
		return
	}

	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestIntegrationAllFormatsAgree$")
	cmd.Env = append(os.Environ(), "GO_SPECS_INTEGRATION_DIR="+dir)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected the subprocess suite (a failing and a panicking spec) to fail, but it passed:\n%s", out)
	}

	xmlDoc := readIntegrationXML(t, filepath.Join(dir, "report.xml"))
	jsonDoc := readIntegrationJSON(t, filepath.Join(dir, "report.json"))
	txt := readIntegrationFile(t, filepath.Join(dir, "report.txt"))
	html := readIntegrationFile(t, filepath.Join(dir, "report.html"))

	// Known shape of the suite below: 4 specs total, 2 passed, 1 failed, 1 errored (panic), none
	// skipped or filtered (see the doc comment on why).
	wantTotal, wantPassed, wantFailed, wantError, wantSkipped, wantFiltered := 4, 2, 1, 1, 0, 0
	if jsonDoc.Execution.Total != wantTotal || jsonDoc.Execution.Passed != wantPassed ||
		jsonDoc.Execution.Failed != wantFailed || jsonDoc.Execution.Error != wantError ||
		jsonDoc.Execution.Skipped != wantSkipped || jsonDoc.Execution.Filtered != wantFiltered {
		t.Fatalf("json execution totals = %+v, want total=%d passed=%d failed=%d error=%d skipped=%d filtered=%d",
			jsonDoc.Execution, wantTotal, wantPassed, wantFailed, wantError, wantSkipped, wantFiltered)
	}
	if len(jsonDoc.Suites) != 2 || jsonDoc.Suites[0].Name != "Checkout" || jsonDoc.Suites[1].Name != "Cart" {
		t.Fatalf("json suites = %+v, want [Checkout, Cart]", jsonDoc.Suites)
	}

	// Coverage arithmetic: summed statements, not averaged percentages (7/9 ≈ 77.78%, never the
	// average of checkout's 60% and cart's 100%, which would be 80%).
	wantCovered, wantTotalStmts := 7, 9
	if jsonDoc.Coverage.Total.Covered != wantCovered || jsonDoc.Coverage.Total.Total != wantTotalStmts {
		t.Fatalf("json aggregate coverage = %+v, want covered=%d total=%d", jsonDoc.Coverage.Total, wantCovered, wantTotalStmts)
	}

	// XML must agree with JSON on every total and on the aggregate coverage it carries as properties.
	if xmlDoc.Tests != wantTotal || xmlDoc.Failures != wantFailed || xmlDoc.Errors != wantError ||
		xmlDoc.Skipped != wantSkipped+wantFiltered {
		t.Fatalf("xml testsuites totals = %+v, want tests=%d failures=%d errors=%d skipped=%d",
			xmlDoc, wantTotal, wantFailed, wantError, wantSkipped+wantFiltered)
	}
	if len(xmlDoc.Suites) != 2 {
		t.Fatalf("got %d xml testsuite elements, want 2", len(xmlDoc.Suites))
	}
	xmlCovered, xmlTotal := xmlCoverageProperties(t, xmlDoc.Suites[0])
	if xmlCovered != wantCovered || xmlTotal != wantTotalStmts {
		t.Fatalf("xml aggregate coverage properties = covered=%d total=%d, want covered=%d total=%d",
			xmlCovered, xmlTotal, wantCovered, wantTotalStmts)
	}

	// TXT must show the exact same execution totals line and the exact same aggregate row.
	wantTotalsLine := fmt.Sprintf("Total: %d  Passed: %d  Failed: %d  Error: %d  Skipped: %d  Filtered: %d",
		wantTotal, wantPassed, wantFailed, wantError, wantSkipped, wantFiltered)
	if !strings.Contains(txt, wantTotalsLine) {
		t.Fatalf("txt report missing totals line %q:\n%s", wantTotalsLine, txt)
	}
	aggregateRow := regexp.MustCompile(`Aggregate\s+7\s+9\s+77\.78%`)
	if !aggregateRow.MatchString(txt) {
		t.Fatalf("txt report missing aggregate coverage row matching %s:\n%s", aggregateRow, txt)
	}

	// HTML must show the same totals and the same aggregate coverage cells.
	for _, needle := range []string{
		fmt.Sprintf("Total: <strong>%d</strong>", wantTotal),
		fmt.Sprintf("Passed: <strong>%d</strong>", wantPassed),
		fmt.Sprintf("Failed: <strong>%d</strong>", wantFailed),
		fmt.Sprintf("Error: <strong>%d</strong>", wantError),
	} {
		if !strings.Contains(html, needle) {
			t.Fatalf("html report missing %q:\n%s", needle, html)
		}
	}
	if !strings.Contains(html, fmt.Sprintf("<td>%d</td>\n  <td>%d</td>", wantCovered, wantTotalStmts)) {
		t.Fatalf("html report missing aggregate coverage cells (covered=%d total=%d):\n%s", wantCovered, wantTotalStmts, html)
	}

	// The panicking spec's diagnostics (a "panic: boom" summary message plus its recovered stack
	// trace as the output body) must have propagated identically into every format that carries
	// case-level diagnostics, not just been counted.
	xmlMessage, xmlBody := xmlErrorDiagnostics(t, xmlDoc)
	jsonMessage, jsonOutput := jsonErrorDiagnostics(t, jsonDoc)
	for _, msg := range []string{txt, html, xmlMessage, jsonMessage} {
		if !strings.Contains(msg, "panic: boom") {
			t.Fatalf("expected %q in panic diagnostics, got:\n%s", "panic: boom", msg)
		}
	}
	for _, body := range []string{txt, html, xmlBody, jsonOutput} {
		if !strings.Contains(body, "goroutine") {
			t.Fatalf("expected a recovered stack trace (containing %q) in panic diagnostics, got:\n%s", "goroutine", body)
		}
	}
}

// runIntegrationSuite builds a real two-suite spec tree through the public specs DSL against t,
// attaches a parsed coverage profile, and flushes all four report formats into dir. It is called
// only from the subprocess re-exec of TestIntegrationAllFormatsAgree.
func runIntegrationSuite(t *testing.T, dir string) {
	mfr := report.NewMultiFormat(
		report.Target{Format: report.FormatXML, Path: filepath.Join(dir, "report.xml")},
		report.Target{Format: report.FormatHTML, Path: filepath.Join(dir, "report.html")},
		report.Target{Format: report.FormatTXT, Path: filepath.Join(dir, "report.txt")},
		report.Target{Format: report.FormatJSON, Path: filepath.Join(dir, "report.json")},
	)

	specs.DescribeWithReporter(t, "Checkout", mfr, func(s *specs.Spec) {
		s.It("charges the card", func(ctx *specs.Context) {
			ctx.Expect(1 + 1).ToEqual(2)
		})
		s.It("rejects an expired card", func(ctx *specs.Context) {
			ctx.Expect(1 + 1).ToEqual(3) // deliberately wrong: exercises StatusFailed
		})
		s.It("blows up", func(ctx *specs.Context) {
			panic("boom") // recovered by the runner: exercises StatusError
		})
	})
	specs.DescribeWithReporter(t, "Cart", mfr, func(s *specs.Spec) {
		s.It("adds an item", func(ctx *specs.Context) {
			ctx.Expect(true).ToEqual(true)
		})
	})

	cov, err := report.ParseCoverageProfile(strings.NewReader(integrationCoverageProfile))
	if err != nil {
		t.Fatalf("ParseCoverageProfile: %v", err)
	}
	mfr.SetCoverage(cov)

	if err := mfr.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
}

// The structs below decode the rendered reports the way an independent external consumer would:
// this package's own render_json.go/render_xml.go DTOs are unexported, so this is not a shortcut
// around that boundary but the same boundary a real CI consumer would cross.

type integrationJSONTotals struct {
	Total    int `json:"total"`
	Passed   int `json:"passed"`
	Failed   int `json:"failed"`
	Error    int `json:"error"`
	Skipped  int `json:"skipped"`
	Filtered int `json:"filtered"`
}

type integrationJSONCase struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Output  string `json:"output"`
}

type integrationJSONSuite struct {
	Name  string                `json:"name"`
	Cases []integrationJSONCase `json:"cases"`
}

type integrationJSONCoverageTotal struct {
	Covered int `json:"covered"`
	Total   int `json:"total"`
}

type integrationJSONReport struct {
	SchemaVersion string                 `json:"schemaVersion"`
	Execution     integrationJSONTotals  `json:"execution"`
	Suites        []integrationJSONSuite `json:"suites"`
	Coverage      struct {
		Total integrationJSONCoverageTotal `json:"total"`
	} `json:"coverage"`
}

type integrationXMLProperty struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

type integrationXMLTestCase struct {
	Name  string `xml:"name,attr"`
	Error *struct {
		Message string `xml:"message,attr"`
		Body    string `xml:",chardata"`
	} `xml:"error"`
}

type integrationXMLTestSuite struct {
	Name       string                   `xml:"name,attr"`
	Properties []integrationXMLProperty `xml:"properties>property"`
	TestCases  []integrationXMLTestCase `xml:"testcase"`
}

type integrationXMLTestSuites struct {
	XMLName  xml.Name                  `xml:"testsuites"`
	Tests    int                       `xml:"tests,attr"`
	Failures int                       `xml:"failures,attr"`
	Errors   int                       `xml:"errors,attr"`
	Skipped  int                       `xml:"skipped,attr"`
	Suites   []integrationXMLTestSuite `xml:"testsuite"`
}

func readIntegrationFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(b)
}

func readIntegrationJSON(t *testing.T, path string) integrationJSONReport {
	t.Helper()
	var doc integrationJSONReport
	if err := json.Unmarshal([]byte(readIntegrationFile(t, path)), &doc); err != nil {
		t.Fatalf("decoding %s: %v", path, err)
	}
	return doc
}

func readIntegrationXML(t *testing.T, path string) integrationXMLTestSuites {
	t.Helper()
	var doc integrationXMLTestSuites
	if err := xml.Unmarshal([]byte(readIntegrationFile(t, path)), &doc); err != nil {
		t.Fatalf("decoding %s: %v", path, err)
	}
	return doc
}

func xmlCoverageProperties(t *testing.T, suite integrationXMLTestSuite) (covered, total int) {
	t.Helper()
	props := map[string]string{}
	for _, p := range suite.Properties {
		props[p.Name] = p.Value
	}
	covered, err := strconv.Atoi(props["go-specs.coverage.aggregate.covered"])
	if err != nil {
		t.Fatalf("xml aggregate covered property: %v", err)
	}
	total, err = strconv.Atoi(props["go-specs.coverage.aggregate.total"])
	if err != nil {
		t.Fatalf("xml aggregate total property: %v", err)
	}
	return covered, total
}

func xmlErrorDiagnostics(t *testing.T, doc integrationXMLTestSuites) (message, body string) {
	t.Helper()
	for _, s := range doc.Suites {
		for _, c := range s.TestCases {
			if c.Error != nil {
				return c.Error.Message, c.Error.Body
			}
		}
	}
	t.Fatal("no <error> testcase found in xml report")
	return "", ""
}

func jsonErrorDiagnostics(t *testing.T, doc integrationJSONReport) (message, output string) {
	t.Helper()
	for _, s := range doc.Suites {
		for _, c := range s.Cases {
			if c.Status == "error" {
				return c.Message, c.Output
			}
		}
	}
	t.Fatal("no error-status case found in json report")
	return "", ""
}
