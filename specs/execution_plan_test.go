package specs

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

type controlledBackend struct {
	failNow bool
	errors  []string
}

func (b *controlledBackend) Helper()               {}
func (b *controlledBackend) FailNow()              { b.failNow = true; panic(isolatedCaseAbort{}) }
func (b *controlledBackend) Fatal(args ...any)     { b.FailNow() }
func (b *controlledBackend) Fatalf(string, ...any) { b.FailNow() }
func (b *controlledBackend) Error(args ...any)     { b.errors = append(b.errors, fmt.Sprint(args...)) }
func (b *controlledBackend) Errorf(format string, args ...any) {
	b.errors = append(b.errors, fmt.Sprintf(format, args...))
}
func (b *controlledBackend) Log(...any)                   {}
func (b *controlledBackend) Logf(string, ...any)          {}
func (b *controlledBackend) Name() string                 { return "controlled" }
func (b *controlledBackend) Cleanup(func())               {}
func (b *controlledBackend) Run(string, func(testing.TB)) {}

func TestGeneratedCaseLifecycle(t *testing.T) {
	t.Run("pass resets context and reverses after hooks", func(t *testing.T) {
		backend := &controlledBackend{}
		var order []string
		result := runIsolatedCase(backend, "case-1", []Instruction{
			{Code: OpBeforeHook, Fn: func(ctx *Context) { order = append(order, "before") }},
			{Code: OpBody, Fn: func(ctx *Context) { order = append(order, "body") }},
			{Code: OpAfterHook, Fn: func(ctx *Context) { order = append(order, "after-inner") }},
			{Code: OpAfterHook, Fn: func(ctx *Context) { order = append(order, "after-outer") }},
		}, PathValues{}, nil)
		if got, want := fmt.Sprint(order), "[before body after-inner after-outer]"; got != want {
			t.Fatalf("hook order = %s, want %s", got, want)
		}
		if result.Failed || result.Panic != nil {
			t.Fatalf("result = %#v, want successful case", result)
		}
		if !result.ContextReset {
			t.Fatalf("context was not reset after case")
		}
	})

	t.Run("nonfatal assertion completes after hooks", func(t *testing.T) {
		backend := &controlledBackend{}
		var afterRuns int
		result := runIsolatedCase(backend, "case-1", []Instruction{
			{Code: OpBody, Fn: func(ctx *Context) { ctx.Expect(false).ToEqual(true) }},
			{Code: OpAfterHook, Fn: func(*Context) { afterRuns++ }},
		}, PathValues{}, nil)
		if !result.Failed || afterRuns != 1 {
			t.Fatalf("result = %#v, errors = %v, after runs = %d", result, backend.errors, afterRuns)
		}
	})

	t.Run("fatal preserves attribution and runs after once", func(t *testing.T) {
		backend := &controlledBackend{}
		var bodyCompleted, afterRuns bool
		result := runIsolatedCase(backend, "case-1", []Instruction{
			{Code: OpBody, Fn: func(ctx *Context) { ctx.backend.FailNow(); bodyCompleted = true }},
			{Code: OpAfterHook, Fn: func(*Context) { afterRuns = true }},
		}, PathValues{}, nil)
		if !result.Failed || !backend.failNow || bodyCompleted || !afterRuns || result.Panic != nil {
			t.Fatalf("result = %#v, failNow = %t, body completed = %t, after ran = %t", result, backend.failNow, bodyCompleted, afterRuns)
		}
	})

	t.Run("panic is retained and runs after once", func(t *testing.T) {
		backend := &controlledBackend{}
		var afterRuns int
		result := runIsolatedCase(backend, "case-1", []Instruction{
			{Code: OpBody, Fn: func(*Context) { panic("body panic") }},
			{Code: OpAfterHook, Fn: func(*Context) { afterRuns++ }},
		}, PathValues{}, nil)
		if got, want := result.Panic, any("body panic"); got != want || afterRuns != 1 {
			t.Fatalf("panic = %#v, after runs = %d", got, afterRuns)
		}
	})

	t.Run("shrink probe retains original values deterministically", func(t *testing.T) {
		path := PathValues{values: []any{7}, present: []bool{true}, index: map[string]int{"value": 0}}
		program := []Instruction{{Code: OpBody, Fn: func(*Context) {}}, {Code: OpAfterHook, Fn: func(*Context) {}}}
		first := runIsolatedCase(&controlledBackend{}, "case-1", program, path, nil)
		path.values[0] = 9
		second := runIsolatedCase(&controlledBackend{}, "case-1", program, path, nil)
		if first.Path.Int("value") != 7 || second.Path.Int("value") != 9 || first.Failed != second.Failed || first.Panic != second.Panic {
			t.Fatalf("first = %#v, second = %#v", first, second)
		}
	})
}

func TestOrdinaryItKeepsGoTestSemantics(t *testing.T) {
	var order []string
	Describe(t, "ordinary", func(spec *Spec) {
		spec.BeforeEach(func(*Context) { order = append(order, "before") })
		spec.AfterEach(func(*Context) { order = append(order, "after") })
		spec.It("runs", func(*Context) { order = append(order, "body") })
	})
	if got, want := fmt.Sprint(order), "[before body after]"; got != want {
		t.Fatalf("ordinary It order = %s, want %s", got, want)
	}
}

func TestRunExecutionStopsGeneratedCasesAtFirstFailure(t *testing.T) {
	gen := newPathGenerator([]PathVar{{Name: "value", Values: []any{1, 2, 3}}}, nil, 0, 0, false, 0, 0, 0)
	var seen []int
	plan := &ExecutionPlan{
		Instructions: []Instruction{{Code: OpBody, Fn: func(ctx *Context) {
			value := ctx.Path().Int("value")
			seen = append(seen, value)
			if value == 2 {
				ctx.Expect(false).ToEqual(true)
			}
		}}},
		ProgramStart: []int{0}, ProgramLen: []int{1}, PathGens: []*PathGenerator{gen},
	}
	backend := &controlledBackend{}
	result := runExecution(backend, nil, plan, 0)
	if got, want := fmt.Sprint(seen), "[1 2]"; got != want {
		t.Fatalf("executed generated values = %s, want %s", got, want)
	}
	if result.Terminal != proposalTerminalFirstFailure || result.Attempts != 2 || result.Accepted != 2 {
		t.Fatalf("terminal result = %+v, want first failure after two accepted attempts", result)
	}
}

func TestRunExecutionUsesExplicitBoundedControllerConfig(t *testing.T) {
	gen := newPathGenerator([]PathVar{{Name: "value", Values: []any{1, 2, 3}}}, nil, 0, 0, false, 0, 0, 0)
	plan := &ExecutionPlan{
		Instructions: []Instruction{{Code: OpBody, Fn: func(*Context) {}}},
		ProgramStart: []int{0}, ProgramLen: []int{1}, PathGens: []*PathGenerator{gen},
	}
	result := runExecution(&controlledBackend{}, nil, plan, 0)
	if result.Terminal != proposalTerminalAttemptsBudget || result.Attempts != 3 || result.Accepted != 3 || result.Rejections != 0 {
		t.Fatalf("terminal result = %+v, want explicit bounded execution of all generated paths", result)
	}
}

func TestRunExecutionHonorsCanceledAndDeadlineContexts(t *testing.T) {
	plan := &ExecutionPlan{
		Instructions: []Instruction{{Code: OpBody, Fn: func(*Context) { t.Fatal("body must not run") }}},
		ProgramStart: []int{0}, ProgramLen: []int{1}, PathGens: []*PathGenerator{newPathGenerator([]PathVar{{Name: "value", Values: []any{1}}}, nil, 0, 0, false, 0, 0, 0)},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result := runExecutionContext(ctx, &controlledBackend{}, nil, plan, 0); result.Terminal != proposalTerminalCanceled {
		t.Fatalf("canceled result = %+v, want canceled terminal", result)
	}
	deadline, deadlineCancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer deadlineCancel()
	if result := runExecutionContext(deadline, &controlledBackend{}, nil, plan, 0); result.Terminal != proposalTerminalDeadline {
		t.Fatalf("deadline result = %+v, want deadline terminal", result)
	}
}

func TestDescribePathsDispatchHonorsCancellationAndDeadline(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	deadline, deadlineCancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer deadlineCancel()

	for _, tc := range []struct {
		name     string
		ctx      context.Context
		terminal proposalTerminal
	}{
		{name: "canceled", ctx: canceled, terminal: proposalTerminalCanceled},
		{name: "deadline", ctx: deadline, terminal: proposalTerminalDeadline},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bodyRan := false
			results := describeWithCompilerContext(t, tc.ctx, "Paths", nil, func(s *Spec) {
				s.Paths(func(pb *PathBuilder) { pb.Int("value", []int{1}) }).It("case", func(*Context) {
					bodyRan = true
				})
			})
			if bodyRan || len(results) != 1 || results[0].Terminal != tc.terminal {
				t.Fatalf("body ran = %t, results = %+v, want terminal %d", bodyRan, results, tc.terminal)
			}
		})
	}
}

// TestRunExecutionContextCartesianDoesNotMaterializeUnderCancellation would fail against the old
// ForEach-into-slice materialization: that approach generated every candidate (running filters
// for all of them) before the controller ever got a chance to observe an already-canceled ctx.
func TestRunExecutionContextCartesianDoesNotMaterializeUnderCancellation(t *testing.T) {
	var considered int
	gen := newPathGenerator([]PathVar{{Name: "value", rangeSpec: &intRange{min: 0, max: 999}}},
		[]PathFilter{func(PathValues) bool { considered++; return true }}, 0, 0, false, 0, 0, 0)
	plan := &ExecutionPlan{
		Instructions: []Instruction{{Code: OpBody, Fn: func(*Context) { t.Fatal("body must not run") }}},
		ProgramStart: []int{0}, ProgramLen: []int{1}, PathGens: []*PathGenerator{gen},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := runExecutionContext(ctx, &controlledBackend{}, nil, plan, 0)
	if result.Terminal != proposalTerminalCanceled {
		t.Fatalf("terminal = %v, want canceled", result.Terminal)
	}
	if considered != 0 {
		t.Fatalf("candidates considered before the controller observed cancellation = %d, want 0", considered)
	}
}

// TestRunExecutionContextCartesianStopsGeneratingOnCancelMidRun proves cancellation stops
// generation immediately rather than only stopping dispatch of an already-generated candidate:
// canceling from inside the first executed case must prevent a second candidate from ever being
// proposed.
func TestRunExecutionContextCartesianStopsGeneratingOnCancelMidRun(t *testing.T) {
	var considered, executed int
	ctx, cancel := context.WithCancel(context.Background())
	gen := newPathGenerator([]PathVar{{Name: "value", rangeSpec: &intRange{min: 0, max: 999}}},
		[]PathFilter{func(PathValues) bool { considered++; return true }}, 0, 0, false, 0, 0, 0)
	plan := &ExecutionPlan{
		Instructions: []Instruction{{Code: OpBody, Fn: func(*Context) { executed++; cancel() }}},
		ProgramStart: []int{0}, ProgramLen: []int{1}, PathGens: []*PathGenerator{gen},
	}
	result := runExecutionContext(ctx, &controlledBackend{}, nil, plan, 0)
	if result.Terminal != proposalTerminalCanceled {
		t.Fatalf("terminal = %v, want canceled", result.Terminal)
	}
	if executed != 1 {
		t.Fatalf("executed = %d, want exactly 1 (canceled from inside the first case)", executed)
	}
	if considered != 1 {
		t.Fatalf("candidates considered = %d, want 1 — generation must not run ahead of execution", considered)
	}
}

// TestRunExecutionContextSampleDoesNotMaterializeUnderCancellation is Sample's counterpart to
// TestRunExecutionContextCartesianDoesNotMaterializeUnderCancellation: proves runExecutionContext
// no longer routes SamplingMode through the ForEach/[]PathValues fallback, which would have
// generated every sample (and run every filter) before the controller ever observed cancellation.
func TestRunExecutionContextSampleDoesNotMaterializeUnderCancellation(t *testing.T) {
	var considered int
	gen := newPathGenerator([]PathVar{{Name: "value", rangeSpec: &intRange{min: 0, max: 999}}},
		[]PathFilter{func(PathValues) bool { considered++; return true }}, 5, 0, false, 0, 0, 0)
	plan := &ExecutionPlan{
		Instructions: []Instruction{{Code: OpBody, Fn: func(*Context) { t.Fatal("body must not run") }}},
		ProgramStart: []int{0}, ProgramLen: []int{1}, PathGens: []*PathGenerator{gen},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := runExecutionContext(ctx, &controlledBackend{}, nil, plan, 0)
	if result.Terminal != proposalTerminalCanceled {
		t.Fatalf("terminal = %v, want canceled", result.Terminal)
	}
	if considered != 0 {
		t.Fatalf("candidates considered before the controller observed cancellation = %d, want 0", considered)
	}
}

// TestRunExecutionContextSampleStopsGeneratingOnCancelMidRun is Sample's counterpart to
// TestRunExecutionContextCartesianStopsGeneratingOnCancelMidRun.
func TestRunExecutionContextSampleStopsGeneratingOnCancelMidRun(t *testing.T) {
	var considered, executed int
	ctx, cancel := context.WithCancel(context.Background())
	gen := newPathGenerator([]PathVar{{Name: "value", rangeSpec: &intRange{min: 0, max: 999}}},
		[]PathFilter{func(PathValues) bool { considered++; return true }}, 5, 0, false, 0, 0, 0)
	plan := &ExecutionPlan{
		Instructions: []Instruction{{Code: OpBody, Fn: func(*Context) { executed++; cancel() }}},
		ProgramStart: []int{0}, ProgramLen: []int{1}, PathGens: []*PathGenerator{gen},
	}
	result := runExecutionContext(ctx, &controlledBackend{}, nil, plan, 0)
	if result.Terminal != proposalTerminalCanceled {
		t.Fatalf("terminal = %v, want canceled", result.Terminal)
	}
	if executed != 1 {
		t.Fatalf("executed = %d, want exactly 1 (canceled from inside the first case)", executed)
	}
	if considered != 1 {
		t.Fatalf("candidates considered = %d, want 1 — generation must not run ahead of execution", considered)
	}
}

// TestRunExecutionContextExploreDoesNotMaterializeUnderCancellation is plain Explore's counterpart
// to TestRunExecutionContextCartesianDoesNotMaterializeUnderCancellation: proves runExecutionContext
// no longer routes strategyPlain ExplorationGuided through the ForEach/[]PathValues fallback, which
// would have generated every iteration (and run every filter) before the controller ever observed
// cancellation.
func TestRunExecutionContextExploreDoesNotMaterializeUnderCancellation(t *testing.T) {
	var considered int
	gen := newPathGenerator([]PathVar{{Name: "value", rangeSpec: &intRange{min: 0, max: 999}}},
		[]PathFilter{func(PathValues) bool { considered++; return true }}, 0, 0, false, 5, 0, 0)
	plan := &ExecutionPlan{
		Instructions: []Instruction{{Code: OpBody, Fn: func(*Context) { t.Fatal("body must not run") }}},
		ProgramStart: []int{0}, ProgramLen: []int{1}, PathGens: []*PathGenerator{gen},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := runExecutionContext(ctx, &controlledBackend{}, nil, plan, 0)
	if result.Terminal != proposalTerminalCanceled {
		t.Fatalf("terminal = %v, want canceled", result.Terminal)
	}
	if considered != 0 {
		t.Fatalf("candidates considered before the controller observed cancellation = %d, want 0", considered)
	}
}

// TestRunExecutionContextExploreStopsGeneratingOnCancelMidRun is plain Explore's counterpart to
// TestRunExecutionContextCartesianStopsGeneratingOnCancelMidRun. considered == 2 (not 1) after the
// first case: nextExplore's lazy corpus seed step and its first loop candidate each check the
// filter once, both on this first call to next() — see nextExplore's doc comment.
func TestRunExecutionContextExploreStopsGeneratingOnCancelMidRun(t *testing.T) {
	var considered, executed int
	ctx, cancel := context.WithCancel(context.Background())
	gen := newPathGenerator([]PathVar{{Name: "value", rangeSpec: &intRange{min: 0, max: 999}}},
		[]PathFilter{func(PathValues) bool { considered++; return true }}, 0, 0, false, 5, 0, 0)
	plan := &ExecutionPlan{
		Instructions: []Instruction{{Code: OpBody, Fn: func(*Context) { executed++; cancel() }}},
		ProgramStart: []int{0}, ProgramLen: []int{1}, PathGens: []*PathGenerator{gen},
	}
	result := runExecutionContext(ctx, &controlledBackend{}, nil, plan, 0)
	if result.Terminal != proposalTerminalCanceled {
		t.Fatalf("terminal = %v, want canceled", result.Terminal)
	}
	if executed != 1 {
		t.Fatalf("executed = %d, want exactly 1 (canceled from inside the first case)", executed)
	}
	if considered != 2 {
		t.Fatalf("candidates considered = %d, want 2 (seed + first candidate) — generation must not run ahead of execution", considered)
	}
}

// TestRunExecutionContextCoverageDoesNotMaterializeUnderCancellation is ExploreCoverage's
// counterpart to TestRunExecutionContextCartesianDoesNotMaterializeUnderCancellation: proves
// runExecutionContext no longer routes strategyCoverage ExplorationGuided through the ForEach/
// []PathValues fallback, which would have generated every iteration (and run every filter) before
// the controller ever observed cancellation.
func TestRunExecutionContextCoverageDoesNotMaterializeUnderCancellation(t *testing.T) {
	var considered int
	gen := newPathGenerator([]PathVar{{Name: "value", rangeSpec: &intRange{min: 0, max: 999}}},
		[]PathFilter{func(PathValues) bool { considered++; return true }}, 0, 0, false, 0, 5, 0)
	plan := &ExecutionPlan{
		Instructions: []Instruction{{Code: OpBody, Fn: func(*Context) { t.Fatal("body must not run") }}},
		ProgramStart: []int{0}, ProgramLen: []int{1}, PathGens: []*PathGenerator{gen},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := runExecutionContext(ctx, &controlledBackend{}, nil, plan, 0)
	if result.Terminal != proposalTerminalCanceled {
		t.Fatalf("terminal = %v, want canceled", result.Terminal)
	}
	if considered != 0 {
		t.Fatalf("candidates considered before the controller observed cancellation = %d, want 0", considered)
	}
}

// TestRunExecutionContextCoverageStopsGeneratingOnCancelMidRun is ExploreCoverage's counterpart to
// TestRunExecutionContextCartesianStopsGeneratingOnCancelMidRun. Unlike Explore's nextExplore,
// nextGuided has no separate seed step (runGuidedExploration doesn't have one either), so
// considered == 1 here, not 2.
func TestRunExecutionContextCoverageStopsGeneratingOnCancelMidRun(t *testing.T) {
	var considered, executed int
	ctx, cancel := context.WithCancel(context.Background())
	gen := newPathGenerator([]PathVar{{Name: "value", rangeSpec: &intRange{min: 0, max: 999}}},
		[]PathFilter{func(PathValues) bool { considered++; return true }}, 0, 0, false, 0, 5, 0)
	plan := &ExecutionPlan{
		Instructions: []Instruction{{Code: OpBody, Fn: func(*Context) { executed++; cancel() }}},
		ProgramStart: []int{0}, ProgramLen: []int{1}, PathGens: []*PathGenerator{gen},
	}
	result := runExecutionContext(ctx, &controlledBackend{}, nil, plan, 0)
	if result.Terminal != proposalTerminalCanceled {
		t.Fatalf("terminal = %v, want canceled", result.Terminal)
	}
	if executed != 1 {
		t.Fatalf("executed = %d, want exactly 1 (canceled from inside the first case)", executed)
	}
	if considered != 1 {
		t.Fatalf("candidates considered = %d, want 1 — generation must not run ahead of execution", considered)
	}
}

// TestRunExecutionContextSmartDoesNotMaterializeUnderCancellation is ExploreSmart's counterpart to
// TestRunExecutionContextCartesianDoesNotMaterializeUnderCancellation.
func TestRunExecutionContextSmartDoesNotMaterializeUnderCancellation(t *testing.T) {
	var considered int
	gen := newPathGenerator([]PathVar{{Name: "value", rangeSpec: &intRange{min: 0, max: 999}}},
		[]PathFilter{func(PathValues) bool { considered++; return true }}, 0, 0, false, 0, 0, 5)
	plan := &ExecutionPlan{
		Instructions: []Instruction{{Code: OpBody, Fn: func(*Context) { t.Fatal("body must not run") }}},
		ProgramStart: []int{0}, ProgramLen: []int{1}, PathGens: []*PathGenerator{gen},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := runExecutionContext(ctx, &controlledBackend{}, nil, plan, 0)
	if result.Terminal != proposalTerminalCanceled {
		t.Fatalf("terminal = %v, want canceled", result.Terminal)
	}
	if considered != 0 {
		t.Fatalf("candidates considered before the controller observed cancellation = %d, want 0", considered)
	}
}

// TestRunExecutionContextSmartStopsGeneratingOnCancelMidRun is ExploreSmart's counterpart to
// TestRunExecutionContextCartesianStopsGeneratingOnCancelMidRun.
func TestRunExecutionContextSmartStopsGeneratingOnCancelMidRun(t *testing.T) {
	var considered, executed int
	ctx, cancel := context.WithCancel(context.Background())
	gen := newPathGenerator([]PathVar{{Name: "value", rangeSpec: &intRange{min: 0, max: 999}}},
		[]PathFilter{func(PathValues) bool { considered++; return true }}, 0, 0, false, 0, 0, 5)
	plan := &ExecutionPlan{
		Instructions: []Instruction{{Code: OpBody, Fn: func(*Context) { executed++; cancel() }}},
		ProgramStart: []int{0}, ProgramLen: []int{1}, PathGens: []*PathGenerator{gen},
	}
	result := runExecutionContext(ctx, &controlledBackend{}, nil, plan, 0)
	if result.Terminal != proposalTerminalCanceled {
		t.Fatalf("terminal = %v, want canceled", result.Terminal)
	}
	if executed != 1 {
		t.Fatalf("executed = %d, want exactly 1 (canceled from inside the first case)", executed)
	}
	if considered != 1 {
		t.Fatalf("candidates considered = %d, want 1 — generation must not run ahead of execution", considered)
	}
}

// TestRunExecutionContextCoverageGrowsCorpusFromRealCoverage is #54's end-to-end proof for
// strategyCoverage: real coverage recorded by an assertion running inside the executed case (not
// a synthetic Coverage fed directly into admitFeedback, as in
// TestPathSequenceCoverageCorpusGrowsOnlyAfterAdmitFeedback) must actually flow through
// runIsolatedCase -> ctx.RecordCoverage -> admitFeedback -> CoverageExplorer.Feedback and grow the
// corpus. EqualTo(ctx, v, v) records an edge whose hash depends on v itself (see
// coverageEdgeHash/valueHash), so distinct candidate values are near-certain to look like novel
// coverage — if the wiring were broken (e.g. cov never reaching ctx, or always compared as if
// identical), the corpus would stay at 0 or 1 regardless of how many candidates ran.
func TestRunExecutionContextCoverageGrowsCorpusFromRealCoverage(t *testing.T) {
	const iterations = 20
	gen := newPathGenerator([]PathVar{{Name: "value", rangeSpec: &intRange{min: 0, max: 1000}}},
		nil, 0, 42, true, 0, iterations, 0)
	plan := &ExecutionPlan{
		Instructions: []Instruction{{Code: OpBody, Fn: func(ctx *Context) {
			v := ctx.Path().Int("value")
			EqualTo(ctx, v, v)
		}}},
		ProgramStart: []int{0}, ProgramLen: []int{1}, PathGens: []*PathGenerator{gen},
	}
	runExecutionContext(context.Background(), &controlledBackend{}, nil, plan, 0)
	if got := gen.coverageExplorer.CorpusLen(); got <= 1 {
		t.Fatalf("CoverageExplorer corpus = %d, want > 1 — real per-candidate coverage from the executed assertion should have grown it past the seed", got)
	}
}

// TestRunExecutionContextSmartGrowsCorpusFromRealCoverage is
// TestRunExecutionContextCoverageGrowsCorpusFromRealCoverage's counterpart for strategySmart.
func TestRunExecutionContextSmartGrowsCorpusFromRealCoverage(t *testing.T) {
	const iterations = 20
	gen := newPathGenerator([]PathVar{{Name: "value", rangeSpec: &intRange{min: 0, max: 1000}}},
		nil, 0, 42, true, 0, 0, iterations)
	plan := &ExecutionPlan{
		Instructions: []Instruction{{Code: OpBody, Fn: func(ctx *Context) {
			v := ctx.Path().Int("value")
			EqualTo(ctx, v, v)
		}}},
		ProgramStart: []int{0}, ProgramLen: []int{1}, PathGens: []*PathGenerator{gen},
	}
	runExecutionContext(context.Background(), &controlledBackend{}, nil, plan, 0)
	if got := gen.smartExplorer.CorpusLen(); got <= 1 {
		t.Fatalf("SmartExplorer corpus = %d, want > 1 — real per-candidate coverage from the executed assertion should have grown it past the seed", got)
	}
}

func TestGeneratedFatalUsesRealSubtestBoundary(t *testing.T) {
	if os.Getenv("GO_SPECS_FATAL_BOUNDARY_HELPER") == "1" {
		var afterRuns int
		Describe(t, "fatal boundary", func(s *Spec) {
			s.AfterEach(func(*Context) { afterRuns++ })
			s.Paths(func(pb *PathBuilder) { pb.Int("value", []int{1, 2}) }).It("case", func(ctx *Context) {
				if ctx.Path().Int("value") == 1 {
					ctx.T.Fatal("generated fatal")
				}
				t.Log("second generated case ran")
			})
		})
		t.Logf("controller returned after=%d", afterRuns)
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestGeneratedFatalUsesRealSubtestBoundary$")
	cmd.Env = append(os.Environ(), "GO_SPECS_FATAL_BOUNDARY_HELPER=1")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("fatal generated case unexpectedly passed: %s", output)
	}
	if !strings.Contains(string(output), "controller returned after=1") || strings.Contains(string(output), "second generated case ran") {
		t.Fatalf("subtest isolation output = %s", output)
	}
}

// TestGeneratedFailureOutputNamesTheCandidate proves `go test -v` failure output identifies WHICH
// generated candidate failed (#103). Before candidate naming, every candidate ran as the literal
// subtest "generated" and this same failure read "--- FAIL: .../generated#01", which named neither
// the owning spec nor the value that broke it. Run in a subprocess because the assertion is about
// the real testing package's own output for a genuinely failing subtest.
func TestGeneratedFailureOutputNamesTheCandidate(t *testing.T) {
	if os.Getenv("GO_SPECS_GENERATED_FAILURE_NAME_HELPER") == "1" {
		Describe(t, "billing", func(s *Spec) {
			s.Paths(func(pb *PathBuilder) { pb.Values("tier", []any{"basic", "pro"}) }).
				It("accepts tier", func(ctx *Context) {
					if ctx.Path().Value("tier") == "pro" {
						ctx.T.Errorf("pro is not supported yet")
					}
				})
		})
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestGeneratedFailureOutputNamesTheCandidate$", "-test.v")
	cmd.Env = append(os.Environ(), "GO_SPECS_GENERATED_FAILURE_NAME_HELPER=1")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("failing generated candidate unexpectedly passed: %s", output)
	}
	text := string(output)
	if strings.Contains(text, "generated#") {
		t.Fatalf("output still uses the old undifferentiated 'generated' subtest name: %s", text)
	}
	// The whole identity, in one line: spec breadcrumb, executed ordinal, and the value that failed.
	const wantFailure = "--- FAIL: TestGeneratedFailureOutputNamesTheCandidate/billing/accepts_tier/case-2-tier=pro"
	if !strings.Contains(text, wantFailure) {
		t.Fatalf("expected failure output to contain %q, got: %s", wantFailure, text)
	}
	if !strings.Contains(text, "case-1-tier=basic") {
		t.Fatalf("expected the passing candidate to be named too, got: %s", text)
	}
}

// TestGeneratedCandidateIsSelectableWithRun proves the naming contract's practical payoff: a name
// copied verbatim out of `go test -v` works as a `go test -run` pattern and selects exactly ONE
// Cartesian candidate (#103). It deliberately discovers the name from the child's own -v output
// rather than hardcoding it, so the test fails if the printed name ever stops being a usable
// pattern — which is the property candidateRuneAllowed exists to protect.
func TestGeneratedCandidateIsSelectableWithRun(t *testing.T) {
	if os.Getenv("GO_SPECS_RUN_SELECTION_HELPER") == "1" {
		Describe(t, "catalog", func(s *Spec) {
			s.Paths(func(pb *PathBuilder) { pb.Values("tier", []any{"basic", "pro", "gold"}) }).
				It("prices tier", func(ctx *Context) {
					fmt.Printf("BODY RAN tier=%v\n", ctx.Path().Value("tier"))
				})
		})
		return
	}

	run := func(extra ...string) string {
		args := append([]string{"-test.run=^TestGeneratedCandidateIsSelectableWithRun$", "-test.v"}, extra...)
		cmd := exec.Command(os.Args[0], args...)
		cmd.Env = append(os.Environ(), "GO_SPECS_RUN_SELECTION_HELPER=1")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("child run %v failed: %v\n%s", extra, err, output)
		}
		return string(output)
	}

	// 1. Discover the candidate names the way a human does: read them off -v output.
	full := run()
	var proName string
	for _, line := range strings.Split(full, "\n") {
		name, ok := strings.CutPrefix(strings.TrimSpace(line), "=== RUN   ")
		if ok && strings.HasSuffix(name, "tier=pro") {
			proName = name
		}
	}
	if proName == "" {
		t.Fatalf("no subtest named for the tier=pro candidate in:\n%s", full)
	}
	if got := strings.Count(full, "BODY RAN"); got != 3 {
		t.Fatalf("expected all 3 candidates to run unfiltered, got %d:\n%s", got, full)
	}

	// 2. Anchor each "/"-separated element — exactly what `go test -run` matches element-wise — and
	//    re-run. The pattern is built from the discovered name with no rewriting at all.
	elements := strings.Split(proName, "/")
	for idx, element := range elements {
		elements[idx] = "^" + element + "$"
	}
	pattern := strings.Join(elements, "/")
	selected := run("-test.run=" + pattern)

	if got := strings.Count(selected, "BODY RAN"); got != 1 {
		t.Fatalf("expected exactly 1 candidate body to run under %q, got %d:\n%s", pattern, got, selected)
	}
	if !strings.Contains(selected, "BODY RAN tier=pro") {
		t.Fatalf("expected the tier=pro candidate to be the one selected, got:\n%s", selected)
	}
}
