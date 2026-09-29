// reporting_contract_test.go is the v0.3.0 end-to-end reporting contract (issue #325). Earlier
// changes each pinned one observable semantic with a unit test on one engine: assertion messages
// (#272), Builder parallel panic status (#314), declaration-order results (#315), package identity
// (#308) and schema v3's unstarted cases (#274). None of those proves what a consumer actually
// reads: the same suite run through each execution mode, then rendered as JSON, JUnit XML, TXT and
// HTML. This file runs one table of scenarios through four modes — default Describe, Builder/Runner,
// Spec.ItParallel and Builder.ItParallel — and checks the normalized SpecResultEvents and every
// renderer against one expectation per scenario.
//
// Every scenario runs in a child process. Assertion, ctx.T and panic scenarios fail a real
// *testing.T subtest, which would fail this test itself if run in-process, and the -run selector of
// the filtered scenario is a property of the process. The child prints one CONTRACT_RESULT line
// (the events plus the four rendered documents); the parent decodes it and asserts. Volatile fields
// (durations, timestamps, stack line numbers) are never compared: the JSON and JUnit documents are
// decoded and checked field by field, and TXT/HTML are checked for content and order only.
//
// Completion order in the ordering scenario is forced with channels, never sleeps, so the result is
// deterministic and race-clean.
package specs

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/report"
)

const (
	contractHelperEnv = "GO_SPECS_REPORTING_CONTRACT_HELPER"
	contractResultTag = "CONTRACT_RESULT "
	// contractPackage stands in for the import path the module-wide merge stamps on every suite
	// (#308). A single-package run leaves Suite.Package empty, so the helper sets it the way the
	// merge does, before rendering.
	contractPackage = "example.com/m/contract"
)

type contractSpec struct {
	name string
	body func(*Context)
}

type contractScenario struct {
	name     string
	engines  []string
	specs    func() []contractSpec
	failFast bool
	// selector is an optional -test.run tail appended after the suite's subtest name, so the
	// filtered scenario makes go test itself exclude specs.
	selector string
	// knownGaps maps an engine to the production defect that makes this scenario fail on it. The
	// case is skipped, not fixed: this slice adds contract tests only (#325).
	knownGaps map[string]string
	want      []contractCase
}

// contractCase is one expected case, in declaration order.
type contractCase struct {
	name    string
	status  string // report.Status vocabulary
	message string
	stack   bool // Output must carry a recovered panic's stack trace
}

// contractEngines declares the four execution modes. Each declares the scenario's specs under a
// "suite" root and runs them against reporter through the real *testing.T.
var contractEngines = map[string]func(t *testing.T, rep report.EventReporter, sc contractScenario){
	"describe": func(t *testing.T, rep report.EventReporter, sc contractScenario) {
		declare := func(s *Spec) {
			for _, sp := range sc.specs() {
				s.It(sp.name, sp.body)
			}
		}
		if !sc.failFast {
			DescribeWithReporter(t, "suite", rep, declare)
			return
		}
		suite := BuildSuite(t, "suite", declare)
		suite.Reporter = rep
		suite.SetFailFast(true)
		suite.Run(t)
	},
	"builder": func(t *testing.T, rep report.EventReporter, sc contractScenario) {
		b := NewBuilder()
		b.Describe("suite", func() {
			for _, sp := range sc.specs() {
				b.It(sp.name, sp.body)
			}
		})
		r := NewRunnerWithReporter(b.Build(), "suite", rep)
		r.FailFast = sc.failFast
		r.Run(t)
	},
	"spec-itparallel": func(t *testing.T, rep report.EventReporter, sc contractScenario) {
		DescribeWithReporter(t, "suite", rep, func(s *Spec) {
			for _, sp := range sc.specs() {
				s.ItParallel(sp.name, sp.body)
			}
		})
	},
	"builder-itparallel": func(t *testing.T, rep report.EventReporter, sc contractScenario) {
		b := NewBuilder()
		b.Describe("suite", func() {
			for _, sp := range sc.specs() {
				b.ItParallel(sp.name, sp.body)
			}
		})
		NewRunnerWithReporter(b.Build(), "suite", rep).Run(t)
	},
}

// contractWaitFor blocks until c closes. The timeout only turns a scheduling bug (bodies not
// running concurrently) into a diagnosable panic instead of a hung test; it is never what releases
// a body.
func contractWaitFor(c <-chan struct{}) {
	select {
	case <-c:
	case <-time.After(10 * time.Second):
		panic("contract bodies are not running concurrently")
	}
}

// contractOrderSpecs makes completion order the exact reverse of declaration order: "first" waits
// for "second", which waits for "third". "first" is the earliest declared and finishes last;
// "second" fails so the failure also sits in the middle of the stream.
func contractOrderSpecs() []contractSpec {
	secondDone, thirdDone := make(chan struct{}), make(chan struct{})
	return []contractSpec{
		{"first", func(*Context) { contractWaitFor(secondDone) }},
		{"second", func(ctx *Context) {
			defer close(secondDone)
			contractWaitFor(thirdDone)
			ctx.Expect(42).To(Equal(43))
		}},
		{"third", func(*Context) { close(thirdDone) }},
	}
}

func contractMixedSpecs(withDirectT bool) func() []contractSpec {
	return func() []contractSpec {
		specs := []contractSpec{
			{"passes", func(*Context) {}},
			{"assertion", func(ctx *Context) { ctx.Expect(42).To(Equal(43)) }},
		}
		if withDirectT {
			specs = append(specs, contractSpec{"direct", func(ctx *Context) { ctx.T.Error("direct boom") }})
		}
		return append(specs, contractSpec{"panics", func(*Context) { panic("kaboom") }})
	}
}

func contractPassSpecs(names ...string) func() []contractSpec {
	return func() []contractSpec {
		var specs []contractSpec
		for _, n := range names {
			specs = append(specs, contractSpec{n, func(*Context) {}})
		}
		return specs
	}
}

var (
	allContractEngines      = []string{"describe", "builder", "spec-itparallel", "builder-itparallel"}
	parallelContractEngines = []string{"spec-itparallel", "builder-itparallel"}
)

func contractScenarios() []contractScenario {
	mixedWant := []contractCase{
		{name: "passes", status: "passed"},
		{name: "assertion", status: "failed", message: wantEqualFailureMessage},
		{name: "panics", status: "error", message: "panic: kaboom", stack: true},
	}
	withDirect := func(want []contractCase) []contractCase {
		// ctx.T.Error records no structured text (events.go: Message stays empty for it).
		return []contractCase{want[0], want[1], {name: "direct", status: "failed"}, want[2]}
	}
	return []contractScenario{
		{
			name:    "mixed-with-ctx-t",
			engines: []string{"describe", "builder", "spec-itparallel"},
			specs:   contractMixedSpecs(true),
			want:    withDirect(mixedWant),
		},
		{
			// Builder.ItParallel bodies run with ctx.T == nil, so no direct-ctx.T spec is declared.
			name:    "mixed-without-ctx-t",
			engines: []string{"builder-itparallel"},
			specs:   contractMixedSpecs(false),
			want:    mixedWant,
		},
		{
			name:    "inverted-completion-order",
			engines: parallelContractEngines,
			specs:   contractOrderSpecs,
			want: []contractCase{
				{name: "first", status: "passed"},
				{name: "second", status: "failed", message: wantEqualFailureMessage},
				{name: "third", status: "passed"},
			},
		},
		{
			name:    "fail-fast",
			engines: []string{"describe", "builder"},
			specs: func() []contractSpec {
				return []contractSpec{
					{"fails", func(ctx *Context) { ctx.Expect(42).To(Equal(43)) }},
					{"never-starts", func(*Context) {}},
					{"also-never-starts", func(*Context) {}},
				}
			},
			failFast: true,
			want: []contractCase{
				{name: "fails", status: "failed", message: wantEqualFailureMessage},
				{name: "never-starts", status: "unstarted"},
				{name: "also-never-starts", status: "unstarted"},
			},
		},
		{
			name:     "filtered",
			engines:  allContractEngines,
			specs:    contractPassSpecs("one", "two", "three"),
			selector: "^two$",
			knownGaps: map[string]string{
				// Builder.ItParallel runs a batch under one generated subtest ("#00"), so `-run
				// ^suite$/^two$` matches none of it: the batch neither runs nor reports, and the
				// suite ends with zero cases. Spec.ItParallel reports the same batch as Filtered
				// (#111, #273). Follow-up: report a Builder.ItParallel batch excluded by -run as
				// Filtered, one event per spec.
				"builder-itparallel": "Builder.ItParallel specs vanish from the report under a non-matching -run selector instead of being reported Filtered",
			},
			want: []contractCase{
				{name: "one", status: "filtered"},
				{name: "two", status: "passed"},
				{name: "three", status: "filtered"},
			},
		},
	}
}

// contractTee fans every event out to several reporters.
type contractTee []report.EventReporter

func (t contractTee) SuiteStarted(e report.SuiteStartEvent) {
	for _, r := range t {
		r.SuiteStarted(e)
	}
}
func (t contractTee) SuiteFinished(e report.SuiteEndEvent) {
	for _, r := range t {
		r.SuiteFinished(e)
	}
}
func (t contractTee) SpecStarted(e report.SpecStartEvent) {
	for _, r := range t {
		r.SpecStarted(e)
	}
}
func (t contractTee) SpecFinished(e report.SpecResultEvent) {
	for _, r := range t {
		r.SpecFinished(e)
	}
}

// contractEvent is the normalized, volatile-free view of one SpecResultEvent.
type contractEvent struct {
	Path      []string `json:"path"`
	Status    string   `json:"status"`
	Message   string   `json:"message"`
	HasOutput bool     `json:"hasOutput"`
	HasStack  bool     `json:"hasStack"`
}

type contractResult struct {
	Events []contractEvent `json:"events"`
	Suite  struct {
		Total, Failed, Skipped, Filtered, Unstarted int
	} `json:"suite"`
	JSON  string `json:"json"`
	JUnit string `json:"junit"`
	TXT   string `json:"txt"`
	HTML  string `json:"html"`
}

// contractStatus is classifyOutcome plus the unstarted status it predates.
func contractStatus(e report.SpecResultEvent) string {
	if e.Unstarted {
		return "unstarted"
	}
	return classifyOutcome(e)
}

// contractRunChild is the helper-process half: run the scenario, then print one CONTRACT_RESULT.
func contractRunChild(t *testing.T, engine, scenario string) {
	var sc contractScenario
	for _, s := range contractScenarios() {
		if s.name == scenario {
			sc = s
		}
	}
	rec := &recordingReporter{}
	collector := report.NewCollector()
	contractEngines[engine](t, contractTee{rec, collector}, sc)

	var res contractResult
	for _, e := range rec.specFinished {
		res.Events = append(res.Events, contractEvent{
			Path:      e.Path,
			Status:    contractStatus(e),
			Message:   e.Message,
			HasOutput: e.Output != "",
			HasStack:  strings.Contains(e.Output, "goroutine "),
		})
	}
	end := rec.suiteFinished[0]
	res.Suite.Total, res.Suite.Failed, res.Suite.Skipped = end.TotalSpecs, end.FailedSpecs, end.SkippedSpecs
	res.Suite.Filtered, res.Suite.Unstarted = end.FilteredSpecs, end.UnstartedSpecs

	rendered := collector.Report()
	for i := range rendered.Suites {
		rendered.Suites[i].Package = contractPackage
	}
	for dst, render := range map[*string]func(*bytes.Buffer) error{
		&res.JSON:  func(b *bytes.Buffer) error { return report.RenderJSON(b, rendered) },
		&res.JUnit: func(b *bytes.Buffer) error { return report.RenderXML(b, rendered) },
		&res.TXT:   func(b *bytes.Buffer) error { return report.RenderTXT(b, rendered) },
		&res.HTML:  func(b *bytes.Buffer) error { return report.RenderHTML(b, rendered) },
	} {
		var buf bytes.Buffer
		if err := render(&buf); err != nil {
			t.Fatalf("render: %v", err)
		}
		*dst = buf.String()
	}
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	fmt.Printf("\n%s%s\n", contractResultTag, raw)
}

// TestReportingContract runs every scenario on every mode it applies to and checks what a report
// consumer reads. It is also the helper entry point: with contractHelperEnv set it runs one
// scenario instead (see contractRunChild).
func TestReportingContract(t *testing.T) {
	if spec := os.Getenv(contractHelperEnv); spec != "" {
		engine, scenario, _ := strings.Cut(spec, "/")
		contractRunChild(t, engine, scenario)
		return
	}
	for _, sc := range contractScenarios() {
		for _, engine := range sc.engines {
			t.Run(sc.name+"/"+engine, func(t *testing.T) {
				if gap := sc.knownGaps[engine]; gap != "" {
					t.Skip("known reporting gap: " + gap)
				}
				pattern := "^TestReportingContract$"
				if sc.selector != "" {
					pattern += "/^suite$/" + sc.selector
				}
				cmd := exec.Command(os.Args[0], "-test.run="+pattern)
				cmd.Env = append(os.Environ(), contractHelperEnv+"="+engine+"/"+sc.name)
				output, _ := cmd.CombinedOutput() // failing scenarios exit non-zero by design
				res := contractDecode(t, output)
				assertContract(t, sc, res)
			})
		}
	}
}

func contractDecode(t *testing.T, output []byte) contractResult {
	t.Helper()
	for _, line := range strings.Split(string(output), "\n") {
		if raw, ok := strings.CutPrefix(line, contractResultTag); ok {
			var res contractResult
			if err := json.Unmarshal([]byte(raw), &res); err != nil {
				t.Fatalf("decode result: %v", err)
			}
			return res
		}
	}
	t.Fatalf("child printed no %q line:\n%s", contractResultTag, output)
	return contractResult{}
}

// Decoding shapes for the JSON and JUnit documents. They mirror only the fields the contract reads.
type contractJSONDoc struct {
	SchemaVersion string `json:"schemaVersion"`
	Execution     contractTotals
	Suites        []struct {
		Name    string `json:"name"`
		Package string `json:"package"`
		Totals  contractTotals
		Cases   []struct {
			Name    string   `json:"name"`
			Path    []string `json:"path"`
			Status  string   `json:"status"`
			Message string   `json:"message"`
			Output  string   `json:"output"`
		} `json:"cases"`
	} `json:"suites"`
}

// contractTotals is report/render_json.go's jsonTotals; embedded there, so Execution decodes flat.
type contractTotals struct {
	Total     int `json:"total"`
	Passed    int `json:"passed"`
	Failed    int `json:"failed"`
	Error     int `json:"error"`
	Skipped   int `json:"skipped"`
	Filtered  int `json:"filtered"`
	Pending   int `json:"pending"`
	Unstarted int `json:"unstarted"`
}

type contractJUnitDetail struct {
	Message string `xml:"message,attr"`
	Body    string `xml:",chardata"`
}

type contractJUnitDoc struct {
	Tests    int `xml:"tests,attr"`
	Failures int `xml:"failures,attr"`
	Errors   int `xml:"errors,attr"`
	Skipped  int `xml:"skipped,attr"`
	Suites   []struct {
		Name     string `xml:"name,attr"`
		Package  string `xml:"package,attr"`
		Tests    int    `xml:"tests,attr"`
		Failures int    `xml:"failures,attr"`
		Errors   int    `xml:"errors,attr"`
		Skipped  int    `xml:"skipped,attr"`
		Cases    []struct {
			Name      string               `xml:"name,attr"`
			ClassName string               `xml:"classname,attr"`
			Failure   *contractJUnitDetail `xml:"failure"`
			Error     *contractJUnitDetail `xml:"error"`
			Skipped   *contractJUnitDetail `xml:"skipped"`
		} `xml:"testcase"`
	} `xml:"testsuite"`
}

// contractCounts derives the totals every renderer must agree on from the expected cases.
func contractCounts(want []contractCase) (c struct{ total, passed, failed, errored, filtered, unstarted int }) {
	for _, w := range want {
		switch w.status {
		case "passed":
			c.passed++
		case "failed":
			c.failed++
		case "error":
			c.errored++
		case "filtered":
			c.filtered++
		case "unstarted":
			c.unstarted++
		}
		if w.status != "unstarted" {
			c.total++ // Total keeps its "entered execution" meaning; unstarted never counts (#274)
		}
	}
	return c
}

func assertContract(t *testing.T, sc contractScenario, res contractResult) {
	t.Helper()
	counts := contractCounts(sc.want)

	// Normalized SpecResultEvents: declaration order, stable path, status, message, stack.
	if len(res.Events) != len(sc.want) {
		t.Fatalf("got %d events, want %d: %+v", len(res.Events), len(sc.want), res.Events)
	}
	for i, w := range sc.want {
		e := res.Events[i]
		if strings.Join(e.Path, "/") != "suite/"+w.name || e.Status != w.status || e.Message != w.message {
			t.Errorf("event[%d] = %+v, want path suite/%s status %s message %q", i, e, w.name, w.status, w.message)
		}
		if e.HasStack != w.stack || (e.HasOutput && !w.stack) {
			t.Errorf("event[%d] %s: hasOutput=%v hasStack=%v, want stack=%v (only a recovered panic carries output)", i, w.name, e.HasOutput, e.HasStack, w.stack)
		}
	}
	// SuiteEndEvent aggregates: an errored spec is a failed spec there.
	if got := (struct{ Total, Failed, Skipped, Filtered, Unstarted int }{counts.total, counts.failed + counts.errored, 0, counts.filtered, counts.unstarted}); res.Suite != got {
		t.Errorf("SuiteEndEvent totals = %+v, want %+v", res.Suite, got)
	}

	assertContractJSON(t, sc, counts, res.JSON)
	assertContractJUnit(t, sc, counts, res.JUnit)
	assertContractText(t, "txt", sc, res.TXT, false)
	assertContractText(t, "html", sc, res.HTML, true)
}

func assertContractJSON(t *testing.T, sc contractScenario, counts struct{ total, passed, failed, errored, filtered, unstarted int }, raw string) {
	t.Helper()
	var doc contractJSONDoc
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("json: %v", err)
	}
	// Schema v3 is unchanged as a version even though message and status semantics moved in 0.3.0;
	// see docs/REPORTING.md "Consumer migration".
	if doc.SchemaVersion != "3" {
		t.Errorf("json schemaVersion = %q, want 3", doc.SchemaVersion)
	}
	wantTotals := contractTotals{Total: counts.total, Passed: counts.passed, Failed: counts.failed, Error: counts.errored, Filtered: counts.filtered, Unstarted: counts.unstarted}
	if doc.Execution != wantTotals {
		t.Errorf("json execution = %+v, want %+v", doc.Execution, wantTotals)
	}
	if len(doc.Suites) != 1 || doc.Suites[0].Name == "" || doc.Suites[0].Package != contractPackage || doc.Suites[0].Totals != wantTotals {
		t.Fatalf("json suites = %+v, want one named suite in package %q with %+v", doc.Suites, contractPackage, wantTotals)
	}
	cases := doc.Suites[0].Cases
	if len(cases) != len(sc.want) {
		t.Fatalf("json has %d cases, want %d", len(cases), len(sc.want))
	}
	for i, w := range sc.want {
		c := cases[i]
		if c.Name != w.name || strings.Join(c.Path, "/") != "suite/"+w.name || c.Status != w.status || c.Message != w.message || (c.Output != "") != w.stack {
			t.Errorf("json case[%d] = %+v, want %s %s message %q stack=%v", i, c, w.name, w.status, w.message, w.stack)
		}
	}
}

func assertContractJUnit(t *testing.T, sc contractScenario, counts struct{ total, passed, failed, errored, filtered, unstarted int }, raw string) {
	t.Helper()
	var doc contractJUnitDoc
	if err := xml.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("junit: %v", err)
	}
	// tests counts the declared size (Total+Unstarted); skipped folds filtered and unstarted (#274).
	tests, skipped := counts.total+counts.unstarted, counts.filtered+counts.unstarted
	if doc.Tests != tests || doc.Failures != counts.failed || doc.Errors != counts.errored || doc.Skipped != skipped {
		t.Errorf("junit totals = %d/%d/%d/%d, want tests/failures/errors/skipped %d/%d/%d/%d",
			doc.Tests, doc.Failures, doc.Errors, doc.Skipped, tests, counts.failed, counts.errored, skipped)
	}
	if len(doc.Suites) != 1 {
		t.Fatalf("junit has %d suites, want 1", len(doc.Suites))
	}
	s := doc.Suites[0]
	if s.Package != contractPackage || s.Tests != tests || s.Failures != counts.failed || s.Errors != counts.errored || s.Skipped != skipped {
		t.Errorf("junit suite = %+v, want package %q and the same totals", s, contractPackage)
	}
	if len(s.Cases) != len(sc.want) {
		t.Fatalf("junit has %d testcases, want %d", len(s.Cases), len(sc.want))
	}
	for i, w := range sc.want {
		c := s.Cases[i]
		if c.Name != w.name || c.ClassName != contractPackage+"/suite" {
			t.Errorf("junit testcase[%d] = %q/%q, want %q in class %q", i, c.ClassName, c.Name, w.name, contractPackage+"/suite")
		}
		var wantFailure, wantError, wantSkipped bool
		switch w.status {
		case "failed":
			wantFailure = true
		case "error":
			wantError = true
		case "filtered", "unstarted":
			wantSkipped = true
		}
		if (c.Failure != nil) != wantFailure || (c.Error != nil) != wantError || (c.Skipped != nil) != wantSkipped {
			t.Errorf("junit testcase %s: failure=%v error=%v skipped=%v, want %v/%v/%v", w.name, c.Failure != nil, c.Error != nil, c.Skipped != nil, wantFailure, wantError, wantSkipped)
			continue
		}
		switch {
		case c.Failure != nil:
			if c.Failure.Message != w.message {
				t.Errorf("junit <failure> for %s message = %q, want %q (empty for a direct ctx.T failure)", w.name, c.Failure.Message, w.message)
			}
		case c.Error != nil:
			if c.Error.Message != w.message || !strings.Contains(c.Error.Body, "goroutine ") {
				t.Errorf("junit <error> for %s message = %q, stack present = %v, want %q with a stack", w.name, c.Error.Message, strings.Contains(c.Error.Body, "goroutine "), w.message)
			}
		case c.Skipped != nil:
			wantMsg := map[string]string{"filtered": "filtered", "unstarted": "not run: fail-fast"}[w.status]
			if c.Skipped.Message != wantMsg {
				t.Errorf("junit <skipped> for %s message = %q, want %q", w.name, c.Skipped.Message, wantMsg)
			}
		}
	}
}

// assertContractText checks TXT and HTML by content and order only. TXT lists failed and errored
// cases; HTML lists every case. Both must carry the message, the package, and list cases in
// declaration order.
func assertContractText(t *testing.T, format string, sc contractScenario, out string, allCases bool) {
	t.Helper()
	if !strings.Contains(out, contractPackage) {
		t.Errorf("%s output lacks package %q", format, contractPackage)
	}
	last := -1
	for _, w := range sc.want {
		listed := allCases || w.status == "failed" || w.status == "error"
		i := strings.Index(out, "suite/"+w.name)
		if !listed {
			if i >= 0 {
				t.Errorf("%s lists %s (%s), which it must not", format, w.name, w.status)
			}
			continue
		}
		if i < 0 {
			t.Errorf("%s output lacks case suite/%s:\n%s", format, w.name, out)
			continue
		}
		if i < last {
			t.Errorf("%s lists suite/%s before an earlier-declared case:\n%s", format, w.name, out)
		}
		last = i
		if w.message != "" && !strings.Contains(out, w.message) {
			t.Errorf("%s output lacks message %q:\n%s", format, w.message, out)
		}
	}
}
