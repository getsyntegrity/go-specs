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
// A failing spec would turn this example red, so the example below runs with fail-fast enabled on
// a suite where everything passes: nothing is cut short, and the report shows zero unstarted
// specs. The unstarted status only appears once a spec actually fails. The legacy Builder engine
// has the same switch as Runner.FailFast, and Context.SetFailFast is the hook the runners call
// internally; suites should use CompiledSuite.SetFailFast.
package examples_test

import (
	"testing"

	"github.com/getsyntegrity/go-specs/report"
	"github.com/getsyntegrity/go-specs/specs"
)

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
	if totals.Passed != 3 || totals.Unstarted != 0 || len(order) != 6 {
		t.Fatalf("all specs pass, so none may be cut short: totals=%+v order=%v", totals, order)
	}
}
