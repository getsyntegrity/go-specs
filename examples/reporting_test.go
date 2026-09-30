// reporting_test.go shows how to observe a run and turn it into machine- and human-readable
// reports with the report package.
//
// Use it when a CI system, a dashboard or a reviewer needs more than `go test` output: JUnit XML
// for CI test tabs, JSON for tooling, plain text for logs, HTML for people, plus coverage folded
// into the same document.
//
// The pieces:
//
//   - report.EventReporter is the hook the spec runner calls for every suite and spec. Pass one to
//     specs.DescribeWithReporter(t, name, reporter, fn) instead of specs.Describe.
//   - report.NewCollector() is an EventReporter that keeps the results; Collector.Report() returns
//     a report.NormalizedReport (suites, cases with a normalized Status, totals, coverage). Every
//     renderer consumes that one model.
//   - report.New(w) is an EventReporter that just prints one line per event to w, handy for
//     debugging a run.
//   - report.RenderJSON, RenderTXT, RenderXML (JUnit) and RenderHTML write a NormalizedReport.
//   - report.NewMultiFormat(targets...) collects and, on Flush, writes every requested
//     report.Target; report.ParseTarget parses the "format:path" spelling, for example
//     "xml:artifacts/report.xml".
//   - report.ParseCoverageProfile reads a `go test -coverprofile` file, and Collector.SetCoverage
//     attaches it.
//
// Durations and timestamps come from the run, so a live report is not byte-stable. The runnable
// Examples below therefore feed hand-built events with fixed durations to a Collector, which is
// also what a custom producer would do; the Test functions show the real thing with
// DescribeWithReporter and assert on stable fields.
//
// Related, but not shown here (they need files and a process): the cmd/go-specs-report CLI, which
// merges per-package shards into one report, and docs/REPORTING.md, which documents the JSON
// schema, multi-package reporting and the exit-code contract.
package examples_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/report"
	"github.com/getsyntegrity/go-specs/specs"
)

// reportingSampleCollector feeds a collector by hand with one suite of four specs: a pass, a
// failure, a skip and a pending one. Durations are fixed, so everything rendered from it is stable.
func reportingSampleCollector() *report.Collector {
	c := report.NewCollector()
	c.SuiteStarted(report.SuiteStartEvent{Name: "Checkout"})
	spec := func(name string) report.SpecResultEvent {
		return report.SpecResultEvent{SpecStartEvent: report.SpecStartEvent{Name: name, Path: []string{"Checkout", name}}}
	}
	pass := spec("charges the card")
	pass.Duration = 12 * time.Millisecond
	fail := spec("rejects a declined card")
	fail.Failed, fail.Message, fail.Duration = true, "expected status 402, got 200", 30*time.Millisecond
	skip := spec("talks to the real gateway")
	skip.Skipped = true
	pending := spec("supports wallets")
	pending.Pending = true
	for _, e := range []report.SpecResultEvent{pass, fail, skip, pending} {
		c.SpecFinished(e)
	}
	c.SuiteFinished(report.SuiteEndEvent{Name: "Checkout", Duration: 50 * time.Millisecond, TotalSpecs: 4, FailedSpecs: 1, SkippedSpecs: 1, PendingSpecs: 1})
	return c
}

// A Collector turns events into a NormalizedReport. Each case gets one Status from a closed
// vocabulary (passed, failed, error, skipped, filtered, pending, unstarted), and the totals are
// computed for you, so a consumer never re-derives them.
func Example_reportingCollectorNormalizesEvents() {
	rep := reportingSampleCollector().Report()

	fmt.Println("schema:", rep.SchemaVersion)
	fmt.Printf("total=%d passed=%d failed=%d skipped=%d pending=%d\n",
		rep.Execution.Total, rep.Execution.Passed, rep.Execution.Failed, rep.Execution.Skipped, rep.Execution.Pending)
	for _, cs := range rep.Suites[0].Cases {
		fmt.Printf("%-9s %s\n", cs.Status, cs.Name)
	}
	// Output:
	// schema: 3
	// total=4 passed=1 failed=1 skipped=1 pending=1
	// passed    charges the card
	// failed    rejects a declined card
	// skipped   talks to the real gateway
	// pending   supports wallets
}

// RenderTXT writes deterministic plain text without colour codes: totals, every failed or errored
// case with its message, and a coverage table when there is one. It suits terminals and archived
// CI logs.
func Example_reportingRenderTXT() {
	c := reportingSampleCollector()
	cov, err := report.ParseCoverageProfile(strings.NewReader(reportingCoverageProfile))
	if err != nil {
		panic(err)
	}
	c.SetCoverage(cov)

	var buf bytes.Buffer
	if err := report.RenderTXT(&buf, c.Report()); err != nil {
		panic(err)
	}
	fmt.Print(buf.String())
	// Output:
	// go-specs report
	// Total: 4  Passed: 1  Failed: 1  Error: 0  Skipped: 1  Filtered: 0  Pending: 1  Unstarted: 0
	// Duration: 0.050s
	//
	// Suite: Checkout
	//   FAIL  Checkout/rejects a declined card (0.030s)
	//         message: expected status 402, got 200
	//   Totals: total=4 passed=1 failed=1 error=0 skipped=1 filtered=0 pending=1 unstarted=0
	//
	// Coverage
	// Package                                   Covered  Total  Percent
	// github.com/getsyntegrity/go-specs/report  4        4      100.00%
	// github.com/getsyntegrity/go-specs/specs   3        5      60.00%
	// Aggregate                                 7        9      77.78%
}

// RenderJSON writes the versioned JSON document that tooling should consume. Arrays are always
// arrays (never null), durations are whole milliseconds, and the schema version travels with the
// data. Here the document is decoded again to print just the execution totals.
func Example_reportingRenderJSON() {
	var buf bytes.Buffer
	if err := report.RenderJSON(&buf, reportingSampleCollector().Report()); err != nil {
		panic(err)
	}

	out := buf.String()
	fmt.Println(strings.Contains(out, `"schemaVersion": "3"`))
	fmt.Println(strings.Contains(out, `"status": "failed"`))
	fmt.Println(strings.Contains(out, `"message": "expected status 402, got 200"`))
	// Output:
	// true
	// true
	// true
}

// RenderXML writes JUnit-compatible XML, which most CI systems show as a test tab. Failed cases
// become <failure>, errors <error>; skipped, filtered, pending and unstarted specs all become
// <skipped> with a message that tells them apart.
func Example_reportingRenderXML() {
	var buf bytes.Buffer
	if err := report.RenderXML(&buf, reportingSampleCollector().Report()); err != nil {
		panic(err)
	}

	for _, line := range strings.Split(buf.String(), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "<testsuites") || strings.HasPrefix(trimmed, "<failure") || strings.HasPrefix(trimmed, "<skipped") {
			fmt.Println(trimmed)
		}
	}
	// Output:
	// <testsuites tests="4" failures="1" errors="0" skipped="2" time="0.050">
	// <failure message="expected status 402, got 200"></failure>
	// <skipped></skipped>
	// <skipped message="pending"></skipped>
}

// RenderHTML writes one self-contained page (inline CSS, no external assets), so it can be
// attached to a CI run and opened offline.
func Example_reportingRenderHTML() {
	var buf bytes.Buffer
	if err := report.RenderHTML(&buf, reportingSampleCollector().Report()); err != nil {
		panic(err)
	}

	out := buf.String()
	fmt.Println(strings.HasPrefix(out, "<!DOCTYPE html>"))
	fmt.Println(strings.Contains(out, "rejects a declined card"))
	fmt.Println(strings.Contains(out, "http://") || strings.Contains(out, "https://"))
	// Output:
	// true
	// true
	// false
}

// reportingCoverageProfile is a profile in exactly the format `go test -coverprofile=out` writes:
// file:startLine.col,endLine.col numStatements count.
const reportingCoverageProfile = `mode: set
github.com/getsyntegrity/go-specs/specs/spec.go:10.1,12.2 3 1
github.com/getsyntegrity/go-specs/specs/spec.go:14.1,16.2 2 0
github.com/getsyntegrity/go-specs/report/model.go:5.1,7.2 4 1
`

// ParseCoverageProfile groups statements by package. A statement counts as covered when its block
// ran at least once, whatever the profile mode. The aggregate sums statements across packages, it
// does not average package percentages.
func Example_reportingParseCoverageProfile() {
	cov, err := report.ParseCoverageProfile(strings.NewReader(reportingCoverageProfile))
	if err != nil {
		panic(err)
	}

	for _, p := range cov.Packages { // sorted by import path
		fmt.Printf("%s %d/%d %.1f%%\n", p.ImportPath, p.Covered, p.Total, p.Percentage())
	}
	fmt.Printf("aggregate %d/%d %.1f%%\n", cov.Total.Covered, cov.Total.Total, cov.Total.Percentage())
	// Output:
	// github.com/getsyntegrity/go-specs/report 4/4 100.0%
	// github.com/getsyntegrity/go-specs/specs 3/5 60.0%
	// aggregate 7/9 77.8%
}

// A malformed profile is an error that names the line, not a silent zero.
func Example_reportingParseCoverageProfileError() {
	_, err := report.ParseCoverageProfile(strings.NewReader("this is not a profile\n"))
	fmt.Println(err)
	// Output: coverage profile: line 1: expected mode line, got "this is not a profile"
}

// ParseTarget parses the "format:path" spelling used for report outputs. It only parses; nothing
// is written until MultiFormatReporter.Flush.
func Example_reportingParseTarget() {
	target, err := report.ParseTarget("xml:artifacts/report.xml")
	fmt.Println(target.Format, target.Path, err)

	_, err = report.ParseTarget("pdf:report.pdf")
	fmt.Println(err)

	_, err = report.ParseTarget("json")
	fmt.Println(err)
	// Output:
	// xml artifacts/report.xml <nil>
	// go-specs report target "pdf:report.pdf": unknown format "pdf" (want xml, html, txt, or json)
	// go-specs report target "json": expected format:path
}

// report.New(w) prints one line per suite event. It is a debugging aid, not a report format.
func Example_reportingEventLog() {
	var buf bytes.Buffer
	r := report.New(&buf)
	r.SuiteStarted(report.SuiteStartEvent{Name: "Checkout"})
	r.SuiteFinished(report.SuiteEndEvent{Name: "Checkout", Duration: 50 * time.Millisecond, SkippedSpecs: 1})

	fmt.Print(buf.String())
	// Output:
	// SuiteStarted Checkout
	// SuiteFinished Checkout duration=50ms skipped=1 pending=0
}

// --- The real thing: DescribeWithReporter -----------------------------------------------------

// DescribeWithReporter runs a normal suite and feeds every event to the reporter. The Collector
// then holds the results, whose names, paths and statuses are stable even though durations are not.
func TestReporting_collectorWithDescribeWithReporter(t *testing.T) {
	collector := report.NewCollector()

	specs.DescribeWithReporter(t, "Cart", collector, func(s *specs.Spec) {
		s.It("adds an item", func(ctx *specs.Context) {
			ctx.Expect(1 + 1).ToEqual(2)
		})
		s.When("the cart is empty", func(s *specs.Spec) {
			s.It("has a zero total", func(ctx *specs.Context) {
				ctx.Expect(0).ToEqual(0)
			})
			s.SkipIt("applies a coupon", func(ctx *specs.Context) {})
		})
	})

	rep := collector.Report()
	if len(rep.Suites) != 1 || rep.Suites[0].Name != "Cart" {
		t.Fatalf("suites = %+v", rep.Suites)
	}
	got := map[string]report.Status{}
	for _, cs := range rep.Suites[0].Cases {
		got[strings.Join(cs.Path, "/")] = cs.Status
	}
	want := map[string]report.Status{
		"Cart/adds an item":                       report.StatusPassed,
		"Cart/the cart is empty/has a zero total": report.StatusPassed,
		"Cart/the cart is empty/applies a coupon": report.StatusSkipped,
	}
	for path, status := range want {
		if got[path] != status {
			t.Errorf("%s = %q, want %q (all: %v)", path, got[path], status, got)
		}
	}
	if rep.Execution.Total != 3 || rep.Execution.Passed != 2 || rep.Execution.Skipped != 1 {
		t.Errorf("totals = %+v", rep.Execution)
	}
}

// Renderers accept any NormalizedReport, including one from a real run. Only the stable parts of
// the output are asserted here, because it contains measured durations.
func TestReporting_renderARealRun(t *testing.T) {
	collector := report.NewCollector()
	specs.DescribeWithReporter(t, "Billing", collector, func(s *specs.Spec) {
		s.It("totals an invoice", func(ctx *specs.Context) {
			ctx.Expect(10 + 5).ToEqual(15)
		})
	})

	var txt bytes.Buffer
	if err := report.RenderTXT(&txt, collector.Report()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"go-specs report", "Suite: Billing", "Total: 1  Passed: 1"} {
		if !strings.Contains(txt.String(), want) {
			t.Errorf("TXT report is missing %q:\n%s", want, txt.String())
		}
	}
}

// MultiFormatReporter collects like a Collector and writes every target on Flush, creating parent
// directories. With no targets it collects but writes nothing, so reporting stays opt-in. In a
// real project Flush is called once from TestMain after m.Run(); here it writes into a temporary
// directory.
func TestReporting_multiFormatWritesTargets(t *testing.T) {
	dir := t.TempDir()
	xmlTarget, err := report.ParseTarget("xml:" + filepath.Join(dir, "out", "report.xml"))
	if err != nil {
		t.Fatal(err)
	}
	jsonTarget, err := report.ParseTarget("json:" + filepath.Join(dir, "out", "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	multi := report.NewMultiFormat(xmlTarget, jsonTarget)

	specs.DescribeWithReporter(t, "Shipping", multi, func(s *specs.Spec) {
		s.It("quotes a parcel", func(ctx *specs.Context) {
			ctx.Expect(3).ToEqual(3)
		})
	})
	if err := multi.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	for _, name := range []string{"report.xml", "report.json"} {
		data, err := os.ReadFile(filepath.Join(dir, "out", name))
		if err != nil {
			t.Fatalf("%s was not written: %v", name, err)
		}
		if !strings.Contains(string(data), "Shipping") {
			t.Errorf("%s does not mention the suite:\n%s", name, data)
		}
	}
}
