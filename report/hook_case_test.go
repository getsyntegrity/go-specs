package report

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"strings"
	"testing"
	"unsafe"
)

// hook_case_test.go pins the report-model contract for a synthetic BeforeAll/AfterAll group hook
// case (issue #207, docs/SUITE_HOOKS_CONTRACT.md H8): a failed group hook is reported as one case,
// structurally distinguishable from a real spec by SpecResultEvent.Hook / Case.Hook (not merely by
// its bracketed name), rendered by every renderer, and counted in totals like any other case. A
// suite that never registers a group hook must see Hook stay "" everywhere and its report output
// stay byte-for-byte identical — that half of the contract is already covered by the existing
// golden tests (TestRenderTXTGolden, TestRenderJSONGolden, TestRenderXMLGolden), which this file
// does not duplicate.

// TestSpecResultEventSizeUnchanged pins the fix for the #207 performance regression: commit
// 078ccd1 added a "Hook string" field to SpecStartEvent (embedded in SpecResultEvent), growing
// SpecResultEvent from 112 to 128 bytes on amd64. SpecResultEvent is copied by value once per spec
// on every engine, so that growth was paid by every suite, including Builder/Runner's
// BenchmarkSuite_1000 which never emits a hook case at all — a ~15% regression (17.5µs to 20.2µs)
// that violates H10 ("no cost for suites without BeforeAll/AfterAll", docs/SUITE_HOOKS_CONTRACT.md).
// The fix moves the hook marker onto SpecResultEvent itself as a HookKind (uint8), sized to fit in
// padding the existing Failed/Skipped/Filtered/Pending bools already leave before Duration, so
// SpecResultEvent returns to its original, pre-#207 size.
func TestSpecResultEventSizeUnchanged(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("size assertion is only meaningful on a 64-bit platform")
	}
	const want = 112
	if got := unsafe.Sizeof(SpecResultEvent{}); got != want {
		t.Fatalf("unsafe.Sizeof(SpecResultEvent{}) = %d, want %d bytes (issue #207 H10: SpecResultEvent "+
			"is copied per spec on every engine, so any growth here is a per-spec tax paid even by "+
			"suites with no BeforeAll/AfterAll)", got, want)
	}
}

// TestSpecResultEventCarriesHook proves SpecResultEvent carries its own Hook, independent of the
// embedded SpecStartEvent (which does not have a Hook field at all — see events.go's HookKind
// doc): the marker lives only on the result event.
func TestSpecResultEventCarriesHook(t *testing.T) {
	start := SpecStartEvent{Name: "[BeforeAll]"}
	result := SpecResultEvent{SpecStartEvent: start, Failed: true, Hook: HookBeforeAll}
	if result.Hook != HookBeforeAll {
		t.Fatalf("SpecResultEvent.Hook = %v, want %v", result.Hook, HookBeforeAll)
	}
}

// TestCollectorCopiesHookIntoCase proves the Collector copies SpecResultEvent.Hook onto the Case
// it builds, as Case.Hook's string form, for both hook kinds and for the ordinary empty value.
func TestCollectorCopiesHookIntoCase(t *testing.T) {
	cases := []struct {
		name string
		hook HookKind
		want string
	}{
		{"ordinary spec", HookNone, ""},
		{"BeforeAll", HookBeforeAll, "BeforeAll"},
		{"AfterAll", HookAfterAll, "AfterAll"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := NewCollector()
			c.SuiteStarted(SuiteStartEvent{Name: "S"})
			c.SpecFinished(SpecResultEvent{
				SpecStartEvent: SpecStartEvent{Name: "[" + tc.want + "]"},
				Failed:         tc.hook != HookNone,
				Hook:           tc.hook,
			})
			c.SuiteFinished(SuiteEndEvent{Name: "S"})
			r := c.Report()
			if got := r.Suites[0].Cases[0].Hook; got != tc.want {
				t.Fatalf("Case.Hook = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestHookCaseCountsInTotals proves a failed hook case is counted in both the suite's and the
// report's totals exactly like any other failed case (docs/SUITE_HOOKS_CONTRACT.md H8: "counted in
// totals").
func TestHookCaseCountsInTotals(t *testing.T) {
	c := NewCollector()
	c.SuiteStarted(SuiteStartEvent{Name: "S"})
	c.SpecFinished(SpecResultEvent{SpecStartEvent: SpecStartEvent{Name: "[BeforeAll]"}, Failed: true, Hook: HookBeforeAll})
	c.SuiteFinished(SuiteEndEvent{Name: "S"})
	r := c.Report()

	want := Totals{Total: 1, Failed: 1}
	if r.Suites[0].Totals != want {
		t.Fatalf("suite totals = %+v, want %+v", r.Suites[0].Totals, want)
	}
	if r.Execution != want {
		t.Fatalf("execution totals = %+v, want %+v", r.Execution, want)
	}
}

func sampleReportWithFailedBeforeAllHook() NormalizedReport {
	c := NewCollector()
	c.SuiteStarted(SuiteStartEvent{Name: "Checkout"})
	c.SpecFinished(SpecResultEvent{
		SpecStartEvent: SpecStartEvent{Name: "[BeforeAll]", Path: []string{"Checkout", "when the cart has items"}},
		Failed:         true,
		Message:        "expected true, got false",
		Hook:           HookBeforeAll,
	})
	c.SuiteFinished(SuiteEndEvent{Name: "Checkout"})
	return c.Report()
}

// TestRenderTXTShowsHookKind proves RenderTXT surfaces which hook kind failed, not just the
// bracketed name already in Case.Name.
func TestRenderTXTShowsHookKind(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderTXT(&buf, sampleReportWithFailedBeforeAllHook()); err != nil {
		t.Fatalf("RenderTXT: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "[BeforeAll]") {
		t.Fatalf("expected the hook case name in output:\n%s", out)
	}
	if !strings.Contains(out, "hook: BeforeAll") {
		t.Fatalf("expected a structural hook indicator (\"hook: BeforeAll\") in output:\n%s", out)
	}
}

// TestRenderJSONHookField proves RenderJSON emits "hook" for a hook case and omits the key
// entirely for an ordinary case (omitempty), so a consumer never sees "hook":"" noise.
func TestRenderJSONHookField(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderJSON(&buf, sampleReportWithFailedBeforeAllHook()); err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}
	var doc jsonReport
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got := doc.Suites[0].Cases[0].Hook; got != "BeforeAll" {
		t.Fatalf("jsonCase.Hook = %q, want %q", got, "BeforeAll")
	}
	if bytes.Contains(buf.Bytes(), []byte(`"hook": ""`)) {
		t.Fatalf(`expected omitempty to drop an empty "hook" key, got:%s`, buf.String())
	}

	// An ordinary suite must render with no "hook" key at all.
	var ordinary bytes.Buffer
	if err := RenderJSON(&ordinary, sampleReport()); err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}
	if bytes.Contains(ordinary.Bytes(), []byte(`"hook"`)) {
		t.Fatalf("expected no \"hook\" key for a suite with no group hooks, got:\n%s", ordinary.String())
	}
}

// TestRenderHTMLHookBadge proves RenderHTML renders a structural hook indicator for a hook case
// and nothing extra for an ordinary suite.
func TestRenderHTMLHookBadge(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderHTML(&buf, sampleReportWithFailedBeforeAllHook()); err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}
	if !strings.Contains(buf.String(), "hook-BeforeAll") {
		t.Fatalf("expected a hook-BeforeAll class/marker in HTML output:\n%s", buf.String())
	}

	var ordinary bytes.Buffer
	if err := RenderHTML(&ordinary, sampleReport()); err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}
	if strings.Contains(ordinary.String(), "hook-") {
		t.Fatalf("expected no hook marker for a suite with no group hooks, got:\n%s", ordinary.String())
	}
}

// TestRenderXMLHookAttribute proves RenderXML (JUnit) carries the hook kind as a structural
// attribute on the <testcase>, and omits it entirely for an ordinary spec.
func TestRenderXMLHookAttribute(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderXML(&buf, sampleReportWithFailedBeforeAllHook()); err != nil {
		t.Fatalf("RenderXML: %v", err)
	}
	var doc junitTestSuites
	if err := xml.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("xml.Unmarshal: %v", err)
	}
	if len(doc.Suites) != 1 || len(doc.Suites[0].TestCases) != 1 {
		t.Fatalf("got %+v, want one suite with one testcase", doc.Suites)
	}
	if got := doc.Suites[0].TestCases[0].Hook; got != "BeforeAll" {
		t.Fatalf("testcase hook attribute = %q, want %q", got, "BeforeAll")
	}
	if bytes.Contains(buf.Bytes(), []byte(`hook="`)) == false {
		t.Fatalf("expected a hook attribute in the raw XML, got:\n%s", buf.String())
	}

	var ordinary bytes.Buffer
	if err := RenderXML(&ordinary, sampleReport()); err != nil {
		t.Fatalf("RenderXML: %v", err)
	}
	if bytes.Contains(ordinary.Bytes(), []byte(`hook="`)) {
		t.Fatalf("expected no hook attribute for a suite with no group hooks, got:\n%s", ordinary.String())
	}
}
