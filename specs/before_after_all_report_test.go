package specs

import (
	"bytes"
	"encoding/xml"
	"slices"
	"strings"
	"testing"

	"github.com/getsyntegrity/go-specs/report"
)

// before_after_all_report_test.go pins the identity a synthetic group hook case carries end to
// end — from the event the runner emits, through report.Collector, into the JUnit and TXT
// renderers (issue #207, docs/SUITE_HOOKS_CONTRACT.md H8). The renderers already treat a hook
// case's Path as the GROUP path; these tests make sure the runner actually emits it that way,
// instead of appending the "[BeforeAll]"/"[AfterAll]" marker to it a second time.

// hookIdentitySuite is one suite with a failing BeforeAll in a nested group and a failing AfterAll
// in the root group, so both hook kinds and both nesting depths are covered by one run.
func hookIdentitySuite(s *Spec) {
	s.AfterAll(func(ctx *Context) { ctx.Expect(false).To(BeTrue()) })
	s.When("when cart has items", func(w *Spec) {
		w.BeforeAll(func(ctx *Context) { ctx.Expect(false).To(BeTrue()) })
		w.It("charges the card", func(*Context) {})
	})
	s.It("passes", func(*Context) {})
}

// TestHookCaseEventPathIsTheGroupPathOnly asserts on the real emitted events: a hook case's Path is
// exactly the group's declared scope chain, with no marker element appended.
func TestHookCaseEventPathIsTheGroupPathOnly(t *testing.T) {
	rep := &recordingReporter{}
	runCompiledSuiteWith(&controlledBackend{}, rep, BuildSuite(nil, "Checkout", hookIdentitySuite))

	want := map[report.HookKind][]string{
		report.HookBeforeAll: {"Checkout", "when cart has items"},
		report.HookAfterAll:  {"Checkout"},
	}
	seen := map[report.HookKind]bool{}
	for _, e := range rep.specFinished {
		if e.Hook == report.HookNone {
			continue
		}
		seen[e.Hook] = true
		if !slices.Equal(e.Path, want[e.Hook]) {
			t.Fatalf("[%s] case Path = %q, want the group path %q", e.Hook, e.Path, want[e.Hook])
		}
		if e.Name != "["+e.Hook.String()+"]" {
			t.Fatalf("[%s] case Name = %q, want %q", e.Hook, e.Name, "["+e.Hook.String()+"]")
		}
	}
	for kind := range want {
		if !seen[kind] {
			t.Fatalf("no %s hook case was emitted; events: %+v", kind, rep.specFinished)
		}
	}
	// SpecStarted carries the same identity as SpecFinished for the hook case.
	for _, e := range rep.specStarted {
		if strings.HasPrefix(e.Name, "[") && slices.Contains(e.Path, e.Name) {
			t.Fatalf("hook SpecStarted Path %q repeats the marker %q", e.Path, e.Name)
		}
	}
}

// TestHookCaseRendersWithGroupIdentity runs the same suite through report.Collector and checks the
// rendered output: the JUnit classname is exactly the group path and the TXT line is
// "<group path> [BeforeAll]", with the marker appearing exactly once.
func TestHookCaseRendersWithGroupIdentity(t *testing.T) {
	c := report.NewCollector()
	c.SuiteStarted(report.SuiteStartEvent{Name: "Checkout"})
	runCompiledSuiteWith(&controlledBackend{}, c, BuildSuite(nil, "Checkout", hookIdentitySuite))
	c.SuiteFinished(report.SuiteEndEvent{Name: "Checkout"})
	normalized := c.Report()

	var xmlOut bytes.Buffer
	if err := report.RenderXML(&xmlOut, normalized); err != nil {
		t.Fatalf("RenderXML: %v", err)
	}
	var doc struct {
		Suites []struct {
			Cases []struct {
				Name      string `xml:"name,attr"`
				ClassName string `xml:"classname,attr"`
			} `xml:"testcase"`
		} `xml:"testsuite"`
	}
	if err := xml.Unmarshal(xmlOut.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshal JUnit output: %v\n%s", err, xmlOut.String())
	}
	wantClass := map[string]string{
		"[BeforeAll]": "Checkout/when cart has items",
		"[AfterAll]":  "Checkout",
	}
	found := 0
	for _, s := range doc.Suites {
		for _, tc := range s.Cases {
			want, ok := wantClass[tc.Name]
			if !ok {
				continue
			}
			found++
			if tc.ClassName != want {
				t.Fatalf("JUnit classname for %s = %q, want the group path %q", tc.Name, tc.ClassName, want)
			}
		}
	}
	if found != len(wantClass) {
		t.Fatalf("found %d hook testcases in JUnit output, want %d:\n%s", found, len(wantClass), xmlOut.String())
	}

	var txtOut bytes.Buffer
	if err := report.RenderTXT(&txtOut, normalized); err != nil {
		t.Fatalf("RenderTXT: %v", err)
	}
	txt := txtOut.String()
	for _, line := range []string{"Checkout/when cart has items [BeforeAll]", "Checkout [AfterAll]"} {
		if !strings.Contains(txt, "FAIL  "+line+" (") {
			t.Fatalf("TXT output does not contain the line %q:\n%s", line, txt)
		}
	}
	if n := strings.Count(txt, "[BeforeAll]"); n != 1 {
		t.Fatalf("TXT output mentions [BeforeAll] %d times, want exactly once:\n%s", n, txt)
	}
}

// runCompiledSuiteWith runs a BuildSuite(nil, ...) result against backend/rep without the
// SuiteStarted/SuiteFinished pair CompiledSuite.Run adds, so a test can drive a fake backend.
func runCompiledSuiteWith(backend testBackend, rep report.EventReporter, s *CompiledSuite) {
	s.runSpecs(backend, rep)
}
