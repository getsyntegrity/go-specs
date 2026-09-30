// fail_fast_test.go shows CompiledSuite.SetFailFast.
//
// Fail-fast stops a run after the first failure, which is useful when later specs depend on
// earlier ones or a long suite should give feedback quickly. Semantics:
//
//   - The failing spec still runs its own AfterEach hooks, and the AfterAll of every group that was
//     already entered still runs.
//   - Specs that never start are not silently dropped: they are reported with the "unstarted"
//     status (report.StatusUnstarted) and are not counted in the suite total.
//   - A spec inside an ItParallel batch that already started runs to completion; the stop applies
//     once the whole batch has finished.
//   - Filtered (`-run`) and skipped specs are not failures and never trigger the stop.
//
// A failing spec would turn this example red, so the example runs the suite on recordingTB, a
// small stand-in for *testing.T that records failures instead of reporting them. The suite sees a
// real failure and cuts the rest of the run; the example asserts on the report and stays green.
// The legacy Builder engine has the same switch as Runner.FailFast, and Context.SetFailFast is
// the hook the runners call internally; suites should use CompiledSuite.SetFailFast.
package examples_test

import (
	"fmt"
	"testing"

	"github.com/getsyntegrity/go-specs/report"
	"github.com/getsyntegrity/go-specs/specs"
)

// recordingTB stands in for *testing.T while a suite runs, so the failing spec this example needs
// is recorded instead of failing the real test. Embedding testing.TB keeps the rest of the
// interface (Name, Log, Cleanup, ...) delegated to the real T; only the failure methods are
// replaced. Because it is not a *testing.T, the suite runs its specs directly on it.
type recordingTB struct {
	testing.TB
	failures []string
}

func (r *recordingTB) Helper()      {}
func (r *recordingTB) Failed() bool { return len(r.failures) > 0 }
func (r *recordingTB) Fail()        { r.failures = append(r.failures, "Fail") }
func (r *recordingTB) FailNow()     { r.Fail() }
func (r *recordingTB) Error(args ...any) {
	r.failures = append(r.failures, fmt.Sprint(args...))
}
func (r *recordingTB) Errorf(format string, args ...any) {
	r.failures = append(r.failures, fmt.Sprintf(format, args...))
}
func (r *recordingTB) Fatal(args ...any)                 { r.Error(args...) }
func (r *recordingTB) Fatalf(format string, args ...any) { r.Errorf(format, args...) }

// One spec fails, and the two after it are cut: they are reported as unstarted and their bodies
// never execute. The example itself stays green because the failure lands on recordingTB.
func TestFailFast_failureCutsLaterSpecsAsUnstarted(t *testing.T) {
	collector := report.NewCollector()
	var ran []string

	suite := specs.BuildSuite(t, "checkout", func(s *specs.Spec) {
		s.It("validates the cart", func(ctx *specs.Context) { ran = append(ran, "validate") })
		s.It("charges the card", func(ctx *specs.Context) {
			ran = append(ran, "charge")
			ctx.Expect("declined").ToEqual("approved") // the failure that triggers the cut
		})
		s.It("sends the receipt", func(ctx *specs.Context) { ran = append(ran, "receipt") })
		s.It("updates the stock", func(ctx *specs.Context) { ran = append(ran, "stock") })
	})
	suite.Reporter = collector
	suite.SetFailFast(true)

	fake := &recordingTB{TB: t}
	suite.Run(fake)

	rep := collector.Report()
	statuses := map[string]report.Status{}
	for _, c := range rep.Suites[0].Cases {
		statuses[c.Name] = c.Status
	}
	want := map[string]report.Status{
		"validates the cart": report.StatusPassed,
		"charges the card":   report.StatusFailed,
		"sends the receipt":  report.StatusUnstarted,
		"updates the stock":  report.StatusUnstarted,
	}
	for name, status := range want {
		if statuses[name] != status {
			t.Errorf("%q: status = %v, want %v", name, statuses[name], status)
		}
	}
	if len(ran) != 2 {
		t.Errorf("only the first two spec bodies may run, got %v", ran)
	}
	if !fake.Failed() {
		t.Error("the charging spec should have failed on the recording backend")
	}
	totals := rep.Execution
	if totals.Total != 2 || totals.Passed != 1 || totals.Failed != 1 || totals.Unstarted != 2 {
		t.Errorf("totals = %+v, want Total 2 (unstarted never counts), Passed 1, Failed 1, Unstarted 2", totals)
	}
}

// With fail-fast enabled on a suite where everything passes, nothing is cut short and the report
// shows zero unstarted specs: the unstarted status only appears once a spec actually fails.
func TestFailFast_enabledOnPassingSuite(t *testing.T) {
	collector := report.NewCollector()
	var order []string

	suite := specs.BuildSuite(t, "checkout", func(s *specs.Spec) {
		s.AfterEach(func(ctx *specs.Context) { order = append(order, "after") })
		s.It("validates the cart", func(ctx *specs.Context) { order = append(order, "validate") })
		s.It("charges the card", func(ctx *specs.Context) { order = append(order, "charge") })
		s.It("sends the receipt", func(ctx *specs.Context) { order = append(order, "receipt") })
	})
	suite.Reporter = collector
	suite.SetFailFast(true)
	suite.Run(t)

	totals := collector.Report().Execution
	// Specs outside a `-run` selection are filtered, so count what ran: each passing spec is
	// followed by its AfterEach.
	if totals.Failed != 0 || totals.Unstarted != 0 || len(order) != 2*totals.Passed {
		t.Fatalf("all specs pass, so none may be cut short: totals=%+v order=%v", totals, order)
	}
}
