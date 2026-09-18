package specs

import (
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/report"
)

// TestRunnerWithReporterEmitsSuiteAndSpecEvents proves NewRunnerWithReporter's Runner reports the
// same SuiteStarted/SpecStarted/SpecFinished/SuiteFinished shape as CompiledSuite/Describe do (see
// execution_plan_reporter_test.go), for the separate Program/Runner execution model.
func TestRunnerWithReporterEmitsSuiteAndSpecEvents(t *testing.T) {
	rep := &recordingReporter{}
	prog := BuildProgram(func(b *Builder) {
		b.Describe("Suite", func() {
			b.It("passes", func(ctx *Context) {})
			b.It("fails", func(ctx *Context) { ctx.recordFailure() })
		})
	})

	NewRunnerWithReporter(prog, "Suite", rep).Run(t)

	if len(rep.suiteStarted) != 1 || rep.suiteStarted[0].Name != "Suite" {
		t.Fatalf("expected one SuiteStarted for Suite, got %+v", rep.suiteStarted)
	}
	if len(rep.specStarted) != 2 {
		t.Fatalf("expected two SpecStarted, got %+v", rep.specStarted)
	}
	if len(rep.specFinished) != 2 {
		t.Fatalf("expected two SpecFinished, got %+v", rep.specFinished)
	}
	byName := map[string]bool{}
	for _, e := range rep.specFinished {
		byName[e.Name] = e.Failed
	}
	if failed, ok := byName["passes"]; !ok || failed {
		t.Errorf("expected 'passes' to be reported as not failed, got %+v", rep.specFinished)
	}
	if failed, ok := byName["fails"]; !ok || !failed {
		t.Errorf("expected 'fails' to be reported as failed, got %+v", rep.specFinished)
	}
	if len(rep.suiteFinished) != 1 {
		t.Fatalf("expected one SuiteFinished, got %+v", rep.suiteFinished)
	}
	end := rep.suiteFinished[0]
	if end.Name != "Suite" || end.TotalSpecs != 2 || end.FailedSpecs != 1 {
		t.Fatalf("expected Suite TotalSpecs=2 FailedSpecs=1, got %+v", end)
	}
}

// TestRunnerWithReporterRecordsSpecAndSuiteDuration proves SpecFinished.Duration and
// SuiteFinished.Duration measure real elapsed time (not a placeholder zero), and that
// SpecFinished.SpecStartEvent is the exact event SpecStarted sent — the invariant Duration depends
// on, since it's computed from that event's Time, not reconstructed at finish time.
func TestRunnerWithReporterRecordsSpecAndSuiteDuration(t *testing.T) {
	const sleep = 20 * time.Millisecond
	rep := &recordingReporter{}
	prog := BuildProgram(func(b *Builder) {
		b.Describe("Suite", func() {
			b.It("slow", func(ctx *Context) { time.Sleep(sleep) })
		})
	})

	NewRunnerWithReporter(prog, "Suite", rep).Run(t)

	if len(rep.specStarted) != 1 || len(rep.specFinished) != 1 {
		t.Fatalf("expected one SpecStarted/SpecFinished pair, got started=%+v finished=%+v", rep.specStarted, rep.specFinished)
	}
	started, finished := rep.specStarted[0], rep.specFinished[0]
	if !finished.Time.Equal(started.Time) {
		t.Fatalf("expected SpecFinished.SpecStartEvent to be the exact SpecStarted event, got start=%v finish=%v", started.Time, finished.Time)
	}
	if finished.Duration < sleep {
		t.Fatalf("expected spec Duration >= %v (the spec slept that long), got %v", sleep, finished.Duration)
	}
	if len(rep.suiteFinished) != 1 {
		t.Fatalf("expected one SuiteFinished, got %+v", rep.suiteFinished)
	}
	if end := rep.suiteFinished[0]; end.Duration < sleep {
		t.Fatalf("expected suite Duration >= %v (it wraps the slow spec), got %v", sleep, end.Duration)
	}
}

// TestParallelStepWithObserverRecordsPerGoroutineDuration proves each ItParallel spec's Duration is
// measured against its own goroutine's started event, not shared or reused across specs.
//
// This only asserts a lower bound on the deliberately slow spec and that every spec's reported
// SpecFinished.SpecStartEvent.Time matches its own SpecStarted.Time exactly — never an upper bound
// on the fast siblings. An upper-bound/exact-timing assertion on "fast1"/"fast2" (e.g. Duration <
// sleep) would be flaky under a loaded CI runner, where even an empty spec body can take longer than
// a short sleep to get scheduled between SpecStarted and SpecFinished.
func TestParallelStepWithObserverRecordsPerGoroutineDuration(t *testing.T) {
	const sleep = 20 * time.Millisecond
	rep := &recordingReporter{}
	obs := &reporterObserver{rep: rep}
	backend := &capturingBackend{}
	ctx := &Context{backend: backend, execObserver: obs}

	run := parallelStep([]step{
		runAll([]step{func(*Context) {}}),
		runAll([]step{func(*Context) { time.Sleep(sleep) }}),
		runAll([]step{func(*Context) {}}),
	}, []string{"fast1", "slow", "fast2"}, nil)
	run(ctx)

	if len(rep.specStarted) != 3 || len(rep.specFinished) != 3 {
		t.Fatalf("expected three SpecStarted/SpecFinished, got started=%+v finished=%+v", rep.specStarted, rep.specFinished)
	}
	startByName := map[string]time.Time{}
	for _, e := range rep.specStarted {
		startByName[e.Name] = e.Time
	}
	for _, e := range rep.specFinished {
		start, ok := startByName[e.Name]
		if !ok || !e.Time.Equal(start) {
			t.Fatalf("expected %q's SpecFinished.SpecStartEvent to be its own SpecStarted event, got start=%v finish=%v", e.Name, start, e.Time)
		}
		if e.Duration < 0 {
			t.Fatalf("expected non-negative Duration for %q, got %v", e.Name, e.Duration)
		}
		if e.Name == "slow" && e.Duration < sleep {
			t.Fatalf("expected 'slow' Duration >= %v, got %v", sleep, e.Duration)
		}
	}
}

// TestRunnerNilReporterMatchesReporterFailFastSemantics proves the ctx.failed-per-spec-reset fix
// (required unconditionally, not gated on Reporter != nil — see runSpecsRecovered) does not change
// FailFast behavior: with FailFast set, a failing spec still stops every later spec, whether or not
// a Reporter is attached.
func TestRunnerNilReporterMatchesReporterFailFastSemantics(t *testing.T) {
	t.Run("without reporter", func(t *testing.T) {
		var ranSecond bool
		prog := BuildProgram(func(b *Builder) {
			b.Describe("Suite", func() {
				b.It("first", func(ctx *Context) { ctx.recordFailure() })
				b.It("second", func(ctx *Context) { ranSecond = true })
			})
		})
		r := NewRunner(prog)
		r.FailFast = true
		r.Run(t)
		if ranSecond {
			t.Error("expected FailFast to stop before the second spec")
		}
	})

	t.Run("with reporter", func(t *testing.T) {
		var ranSecond bool
		prog := BuildProgram(func(b *Builder) {
			b.Describe("Suite", func() {
				b.It("first", func(ctx *Context) { ctx.recordFailure() })
				b.It("second", func(ctx *Context) { ranSecond = true })
			})
		})
		rep := &recordingReporter{}
		r := NewRunnerWithReporter(prog, "Suite", rep)
		r.FailFast = true
		r.Run(t)
		if ranSecond {
			t.Error("expected FailFast to stop before the second spec")
		}
		if len(rep.specFinished) != 1 || rep.specFinished[0].Name != "first" || !rep.specFinished[0].Failed {
			t.Fatalf("expected only 'first' to be reported, as failed, got %+v", rep.specFinished)
		}
	})
}

// TestParallelStepWithObserverReportsEachSpecIndividually proves each real ItParallel spec is
// reported individually (one SpecStarted/SpecFinished pair per spec, not one for the whole opaque
// group), with Failed derived from that spec's own result (results[i], the same per-index tracking
// reportFailures already uses), not the group's aggregate ctx.failed. Exercises parallelStep
// directly against a capturingBackend, like program_test.go's other parallelStep tests, so a
// (deliberately) failing spec here never reaches a real *testing.T's Fatalf/Goexit.
func TestParallelStepWithObserverReportsEachSpecIndividually(t *testing.T) {
	rep := &recordingReporter{}
	obs := &reporterObserver{rep: rep}
	backend := &capturingBackend{}
	ctx := &Context{backend: backend, execObserver: obs}

	run := parallelStep([]step{
		runAll([]step{func(*Context) {}}),
		runAll([]step{func(ctx *Context) { ctx.backend.Error("boom") }}),
		runAll([]step{func(*Context) {}}),
	}, []string{"p1", "p2", "p3"}, nil)
	run(ctx)

	if !backend.failed {
		t.Fatal("expected the group's failure (from p2) to still be reported on the outer backend")
	}
	if len(rep.specStarted) != 3 {
		t.Fatalf("expected three SpecStarted (one per real ItParallel spec), got %+v", rep.specStarted)
	}
	if len(rep.specFinished) != 3 {
		t.Fatalf("expected three SpecFinished, got %+v", rep.specFinished)
	}
	var names []string
	byName := map[string]bool{}
	for _, e := range rep.specFinished {
		names = append(names, e.Name)
		byName[e.Name] = e.Failed
	}
	sort.Strings(names)
	if got := names; len(got) != 3 || got[0] != "p1" || got[1] != "p2" || got[2] != "p3" {
		t.Fatalf("expected p1/p2/p3 each reported once, got %v", names)
	}
	if byName["p1"] || byName["p3"] {
		t.Errorf("expected p1/p3 to be reported as not failed, got %+v", rep.specFinished)
	}
	if !byName["p2"] {
		t.Errorf("expected p2 to be reported as failed, got %+v", rep.specFinished)
	}
	if obs.total != 3 || obs.failed != 1 {
		t.Fatalf("expected observer to tally total=3 failed=1, got total=%d failed=%d", obs.total, obs.failed)
	}
}

// TestParallelStepWithObserverPassesFailureStringStraightToMessage proves ItParallel's Message
// comes straight from parallelBackend's already-recorded results[i] string (part of #13-C's scope:
// ItParallel passes that string through as-is, with no reinterpretation), and that Output stays
// empty — parallelBackend has no separable output source to draw one from.
func TestParallelStepWithObserverPassesFailureStringStraightToMessage(t *testing.T) {
	rep := &recordingReporter{}
	obs := &reporterObserver{rep: rep}
	backend := &capturingBackend{}
	ctx := &Context{backend: backend, execObserver: obs}

	run := parallelStep([]step{
		runAll([]step{func(*Context) {}}),
		runAll([]step{func(ctx *Context) { ctx.backend.Errorf("boom: %d", 42) }}),
	}, []string{"p1", "p2"}, nil)
	run(ctx)

	if len(rep.specFinished) != 2 {
		t.Fatalf("expected two SpecFinished, got %+v", rep.specFinished)
	}
	byName := map[string]report.SpecResultEvent{}
	for _, e := range rep.specFinished {
		byName[e.Name] = e
	}
	if got := byName["p1"]; got.Failed || got.Message != "" || got.Output != "" {
		t.Errorf("expected p1 (passing) to have empty Message/Output, got %+v", got)
	}
	if got := byName["p2"]; !got.Failed || got.Message != "boom: 42" {
		t.Errorf("expected p2's Message to be exactly the recorded failure string %q, got %+v", "boom: 42", got)
	}
	if got := byName["p2"]; got.Output != "" {
		t.Errorf("expected p2's Output to stay empty (no separable output source in ItParallel), got %q", got.Output)
	}
}

// TestRunSpecsRecoveredReportsPanicMessageAndOutput proves a recovered spec panic (the one sequential
// failure path that still reaches specFinished today — see specResult's doc comment) reports Message
// as the short "panic: value" summary and Output as the panic's stack trace, built once and reused
// for both the backend's Errorf call and the reported event — never reconstructed a second time.
func TestRunSpecsRecoveredReportsPanicMessageAndOutput(t *testing.T) {
	rep := &recordingReporter{}
	obs := &reporterObserver{rep: rep}
	backend := &controlledBackend{}
	ctx := &Context{backend: backend, execObserver: obs}
	g := &group{
		specs: []step{func(*Context) { panic("boom") }},
		names: []string{"panics"},
	}

	runSpecsRecovered(ctx, g)

	if len(rep.specFinished) != 1 {
		t.Fatalf("expected one SpecFinished, got %+v", rep.specFinished)
	}
	got := rep.specFinished[0]
	if !got.Failed {
		t.Fatalf("expected the panicking spec to be reported as failed, got %+v", got)
	}
	if got.Message != "panic: boom" {
		t.Errorf("expected Message %q, got %q", "panic: boom", got.Message)
	}
	if !strings.Contains(got.Output, "runStepRecovered") {
		t.Errorf("expected Output to contain a stack trace naming runStepRecovered, got %q", got.Output)
	}
	if len(backend.errors) != 1 || backend.errors[0] != got.Message+"\n"+got.Output {
		t.Fatalf("expected backend.Errorf to receive exactly message+\"\\n\"+output, got %v", backend.errors)
	}
}

// TestRunnerWithReporterItParallelSharesRunnerObserver proves a passing ItParallel group, run
// through the real Runner (not parallelStep directly), reports through the same observer instance
// as the surrounding sequential specs — so SuiteEndEvent.TotalSpecs counts both kinds together.
func TestRunnerWithReporterItParallelSharesRunnerObserver(t *testing.T) {
	rep := &recordingReporter{}
	prog := BuildProgram(func(b *Builder) {
		b.Describe("Suite", func() {
			b.It("sequential", func(ctx *Context) {})
			b.ItParallel("p1", func(ctx *Context) {})
			b.ItParallel("p2", func(ctx *Context) {})
		})
	})

	NewRunnerWithReporter(prog, "Suite", rep).Run(t)

	if len(rep.specStarted) != 3 {
		t.Fatalf("expected three SpecStarted (1 sequential + 2 parallel), got %+v", rep.specStarted)
	}
	if len(rep.suiteFinished) != 1 {
		t.Fatalf("expected one SuiteFinished, got %+v", rep.suiteFinished)
	}
	end := rep.suiteFinished[0]
	if end.TotalSpecs != 3 || end.FailedSpecs != 0 {
		t.Fatalf("expected TotalSpecs=3 FailedSpecs=0, got %+v", end)
	}
}

// TestRunnerWithReporterItParallelReportsPath proves an ItParallel spec's declared enclosing scopes
// reach SpecStartEvent.Path too (#112): parallelStep reports each real spec itself, on its own
// goroutine, through the same code path as a sequential spec's Path.
func TestRunnerWithReporterItParallelReportsPath(t *testing.T) {
	rep := &recordingReporter{}
	prog := BuildProgram(func(b *Builder) {
		b.Describe("Suite", func() {
			b.Describe("When parallel", func() {
				b.ItParallel("p1", func(ctx *Context) {})
			})
		})
	})

	NewRunnerWithReporter(prog, "Suite", rep).Run(t)

	if len(rep.specStarted) != 1 {
		t.Fatalf("expected one SpecStarted, got %+v", rep.specStarted)
	}
	wantPath := []string{"Suite", "When parallel", "p1"}
	if got := rep.specStarted[0].Path; strings.Join(got, "|") != strings.Join(wantPath, "|") {
		t.Fatalf("expected declared Path %q, got %q", wantPath, got)
	}
}

// TestRunShardWithReporterCountsOnlyShardSpecs proves SuiteEndEvent.TotalSpecs for a shard reflects
// only the specs that shard actually executed, not the full Program's total — the reporter describes
// the concrete execution that happened, not the unsharded program.
func TestRunShardWithReporterCountsOnlyShardSpecs(t *testing.T) {
	// Built as four distinct groups directly (not via the Builder), so this test isn't at the mercy
	// of finalize()'s hook-based coalescing merging same-hookKey Describes (all hookless here) into
	// one group — RunShard shards by group index, so it needs four real groups to split across.
	noop := func(*Context) {}
	prog := &Program{Groups: []group{
		{specs: []step{noop}, names: []string{"a"}},
		{specs: []step{noop}, names: []string{"b"}},
		{specs: []step{noop}, names: []string{"c"}},
		{specs: []step{noop}, names: []string{"d"}},
	}}

	rep := &recordingReporter{}
	RunShardWithReporter(prog, t, 0, 2, "Shard0", rep)

	if len(rep.suiteFinished) != 1 {
		t.Fatalf("expected one SuiteFinished, got %+v", rep.suiteFinished)
	}
	end := rep.suiteFinished[0]
	if end.TotalSpecs != 2 {
		t.Fatalf("expected shard 0 of 2 to report TotalSpecs=2 (half of 4), got %+v", end)
	}
	if len(rep.specStarted) != 2 {
		t.Fatalf("expected two SpecStarted for this shard, got %+v", rep.specStarted)
	}
}

// TestRunnerNilReporterNoObserver proves a Runner without a Reporter never installs an
// execObserver, so it takes zero extra branches beyond the ordinary nil check — same invariant as
// #12-A: no reporter, no observable difference in execution.
func TestRunnerNilReporterNoObserver(t *testing.T) {
	var sawObserver bool
	prog := BuildProgram(func(b *Builder) {
		b.Describe("Suite", func() {
			b.It("spec", func(ctx *Context) { sawObserver = ctx.execObserver != nil })
		})
	})
	NewRunner(prog).Run(t)
	if sawObserver {
		t.Error("expected ctx.execObserver to be nil without a Reporter")
	}
}

// TestRunnerWithReporterReportsSkippedSpec proves a SkipIt spec is reported as its own
// SpecStarted/SpecFinished{Skipped: true, Failed: false} pair, with its name preserved and its body
// never run — and that it's counted in SuiteEndEvent.TotalSpecs/SkippedSpecs alongside real specs.
func TestRunnerWithReporterReportsSkippedSpec(t *testing.T) {
	var ranSkipped bool
	rep := &recordingReporter{}
	prog := BuildProgram(func(b *Builder) {
		b.Describe("Suite", func() {
			b.It("a", func(ctx *Context) {})
			b.SkipIt("b", func(ctx *Context) { ranSkipped = true })
			b.It("c", func(ctx *Context) {})
		})
	})

	NewRunnerWithReporter(prog, "Suite", rep).Run(t)

	if ranSkipped {
		t.Fatal("expected the skipped spec's body to never run")
	}
	if len(rep.specStarted) != 3 || len(rep.specFinished) != 3 {
		t.Fatalf("expected three SpecStarted/SpecFinished (a, b, c), got started=%+v finished=%+v", rep.specStarted, rep.specFinished)
	}
	var skip *report.SpecResultEvent
	for i := range rep.specFinished {
		if rep.specFinished[i].Name == "b" {
			skip = &rep.specFinished[i]
		}
	}
	if skip == nil {
		t.Fatalf("expected a SpecFinished named %q, got %+v", "b", rep.specFinished)
	}
	if !skip.Skipped {
		t.Errorf("expected Skipped=true for %q, got %+v", "b", *skip)
	}
	if skip.Failed {
		t.Errorf("expected Failed=false for a skipped spec, got %+v", *skip)
	}
	if skip.Duration != 0 {
		t.Errorf("expected Duration=0 for a skipped spec (no body ran), got %v", skip.Duration)
	}
	if len(rep.suiteFinished) != 1 {
		t.Fatalf("expected one SuiteFinished, got %+v", rep.suiteFinished)
	}
	end := rep.suiteFinished[0]
	if end.TotalSpecs != 3 || end.FailedSpecs != 0 || end.SkippedSpecs != 1 {
		t.Fatalf("expected TotalSpecs=3 (a,b,c) FailedSpecs=0 SkippedSpecs=1, got %+v", end)
	}
}

// TestRunnerWithReporterReportsSkippedSpecPath proves a SkipIt spec's declared enclosing scopes
// reach SpecStartEvent.Path too (#112) — a compile-time skip is still a declared spec, and its
// identity should be no less complete than a spec that actually ran.
func TestRunnerWithReporterReportsSkippedSpecPath(t *testing.T) {
	rep := &recordingReporter{}
	prog := BuildProgram(func(b *Builder) {
		b.Describe("Suite", func() {
			b.Describe("When nested", func() {
				b.SkipIt("skipped", func(ctx *Context) {})
			})
		})
	})

	NewRunnerWithReporter(prog, "Suite", rep).Run(t)

	var got *report.SpecStartEvent
	for i := range rep.specStarted {
		if rep.specStarted[i].Name == "skipped" {
			got = &rep.specStarted[i]
		}
	}
	if got == nil {
		t.Fatalf("expected a SpecStarted named %q, got %+v", "skipped", rep.specStarted)
	}
	wantPath := []string{"Suite", "When nested", "skipped"}
	if strings.Join(got.Path, "|") != strings.Join(wantPath, "|") {
		t.Fatalf("expected declared Path %q, got %q", wantPath, got.Path)
	}
}

// TestRunnerWithReporterCountsMixedPassFailSkip proves TotalSpecs/FailedSpecs/SkippedSpecs combine
// correctly when a suite mixes a passing, a failing, and a skipped spec — TotalSpecs is the sum of
// all three categories, not just executed (passed+failed) specs.
func TestRunnerWithReporterCountsMixedPassFailSkip(t *testing.T) {
	rep := &recordingReporter{}
	prog := BuildProgram(func(b *Builder) {
		b.Describe("Suite", func() {
			b.It("passes", func(ctx *Context) {})
			b.It("fails", func(ctx *Context) { ctx.recordFailure() })
			b.SkipIt("skipped", func(ctx *Context) {})
		})
	})

	NewRunnerWithReporter(prog, "Suite", rep).Run(t)

	if len(rep.suiteFinished) != 1 {
		t.Fatalf("expected one SuiteFinished, got %+v", rep.suiteFinished)
	}
	end := rep.suiteFinished[0]
	if end.TotalSpecs != 3 || end.FailedSpecs != 1 || end.SkippedSpecs != 1 {
		t.Fatalf("expected TotalSpecs=3 FailedSpecs=1 SkippedSpecs=1, got %+v", end)
	}
}

// TestRunnerWithReporterReportsMultipleSkips proves several SkipIt specs in the same suite are each
// reported individually, by name, not collapsed into one event or one count.
func TestRunnerWithReporterReportsMultipleSkips(t *testing.T) {
	rep := &recordingReporter{}
	prog := BuildProgram(func(b *Builder) {
		b.Describe("Suite", func() {
			b.SkipIt("skip1", func(ctx *Context) {})
			b.It("runs", func(ctx *Context) {})
			b.SkipIt("skip2", func(ctx *Context) {})
			b.SkipIt("skip3", func(ctx *Context) {})
		})
	})

	NewRunnerWithReporter(prog, "Suite", rep).Run(t)

	var skipNames []string
	for _, e := range rep.specFinished {
		if e.Skipped {
			skipNames = append(skipNames, e.Name)
		}
	}
	sort.Strings(skipNames)
	want := []string{"skip1", "skip2", "skip3"}
	if len(skipNames) != len(want) {
		t.Fatalf("expected skipped names %v, got %v (all finished: %+v)", want, skipNames, rep.specFinished)
	}
	for i := range want {
		if skipNames[i] != want[i] {
			t.Fatalf("expected skipped names %v, got %v", want, skipNames)
		}
	}
	end := rep.suiteFinished[0]
	if end.TotalSpecs != 4 || end.SkippedSpecs != 3 {
		t.Fatalf("expected TotalSpecs=4 SkippedSpecs=3, got %+v", end)
	}
}

// TestRunShardWithReporterCountsOnlyShardSkips proves a shard's SuiteEndEvent.SkippedSpecs (and
// reported SpecFinished{Skipped:true} events) reflect only the skips in groups assigned to that
// shard, not every skip in the unsharded Program — RunShard/RunShardWithReporter shard whole groups
// (see scheduler.go), and a group's skipped names travel with it like any other group data, so this
// is the same "shard describes only what it ran" contract TestRunShardWithReporterCountsOnlyShardSpecs
// already pins for real specs, extended to skips.
func TestRunShardWithReporterCountsOnlyShardSkips(t *testing.T) {
	// shardCount == len(Groups) so shard i gets exactly group i (gi%shardCount==shardIndex), keeping
	// which skip lands on which shard unambiguous.
	prog := &Program{Groups: []group{
		{skipped: []string{"skip-a"}},
		{specs: []step{func(*Context) {}}, names: []string{"b"}},
		{skipped: []string{"skip-c"}},
		{specs: []step{func(*Context) {}}, names: []string{"d"}},
	}}

	rep := &recordingReporter{}
	RunShardWithReporter(prog, t, 0, 4, "Shard0", rep)

	if len(rep.specFinished) != 1 || rep.specFinished[0].Name != "skip-a" || !rep.specFinished[0].Skipped {
		t.Fatalf("expected shard 0 to report exactly skip-a as skipped, got %+v", rep.specFinished)
	}
	end := rep.suiteFinished[0]
	if end.TotalSpecs != 1 || end.SkippedSpecs != 1 {
		t.Fatalf("expected shard 0 TotalSpecs=1 SkippedSpecs=1 (only skip-a's group), got %+v", end)
	}

	rep2 := &recordingReporter{}
	RunShardWithReporter(prog, t, 2, 4, "Shard2", rep2)

	if len(rep2.specFinished) != 1 || rep2.specFinished[0].Name != "skip-c" || !rep2.specFinished[0].Skipped {
		t.Fatalf("expected shard 2 to report exactly skip-c as skipped, got %+v", rep2.specFinished)
	}
	end2 := rep2.suiteFinished[0]
	if end2.TotalSpecs != 1 || end2.SkippedSpecs != 1 {
		t.Fatalf("expected shard 2 TotalSpecs=1 SkippedSpecs=1 (only skip-c's group), got %+v", end2)
	}

	rep3 := &recordingReporter{}
	RunShardWithReporter(prog, t, 1, 4, "Shard1", rep3)

	if len(rep3.specFinished) != 1 || rep3.specFinished[0].Skipped {
		t.Fatalf("expected shard 1 (group b) to report zero skips, got %+v", rep3.specFinished)
	}
	end3 := rep3.suiteFinished[0]
	if end3.SkippedSpecs != 0 {
		t.Fatalf("expected shard 1 SkippedSpecs=0, got %+v", end3)
	}
}

var _ report.EventReporter = (*recordingReporter)(nil)
