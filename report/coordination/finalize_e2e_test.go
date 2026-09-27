package coordination

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getsyntegrity/go-specs/report"
)

// finalize_e2e_test.go is the end-to-end slice of issue #146 (spec 2 of 3): it drives real `go
// test` subprocesses over report/coordination/internal/e2efixture — a sharded, multi-package
// module fixture whose two producer packages both import a shared dependency under `-coverpkg`,
// and whose first producer registers a failing BeforeAll — then calls the real coordination.
// Finalize on the shards and combined coverage profile those subprocesses actually produced, and
// asserts the merged module report. Unlike finalize_test.go and finalize_shards_test.go (which
// prove Finalize's own logic against fabricated shards and a synthetic coverage profile), this is
// the one test in the package proving the full producer-to-finalizer lifecycle (contract v1.2.8
// §3-§9) against the real toolchain: real cache/environment behaviour (already pinned in
// integration_test.go), a real `-coverpkg` overlap profile, and a real BeforeAll/AfterAll hook
// case (#207/#228) surviving the merge.

const (
	fixtureProducerA = "github.com/getsyntegrity/go-specs/report/coordination/internal/e2efixture/producera"
	fixtureProducerB = "github.com/getsyntegrity/go-specs/report/coordination/internal/e2efixture/producerb"
	fixtureShared    = "github.com/getsyntegrity/go-specs/report/coordination/internal/e2efixture/shared"

	fixtureProducerAPath = "./report/coordination/internal/e2efixture/producera/..."
	fixtureProducerBPath = "./report/coordination/internal/e2efixture/producerb/..."
	fixtureSharedPath    = "./report/coordination/internal/e2efixture/shared/..."
)

// packageCoverageTotal returns the Total statement count report.ParseCoverageProfile(Merged)
// recorded for importPath, or -1 if that package never appears in pkgs at all — distinguishing "no
// entry" from "an entry with zero statements", which a plain 0 would not.
func packageCoverageTotal(pkgs []report.PackageCoverage, importPath string) int {
	for _, p := range pkgs {
		if p.ImportPath == importPath {
			return p.Total
		}
	}
	return -1
}

// findHookCase returns the first case anywhere in rep whose Hook marker is set.
func findHookCase(rep report.NormalizedReport) (report.Case, bool) {
	for _, s := range rep.Suites {
		for _, c := range s.Cases {
			if c.Hook != "" {
				return c, true
			}
		}
	}
	return report.Case{}, false
}

func TestFinalizeMergesRealShardsWithDeduplicatedCoverageAndAHookCase(t *testing.T) {
	base := secureTempDir(t)
	mustInitRun(t, base, "run-1", validToken)

	coverProfile := filepath.Join(t.TempDir(), "cover.out")
	coverPkg := strings.Join([]string{fixtureProducerAPath, fixtureProducerBPath, fixtureSharedPath}, ",")

	env := runEnv(base, "run-1", validToken)
	env["GO_SPECS_FIXTURE_HOOK_FAIL"] = "1"

	res := runGoTest(t, env, "-count=1",
		"-coverpkg="+coverPkg, "-coverprofile="+coverProfile,
		fixtureProducerAPath, fixtureProducerBPath)
	// producera's BeforeAll deliberately fails (GO_SPECS_FIXTURE_HOOK_FAIL=1), so the package, and
	// therefore this combined `go test` invocation, is red. Contract v1.2.8 §3 step 7 is exactly
	// this case: finalize must still run and still see a complete shard from a failing package.
	if res.exitCode == 0 {
		t.Fatalf("expected producera's deliberately failing BeforeAll to make this go test invocation exit non-zero:\n%s", res.out)
	}

	names := shardNames(t, base, "run-1")
	if len(names) != 2 {
		t.Fatalf("got %d shards, want one per producer (a failing package still publishes a complete shard): %v", len(names), names)
	}

	if _, err := os.Stat(coverProfile); err != nil {
		t.Fatalf("the combined coverage profile was not written: %v", err)
	}

	// Prove the real profile actually contains -coverpkg overlap for the shared package: both
	// producera's and producerb's test binaries import shared, so each contributes its own raw
	// entry for every one of its blocks (contract v1.2.8 §9, finding F7), regardless of which
	// branch each binary actually exercised.
	rawFile, err := os.Open(coverProfile)
	if err != nil {
		t.Fatal(err)
	}
	rawCov, err := report.ParseCoverageProfile(rawFile)
	closeErr := rawFile.Close()
	if err != nil {
		t.Fatalf("report.ParseCoverageProfile: %v", err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	sharedRawTotal := packageCoverageTotal(rawCov.Packages, fixtureShared)
	if sharedRawTotal <= 0 {
		t.Fatalf("the raw profile has no statements for %s; the fixture does not exercise real -coverpkg overlap (raw packages: %+v)", fixtureShared, rawCov.Packages)
	}

	out := t.TempDir()
	targets := []report.Target{
		{Format: report.FormatJSON, Path: filepath.Join(out, "report.json")},
		{Format: report.FormatXML, Path: filepath.Join(out, "report.xml")},
		{Format: report.FormatTXT, Path: filepath.Join(out, "report.txt")},
		{Format: report.FormatHTML, Path: filepath.Join(out, "report.html")},
	}

	result, err := Finalize(context.Background(), FinalizeOptions{
		RunID: "run-1", Token: validToken, BaseDir: base,
		ExpectedProducers: []string{fixtureProducerA, fixtureProducerB},
		CoverProfile:      coverProfile,
		Targets:           targets,
	})
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if len(result.PackagesMissing) != 0 || len(result.Rejected) != 0 {
		t.Fatalf("Missing=%v Rejected=%v, want both empty: every expected producer published a valid shard", result.PackagesMissing, result.Rejected)
	}
	if len(result.PackagesFound) != 2 {
		t.Fatalf("PackagesFound = %v, want both producers", result.PackagesFound)
	}
	// Finalize itself never errors on a failing test result (contract v1.2.8 §8): the caller's
	// own exit status must come from ExitCode, not from a red `go test`.
	if ec := ExitCode(result, err); ec != 0 {
		t.Fatalf("ExitCode = %d, want 0: a failing test result must not become a reporting failure", ec)
	}

	// producera: 1 failed [BeforeAll] hook case + 1 spec skipped by it = 2.
	// producerb: 2 passing specs.
	const wantTotal, wantFailed, wantSkipped, wantPassed = 4, 1, 1, 2
	ex := result.Merged.Execution
	if ex.Total != wantTotal || ex.Failed != wantFailed || ex.Skipped != wantSkipped || ex.Passed != wantPassed {
		t.Fatalf("Merged.Execution = %+v, want Total=%d Failed=%d Skipped=%d Passed=%d",
			ex, wantTotal, wantFailed, wantSkipped, wantPassed)
	}

	hookCase, ok := findHookCase(result.Merged)
	if !ok {
		t.Fatal("the merged report carries no hook case; producera's failed BeforeAll (#207/#228) was lost in the merge")
	}
	if hookCase.Hook != "BeforeAll" {
		t.Fatalf("hook case Hook = %q, want %q", hookCase.Hook, "BeforeAll")
	}
	if hookCase.Status != report.StatusFailed {
		t.Fatalf("hook case Status = %q, want %q", hookCase.Status, report.StatusFailed)
	}

	dedupedSharedTotal := packageCoverageTotal(result.Merged.Coverage.Packages, fixtureShared)
	if dedupedSharedTotal <= 0 {
		t.Fatalf("Merged.Coverage has no entry for %s (packages: %+v)", fixtureShared, result.Merged.Coverage.Packages)
	}
	if dedupedSharedTotal >= sharedRawTotal {
		t.Fatalf("deduplicated shared Total (%d) is not less than the raw, non-deduplicated Total (%d); "+
			"the real -coverpkg overlap was not collapsed by Finalize's merge (contract v1.2.8 §9, finding F7)",
			dedupedSharedTotal, sharedRawTotal)
	}

	assertRenderersAgreeOnTheMerge(t, targets, result.Merged)
}

// renderedJSONReport is a minimal decode target for report.RenderJSON's documented shape (see
// render_json.go's jsonReport, which is unexported): only the fields this test needs to cross-
// check against the other three renderers.
type renderedJSONReport struct {
	Execution struct {
		Total, Passed, Failed, Skipped int
	} `json:"execution"`
	Suites []struct {
		Cases []struct {
			Hook string `json:"hook"`
		} `json:"cases"`
	} `json:"suites"`
}

// renderedJUnitXML mirrors render_xml.go's junitTestSuites/junitTestCase shape closely enough to
// cross-check totals and the hook attribute.
type renderedJUnitXML struct {
	XMLName  xml.Name `xml:"testsuites"`
	Tests    int      `xml:"tests,attr"`
	Failures int      `xml:"failures,attr"`
	Skipped  int      `xml:"skipped,attr"`
	Suites   []struct {
		TestCases []struct {
			Hook string `xml:"hook,attr"`
		} `xml:"testcase"`
	} `xml:"testsuite"`
}

// assertRenderersAgreeOnTheMerge proves the four rendered targets all originate from the same
// merged NormalizedReport (issue #146 acceptance criterion "XML, HTML, TXT, and JSON originate
// from one merged normalized report"): the same execution totals, and the same hook case, appear
// in every one of them.
func assertRenderersAgreeOnTheMerge(t *testing.T, targets []report.Target, merged report.NormalizedReport) {
	t.Helper()

	pathFor := func(f report.Format) string {
		for _, tg := range targets {
			if tg.Format == f {
				return tg.Path
			}
		}
		t.Fatalf("no target for format %s", f)
		return ""
	}

	jsonRaw, err := os.ReadFile(pathFor(report.FormatJSON))
	if err != nil {
		t.Fatal(err)
	}
	var jsonDoc renderedJSONReport
	if err := json.Unmarshal(jsonRaw, &jsonDoc); err != nil {
		t.Fatalf("rendered JSON does not decode: %v\n%s", err, jsonRaw)
	}
	if jsonDoc.Execution.Total != merged.Execution.Total ||
		jsonDoc.Execution.Passed != merged.Execution.Passed ||
		jsonDoc.Execution.Failed != merged.Execution.Failed ||
		jsonDoc.Execution.Skipped != merged.Execution.Skipped {
		t.Fatalf("rendered JSON execution = %+v, want it to match Merged.Execution = %+v", jsonDoc.Execution, merged.Execution)
	}
	if !jsonHasHook(jsonDoc, "BeforeAll") {
		t.Fatalf("rendered JSON carries no case with hook %q:\n%s", "BeforeAll", jsonRaw)
	}

	xmlRaw, err := os.ReadFile(pathFor(report.FormatXML))
	if err != nil {
		t.Fatal(err)
	}
	var xmlDoc renderedJUnitXML
	if err := xml.Unmarshal(xmlRaw, &xmlDoc); err != nil {
		t.Fatalf("rendered XML does not decode: %v\n%s", err, xmlRaw)
	}
	wantSkippedAttr := merged.Execution.Skipped + merged.Execution.Filtered + merged.Execution.Pending
	if xmlDoc.Tests != merged.Execution.Total || xmlDoc.Failures != merged.Execution.Failed || xmlDoc.Skipped != wantSkippedAttr {
		t.Fatalf("rendered XML testsuites tests=%d failures=%d skipped=%d, want tests=%d failures=%d skipped=%d",
			xmlDoc.Tests, xmlDoc.Failures, xmlDoc.Skipped, merged.Execution.Total, merged.Execution.Failed, wantSkippedAttr)
	}
	if !xmlHasHook(xmlDoc, "BeforeAll") {
		t.Fatalf("rendered XML carries no testcase with hook=%q:\n%s", "BeforeAll", xmlRaw)
	}

	txtRaw, err := os.ReadFile(pathFor(report.FormatTXT))
	if err != nil {
		t.Fatal(err)
	}
	wantTXTTotals := "Total: 4  Passed: 2  Failed: 1  Error: 0  Skipped: 1  Filtered: 0  Pending: 0"
	if !strings.Contains(string(txtRaw), wantTXTTotals) {
		t.Fatalf("rendered TXT does not contain %q:\n%s", wantTXTTotals, txtRaw)
	}
	if !strings.Contains(string(txtRaw), "[BeforeAll]") {
		t.Fatalf("rendered TXT does not name the [BeforeAll] hook case:\n%s", txtRaw)
	}

	htmlRaw, err := os.ReadFile(pathFor(report.FormatHTML))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Total: <strong>4</strong>",
		"Passed: <strong>2</strong>",
		"Failed: <strong>1</strong>",
		"Skipped: <strong>1</strong>",
		"[BeforeAll]",
	} {
		if !strings.Contains(string(htmlRaw), want) {
			t.Fatalf("rendered HTML does not contain %q:\n%s", want, htmlRaw)
		}
	}
}

func jsonHasHook(doc renderedJSONReport, hook string) bool {
	for _, s := range doc.Suites {
		for _, c := range s.Cases {
			if c.Hook == hook {
				return true
			}
		}
	}
	return false
}

func xmlHasHook(doc renderedJUnitXML, hook string) bool {
	for _, s := range doc.Suites {
		for _, c := range s.TestCases {
			if c.Hook == hook {
				return true
			}
		}
	}
	return false
}
