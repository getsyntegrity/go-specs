// pending_test.go pins the pending-spec contract for issue #208: PendingIt/Pending mirror
// SkipIt/Skip's "never runs, identity preserved" shape, but report a distinct status so a pending
// spec ("not implemented yet") is never confused with a skipped one ("intentionally not
// executed") — see pending.go, builder.go's finalize, and runner.go's reporterObserver.
package specs

import (
	"sort"
	"strings"
	"testing"

	"github.com/getsyntegrity/go-specs/report"
)

// TestPendingItDoesNotRunButKeepsIdentity mirrors TestItWithSkipDoesNotRunButKeepsIdentity: a
// PendingIt spec's body must never compile into a step, but its name must still be preserved,
// under group.pendingSpecs rather than group.skipped.
func TestPendingItDoesNotRunButKeepsIdentity(t *testing.T) {
	ran := false
	b := NewBuilder()
	b.PendingIt("not implemented yet", func(*Context) { ran = true })
	b.It("runs", func(*Context) {})
	prog := b.Build()

	if len(prog.Groups) != 1 {
		t.Fatalf("expected one group; got %d", len(prog.Groups))
	}
	g := prog.Groups[0]
	if len(g.specs) != 1 {
		t.Fatalf("expected only the non-pending spec to compile into a step; got %d", len(g.specs))
	}
	if len(g.pendingSpecs) != 1 || g.pendingSpecs[0] != "not implemented yet" {
		t.Fatalf("expected the pending spec's identity to be preserved as %q, got %v", "not implemented yet", g.pendingSpecs)
	}
	if len(g.skipped) != 0 {
		t.Fatalf("expected a pending spec not to be recorded as skipped, got %v", g.skipped)
	}
	NewRunner(prog).Run(t)
	if ran {
		t.Fatal("expected the pending spec's body to never run")
	}
}

// TestPendingItNilBodyNeverRuns proves PendingIt accepts a nil body (a pending spec often has no
// implementation yet) without panicking, and still preserves the spec's identity.
func TestPendingItNilBodyNeverRuns(t *testing.T) {
	b := NewBuilder()
	b.PendingIt("no body yet", nil)
	prog := b.Build()
	if len(prog.Groups) != 1 || len(prog.Groups[0].pendingSpecs) != 1 || prog.Groups[0].pendingSpecs[0] != "no body yet" {
		t.Fatalf("expected one pending spec named %q, got %+v", "no body yet", prog.Groups)
	}
	NewRunner(prog).Run(t) // must not panic
}

// TestItWithPendingRoutesToPendingIt proves ItWith("name", Pending(fn)) behaves exactly like
// PendingIt(name, fn): fn never runs, name is preserved as pending.
func TestItWithPendingRoutesToPendingIt(t *testing.T) {
	ran := false
	b := NewBuilder()
	b.ItWith("not ready", Pending(func(*Context) { ran = true }))
	prog := b.Build()
	if len(prog.Groups) != 1 || len(prog.Groups[0].pendingSpecs) != 1 || prog.Groups[0].pendingSpecs[0] != "not ready" {
		t.Fatalf("expected ItWith(Pending) to record one pending spec named %q, got %+v", "not ready", prog.Groups)
	}
	NewRunner(prog).Run(t)
	if ran {
		t.Fatal("expected the pending spec's body to never run")
	}
}

// TestFocusDropsPendingSpecs mirrors TestFocusAndSkipInteraction: when any spec in the suite is
// focused, pending specs are dropped exactly like plain and skipped specs — a suite mid-TDD with a
// pending spec and a focused spec should not still report the pending spec.
func TestFocusDropsPendingSpecs(t *testing.T) {
	var order []string
	b := NewBuilder()
	b.It("A", func(*Context) { order = append(order, "A") })
	b.FIt("B", func(*Context) { order = append(order, "B") })
	b.PendingIt("C", func(*Context) { order = append(order, "C") })
	prog := b.Build()

	if len(prog.Groups) != 1 || len(prog.Groups[0].specs) != 1 {
		t.Fatalf("expected 1 group with 1 spec (B); got %d groups", len(prog.Groups))
	}
	if len(prog.Groups[0].pendingSpecs) != 0 {
		t.Fatalf("expected the pending spec to be dropped when a focus is present, got %v", prog.Groups[0].pendingSpecs)
	}
	NewRunner(prog).Run(t)
	if len(order) != 1 || order[0] != "B" {
		t.Errorf("order=%v, want [B]", order)
	}
}

// TestRunnerWithReporterReportsPendingSpec mirrors TestRunnerWithReporterReportsSkippedSpec: a
// PendingIt spec is reported as its own SpecStarted/SpecFinished{Pending: true, Failed: false,
// Skipped: false} pair, with Duration 0 and its body never run, and it counts in
// SuiteEndEvent.TotalSpecs/PendingSpecs.
func TestRunnerWithReporterReportsPendingSpec(t *testing.T) {
	var ranPending bool
	rep := &recordingReporter{}
	prog := BuildProgram(func(b *Builder) {
		b.Describe("Suite", func() {
			b.It("a", func(ctx *Context) {})
			b.PendingIt("b", func(ctx *Context) { ranPending = true })
			b.It("c", func(ctx *Context) {})
		})
	})

	NewRunnerWithReporter(prog, "Suite", rep).Run(t)

	if ranPending {
		t.Fatal("expected the pending spec's body to never run")
	}
	if len(rep.specStarted) != 3 || len(rep.specFinished) != 3 {
		t.Fatalf("expected three SpecStarted/SpecFinished (a, b, c), got started=%+v finished=%+v", rep.specStarted, rep.specFinished)
	}
	var pending *report.SpecResultEvent
	for i := range rep.specFinished {
		if rep.specFinished[i].Name == "b" {
			pending = &rep.specFinished[i]
		}
	}
	if pending == nil {
		t.Fatalf("expected a SpecFinished named %q, got %+v", "b", rep.specFinished)
	}
	if !pending.Pending {
		t.Errorf("expected Pending=true for %q, got %+v", "b", *pending)
	}
	if pending.Skipped {
		t.Errorf("expected Skipped=false for a pending spec, got %+v", *pending)
	}
	if pending.Failed {
		t.Errorf("expected Failed=false for a pending spec, got %+v", *pending)
	}
	if pending.Duration != 0 {
		t.Errorf("expected Duration=0 for a pending spec (no body ran), got %v", pending.Duration)
	}
	end := rep.suiteFinished[0]
	if end.TotalSpecs != 3 || end.FailedSpecs != 0 || end.PendingSpecs != 1 || end.SkippedSpecs != 0 {
		t.Fatalf("expected TotalSpecs=3 FailedSpecs=0 PendingSpecs=1 SkippedSpecs=0, got %+v", end)
	}
}

// TestRunnerWithReporterReportsPendingSpecPath mirrors TestRunnerWithReporterReportsSkippedSpecPath:
// a pending spec's declared enclosing scopes reach SpecStartEvent.Path too.
func TestRunnerWithReporterReportsPendingSpecPath(t *testing.T) {
	rep := &recordingReporter{}
	prog := BuildProgram(func(b *Builder) {
		b.Describe("Suite", func() {
			b.Describe("When nested", func() {
				b.PendingIt("not done", func(ctx *Context) {})
			})
		})
	})

	NewRunnerWithReporter(prog, "Suite", rep).Run(t)

	var got *report.SpecStartEvent
	for i := range rep.specStarted {
		if rep.specStarted[i].Name == "not done" {
			got = &rep.specStarted[i]
		}
	}
	if got == nil {
		t.Fatalf("expected a SpecStarted named %q, got %+v", "not done", rep.specStarted)
	}
	wantPath := []string{"Suite", "When nested", "not done"}
	if strings.Join(got.Path, "|") != strings.Join(wantPath, "|") {
		t.Fatalf("expected declared Path %q, got %q", wantPath, got.Path)
	}
}

// TestRunnerWithReporterCountsMixedPassFailSkipPending proves TotalSpecs/FailedSpecs/SkippedSpecs/
// PendingSpecs all combine correctly when a suite mixes all four kinds — pending must not be
// folded into the skipped count or vice versa.
func TestRunnerWithReporterCountsMixedPassFailSkipPending(t *testing.T) {
	rep := &recordingReporter{}
	prog := BuildProgram(func(b *Builder) {
		b.Describe("Suite", func() {
			b.It("passes", func(ctx *Context) {})
			b.It("fails", func(ctx *Context) { ctx.recordFailure() })
			b.SkipIt("skipped", func(ctx *Context) {})
			b.PendingIt("pending", func(ctx *Context) {})
		})
	})

	NewRunnerWithReporter(prog, "Suite", rep).Run(t)

	end := rep.suiteFinished[0]
	if end.TotalSpecs != 4 || end.FailedSpecs != 1 || end.SkippedSpecs != 1 || end.PendingSpecs != 1 {
		t.Fatalf("expected TotalSpecs=4 FailedSpecs=1 SkippedSpecs=1 PendingSpecs=1, got %+v", end)
	}

	var skippedNames, pendingNames []string
	for _, e := range rep.specFinished {
		if e.Skipped {
			skippedNames = append(skippedNames, e.Name)
		}
		if e.Pending {
			pendingNames = append(pendingNames, e.Name)
		}
	}
	sort.Strings(skippedNames)
	sort.Strings(pendingNames)
	if len(skippedNames) != 1 || skippedNames[0] != "skipped" {
		t.Fatalf("expected skipped names [skipped], got %v", skippedNames)
	}
	if len(pendingNames) != 1 || pendingNames[0] != "pending" {
		t.Fatalf("expected pending names [pending], got %v", pendingNames)
	}
}

// TestRunShardWithReporterCountsOnlyShardPending mirrors
// TestRunShardWithReporterCountsOnlyShardSkips: a shard's SuiteEndEvent.PendingSpecs (and reported
// SpecFinished{Pending:true} events) reflect only the pending specs in groups assigned to that
// shard, since RunShard/RunShardWithReporter shard whole groups and a group's pending names travel
// with it like any other group data.
func TestRunShardWithReporterCountsOnlyShardPending(t *testing.T) {
	prog := &Program{Groups: []group{
		{pendingSpecs: []string{"pending-a"}},
		{specs: []step{func(*Context) {}}, names: []string{"b"}},
		{pendingSpecs: []string{"pending-c"}},
		{specs: []step{func(*Context) {}}, names: []string{"d"}},
	}}

	rep := &recordingReporter{}
	RunShardWithReporter(prog, t, 0, 4, "Shard0", rep)

	if len(rep.specFinished) != 1 || rep.specFinished[0].Name != "pending-a" || !rep.specFinished[0].Pending {
		t.Fatalf("expected shard 0 to report exactly pending-a as pending, got %+v", rep.specFinished)
	}
	end := rep.suiteFinished[0]
	if end.TotalSpecs != 1 || end.PendingSpecs != 1 {
		t.Fatalf("expected shard 0 TotalSpecs=1 PendingSpecs=1 (only pending-a's group), got %+v", end)
	}

	rep2 := &recordingReporter{}
	RunShardWithReporter(prog, t, 2, 4, "Shard2", rep2)

	if len(rep2.specFinished) != 1 || rep2.specFinished[0].Name != "pending-c" || !rep2.specFinished[0].Pending {
		t.Fatalf("expected shard 2 to report exactly pending-c as pending, got %+v", rep2.specFinished)
	}
	end2 := rep2.suiteFinished[0]
	if end2.TotalSpecs != 1 || end2.PendingSpecs != 1 {
		t.Fatalf("expected shard 2 TotalSpecs=1 PendingSpecs=1 (only pending-c's group), got %+v", end2)
	}

	rep3 := &recordingReporter{}
	RunShardWithReporter(prog, t, 1, 4, "Shard1", rep3)

	if len(rep3.specFinished) != 1 || rep3.specFinished[0].Pending {
		t.Fatalf("expected shard 1 (group b) to report zero pending specs, got %+v", rep3.specFinished)
	}
	end3 := rep3.suiteFinished[0]
	if end3.PendingSpecs != 0 {
		t.Fatalf("expected shard 1 PendingSpecs=0, got %+v", end3)
	}
}

// TestPendingReportRoundTripsThroughCollector proves a pending spec's event, once fed through
// report.Collector (the same path DescribeWithReporter uses), classifies as report.StatusPending —
// not StatusSkipped — end to end from the Builder engine into the normalized report model.
func TestPendingReportRoundTripsThroughCollector(t *testing.T) {
	rep := &recordingReporter{}
	prog := BuildProgram(func(b *Builder) {
		b.Describe("Suite", func() {
			b.PendingIt("not implemented yet", func(ctx *Context) {})
		})
	})
	NewRunnerWithReporter(prog, "Suite", rep).Run(t)

	c := report.NewCollector()
	c.SuiteStarted(rep.suiteStarted[0])
	for _, e := range rep.specFinished {
		c.SpecFinished(e)
	}
	c.SuiteFinished(rep.suiteFinished[0])

	got := c.Report()
	if len(got.Suites) != 1 || len(got.Suites[0].Cases) != 1 {
		t.Fatalf("expected one suite with one case, got %+v", got.Suites)
	}
	if got.Suites[0].Cases[0].Status != report.StatusPending {
		t.Fatalf("expected StatusPending, got %v", got.Suites[0].Cases[0].Status)
	}
	if got.Suites[0].Totals.Pending != 1 || got.Suites[0].Totals.Skipped != 0 {
		t.Fatalf("expected Totals.Pending=1 Totals.Skipped=0, got %+v", got.Suites[0].Totals)
	}
}
