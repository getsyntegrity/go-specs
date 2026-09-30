// focus_skip_test.go shows FIt, SkipIt, PendingIt and the Focus/Skip/Pending SpecFn wrappers.
//
// These are the tools for working on part of a suite without deleting the rest:
//
//   - SkipIt: "intentionally not executed" (slow, environment-specific, temporarily broken).
//   - PendingIt: "specified, not implemented yet". Reported apart from skipped, so it works as a
//     to-do list.
//   - FIt: "run only this while I debug". Every other spec of the same Describe call is excluded
//     and reported as filtered.
//
// Focus is scoped to a single Describe/BuildSuite call: a FIt never changes what other tests in
// the package run. It is also a policy, not just a filter: a committed FIt fails the enclosing
// test unless GO_SPECS_ALLOW_FOCUS=1 (docs/DSL.md, "Committed focus fails the enclosing test").
// The examples below set that variable with t.Setenv so that they can show the filter working.
package examples_test

import (
	"testing"

	"github.com/getsyntegrity/go-specs/report"
	"github.com/getsyntegrity/go-specs/specs"
)

// statusByName flattens a collected report into name -> status, to keep assertions short.
func statusByName(c *report.Collector) map[string]report.Status {
	out := map[string]report.Status{}
	for _, suite := range c.Report().Suites {
		for _, cs := range suite.Cases {
			out[cs.Name] = cs.Status
		}
	}
	return out
}

// SkipIt keeps the name in the report but never compiles or runs the body. PendingIt does the same
// with a different status, and its body may be nil.
func TestFocusSkip_skipAndPending(t *testing.T) {
	collector := report.NewCollector()
	ran := false

	specs.DescribeWithReporter(t, "payments", collector, func(s *specs.Spec) {
		s.It("charges a card", func(ctx *specs.Context) { ran = true })
		s.SkipIt("talks to the real gateway", func(ctx *specs.Context) {
			t.Error("a skipped spec must not run")
		})
		s.PendingIt("supports wallets", nil)
	})

	got := statusByName(collector)
	if !ran ||
		got["talks to the real gateway"] != report.StatusSkipped ||
		got["supports wallets"] != report.StatusPending {
		t.Fatalf("unexpected statuses: ran=%v %v", ran, got)
	}
}

// FIt focuses a spec. Once a Describe call contains one, only focused specs run; the others (and
// any SkipIt/PendingIt in that call) are excluded and reported as filtered, so a filtered suite is
// visible in the report instead of silently green. Hooks around the focused spec still run.
func TestFocusSkip_fitFocusesOneSpec(t *testing.T) {
	t.Setenv("GO_SPECS_ALLOW_FOCUS", "1") // without this, a committed FIt fails the test
	collector := report.NewCollector()
	var ran []string

	specs.DescribeWithReporter(t, "focus", collector, func(s *specs.Spec) {
		s.It("regular", func(ctx *specs.Context) { ran = append(ran, "regular") })
		s.FIt("focused", func(ctx *specs.Context) { ran = append(ran, "focused") })
		s.SkipIt("skipped", nil)
	})

	got := statusByName(collector)
	if len(ran) != 1 || ran[0] != "focused" ||
		got["regular"] != report.StatusFiltered ||
		got["skipped"] != report.StatusFiltered {
		t.Fatalf("unexpected focus outcome: ran=%v statuses=%v", ran, got)
	}
}

// The Focus, Skip and Pending wrappers return a specs.SpecFn, the same three modes as the methods
// above, for code that picks the mode at runtime. They are passed to Builder.ItWith (the legacy
// Builder API, see builder_test.go). Spec has no ItWith: on a *specs.Spec use FIt, SkipIt and
// PendingIt directly.
func TestFocusSkip_specFnWrappers(t *testing.T) {
	t.Setenv("GO_SPECS_ALLOW_FOCUS", "1")
	var ran []string

	b := specs.NewBuilder()
	b.Describe("wrappers", func() {
		b.It("plain", func(ctx *specs.Context) { ran = append(ran, "plain") })
		b.ItWith("focused", specs.Focus(func(ctx *specs.Context) { ran = append(ran, "focused") }))
		b.ItWith("skipped", specs.Skip(func(ctx *specs.Context) { ran = append(ran, "skipped") }))
		b.ItWith("pending", specs.Pending(nil))
	})
	specs.NewRunner(b.Build()).Run(t)

	if len(ran) != 1 || ran[0] != "focused" {
		t.Fatalf("only the focused spec should run, ran %v", ran)
	}
}
