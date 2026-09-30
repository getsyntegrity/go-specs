// context_cleanup_test.go pins ctx.Cleanup of issue #357: every execution engine that hands a
// *Context to a case body must run the registered cleanups when the case ends (after AfterEach and
// after every ctx.Go task settled, last registered first, also after a fatal assertion or a panic).
// The engines run in-process over fake backends; the table (cleanupEngines) is shared with
// context_errorf_test.go. The behavior that needs a real *testing.T runs in a subprocess there.
package specs

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// cleanupCase is one case to run on an engine: a body and, for engines that have the notion, an
// AfterEach hook.
type cleanupCase struct {
	body  func(*Context)
	after func(*Context)
}

// cleanupOutcome is what an engine reported for its one case.
type cleanupOutcome struct {
	failed  bool
	errored bool // reported as a panic (an error with a stack), where the engine can say so
	message string
	// reports is how many Errorf calls the engine's fake backend received (flat engines only).
	reports int
}

type cleanupEngine struct {
	name string
	// afterEach is true when the engine runs a real AfterEach hook after ctx.Go tasks settled.
	afterEach bool
	// classifies is true when the engine's report distinguishes an error (panic) from a failure.
	classifies bool
	// rawMessage is true when message is exactly the failure text, with no "file:line:" decoration.
	rawMessage bool
	// everyReport is true when the engine's fake backend receives every Errorf, so a test can count them.
	everyReport bool
	run         func(t *testing.T, c cleanupCase) cleanupOutcome
}

func outcomeOfEvent(rep *recordingReporter) cleanupOutcome {
	if len(rep.specFinished) != 1 {
		return cleanupOutcome{message: fmt.Sprintf("expected 1 finished spec, got %d", len(rep.specFinished))}
	}
	e := rep.specFinished[0]
	return cleanupOutcome{failed: e.Failed, errored: classifyOutcome(e) == "error", message: e.Message}
}

// errorCollector is the failureReporter RunParallel engines report to.
type errorCollector struct{ msgs []string }

func (e *errorCollector) Helper() {}
func (e *errorCollector) Errorf(format string, args ...any) {
	e.msgs = append(e.msgs, fmt.Sprintf(format, args...))
}

func outcomeOfCollector(e *errorCollector) cleanupOutcome {
	if len(e.msgs) == 0 {
		return cleanupOutcome{}
	}
	return cleanupOutcome{failed: true, message: e.msgs[0]}
}

// outcomeOfContext reads a flat sequential engine's outcome from the Context and its fake backend.
func outcomeOfContext(ctx *Context, be *controlledBackend) cleanupOutcome {
	o := cleanupOutcome{failed: ctx.hasFailed(), message: ctx.failure.Message, reports: len(be.errors)}
	for _, m := range be.errors {
		if strings.Contains(m, "panic") {
			o.errored = true
		}
		if o.message == "" {
			o.message = m
		}
	}
	return o
}

func cleanupEngines() []cleanupEngine {
	builderParallel := func(withAfter bool) func(t *testing.T, c cleanupCase) cleanupOutcome {
		return func(t *testing.T, c cleanupCase) cleanupOutcome {
			b := NewBuilder()
			b.Describe("S", func() {
				if withAfter {
					b.AfterEach(c.after)
				}
				b.ItParallel("case", c.body)
			})
			rep := &recordingReporter{}
			ctx := acquireContext(&controlledBackend{})
			ctx.execObserver = &reporterObserver{rep: rep}
			runGroups(ctx, b.Build().Groups)
			releaseContext(ctx)
			return outcomeOfEvent(rep)
		}
	}
	flat := func(run func(ctx *Context, body func(*Context))) func(t *testing.T, c cleanupCase) cleanupOutcome {
		return func(t *testing.T, c cleanupCase) cleanupOutcome {
			be := &controlledBackend{}
			ctx := acquireContext(be)
			defer releaseContext(ctx)
			run(ctx, c.body)
			return outcomeOfContext(ctx, be)
		}
	}
	return []cleanupEngine{
		{name: "Spec.It", afterEach: true, classifies: true, rawMessage: true, run: func(t *testing.T, c cleanupCase) cleanupOutcome {
			cmp := newBytecodeCompiler()
			cmp.PushScope("S")
			s := &Spec{name: "S", compiler: cmp}
			if c.after != nil {
				s.AfterEach(c.after)
			}
			s.It("case", c.body)
			rep := &recordingReporter{}
			runPlanSpecsInOrder(&controlledBackend{}, rep, cmp.TakePlan(), false, nil)
			return outcomeOfEvent(rep)
		}},
		{name: "Builder.It", afterEach: true, classifies: true, rawMessage: true, run: func(t *testing.T, c cleanupCase) cleanupOutcome {
			b := NewBuilder()
			b.Describe("S", func() {
				if c.after != nil {
					b.AfterEach(c.after)
				}
				b.It("case", c.body)
			})
			rep := &recordingReporter{}
			ctx := acquireContext(&controlledBackend{})
			ctx.execObserver = &reporterObserver{rep: rep}
			runGroups(ctx, b.Build().Groups)
			releaseContext(ctx)
			return outcomeOfEvent(rep)
		}},
		{name: "Builder.ItParallel", afterEach: false, classifies: true, rawMessage: true, run: builderParallel(false)},
		{name: "Builder.ItParallel+AfterEach", afterEach: true, classifies: true, rawMessage: true, run: builderParallel(true)},
		{name: "MinimalRunner.RunParallel", run: func(t *testing.T, c cleanupCase) cleanupOutcome {
			col := &errorCollector{}
			NewMinimalRunnerFromSpecs([]RunSpec{{Name: "case", Fn: c.body}}).RunParallel(col, 1)
			return outcomeOfCollector(col)
		}},
		{name: "MinimalRunner.RunParallelBatched", run: func(t *testing.T, c cleanupCase) cleanupOutcome {
			col := &errorCollector{}
			NewMinimalRunnerFromSpecs([]RunSpec{{Name: "case", Fn: c.body}}).RunParallelBatched(col, 1, 4)
			return outcomeOfCollector(col)
		}},
		{name: "BytecodeRunner.RunParallel", run: func(t *testing.T, c cleanupCase) cleanupOutcome {
			bb := NewBCBuilder(4)
			bb.AddSpec(c.body)
			col := &errorCollector{}
			NewBytecodeRunner(bb.BuildBC()).RunParallel(col, 1)
			return outcomeOfCollector(col)
		}},
		{name: "MinimalRunner.Run", everyReport: true, run: flat(func(ctx *Context, body func(*Context)) {
			runMinimalSpecs(ctx, []RunSpec{{Name: "case", Fn: body}})
		})},
		{name: "BlockRunner.Run", everyReport: true, run: flat(func(ctx *Context, body func(*Context)) {
			runBlocks(ctx, []func(*Context){body}, []specBlock{{start: 0, count: 1}})
		})},
		{name: "BytecodeRunner.Run", everyReport: true, run: flat(func(ctx *Context, body func(*Context)) {
			bb := NewBCBuilder(4)
			bb.AddSpec(body)
			p := bb.BuildBC()
			runBytecodeSequential(ctx, p.Code, p.SpecStarts)
		})},
	}
}

func forEachCleanupEngine(t *testing.T, fn func(t *testing.T, e cleanupEngine)) {
	t.Helper()
	for _, e := range cleanupEngines() {
		t.Run(e.name, func(t *testing.T) { fn(t, e) })
	}
}

func TestCtxCleanupRunsLIFOAfterAfterEachAndAfterCtxGoTasks(t *testing.T) {
	forEachCleanupEngine(t, func(t *testing.T, e cleanupEngine) {
		var ev goEvents
		c := cleanupCase{body: func(ctx *Context) {
			ev.add("body")
			ctx.Cleanup(func() { ev.add("cleanup1") })
			ctx.Cleanup(func() { ev.add("cleanup2") })
			ctx.Go(func(*Context) {
				time.Sleep(20 * time.Millisecond)
				ev.add("task")
			})
		}}
		want := []string{"body", "task", "cleanup2", "cleanup1"}
		if e.afterEach {
			c.after = func(*Context) { ev.add("after") }
			want = []string{"body", "task", "after", "cleanup2", "cleanup1"}
		}
		if out := e.run(t, c); out.failed {
			t.Fatalf("a passing case with cleanups reported a failure: %+v", out)
		}
		if got := fmt.Sprint(ev.list); got != fmt.Sprint(want) {
			t.Errorf("order = %v, want %v", ev.list, want)
		}
	})
}

func TestCtxCleanupRunsWhenTheBodyFailsFatally(t *testing.T) {
	forEachCleanupEngine(t, func(t *testing.T, e cleanupEngine) {
		var ev goEvents
		out := e.run(t, cleanupCase{body: func(ctx *Context) {
			ctx.Cleanup(func() { ev.add("cleanup") })
			ctx.Expect(1).ToEqual(2)
		}})
		if !out.failed {
			t.Errorf("the failing assertion was not reported: %+v", out)
		}
		if got := fmt.Sprint(ev.list); got != "[cleanup]" {
			t.Errorf("cleanup runs = %v, want exactly one", ev.list)
		}
	})
}

func TestCtxCleanupRunsWhenTheBodyPanics(t *testing.T) {
	forEachCleanupEngine(t, func(t *testing.T, e cleanupEngine) {
		var ev goEvents
		out := e.run(t, cleanupCase{body: func(ctx *Context) {
			ctx.Cleanup(func() { ev.add("cleanup") })
			panic("body-boom")
		}})
		if !out.failed || (e.classifies && !out.errored) {
			t.Errorf("the panic was not reported as an error: %+v", out)
		}
		if got := fmt.Sprint(ev.list); got != "[cleanup]" {
			t.Errorf("cleanup runs = %v, want exactly one", ev.list)
		}
	})
}

func TestCtxCleanupPanicIsReportedAsAnErrorOfTheCaseAndTheRestStillRun(t *testing.T) {
	forEachCleanupEngine(t, func(t *testing.T, e cleanupEngine) {
		var ev goEvents
		out := e.run(t, cleanupCase{body: func(ctx *Context) {
			ctx.Cleanup(func() { ev.add("registered-first") })
			ctx.Cleanup(func() { panic("cleanup-boom") })
			ctx.Cleanup(func() { ev.add("registered-last") })
		}})
		if !out.failed {
			t.Fatalf("a panicking cleanup did not fail the case: %+v", out)
		}
		if e.classifies && !out.errored {
			t.Errorf("a panicking cleanup must be reported as an error (panic), got a plain failure: %+v", out)
		}
		if !strings.Contains(out.message, "cleanup-boom") {
			t.Errorf("message %q does not name the cleanup panic", out.message)
		}
		if got := fmt.Sprint(ev.list); got != "[registered-last registered-first]" {
			t.Errorf("cleanups that ran = %v, want both, last registered first", ev.list)
		}
	})
}

func TestCtxCleanupDoesNotLeakIntoTheNextCaseOrAcceptStaleRegistrations(t *testing.T) {
	forEachGoEngine(t, func(t *testing.T, run goRunFn) {
		var ran int
		var leaked *Context
		rep := run(t, false, nil,
			goDeclSpec{"registers", func(ctx *Context) {
				leaked = ctx
				ctx.Cleanup(func() { ran++ })
			}},
			goDeclSpec{"plain", func(ctx *Context) { ctx.Expect(1).ToEqual(1) }},
		)
		if ran != 1 {
			t.Errorf("cleanup ran %d times across two cases, want exactly 1", ran)
		}
		if got := finishedByName(t, rep); classifyOutcome(got["plain"]) != "passed" {
			t.Errorf("the next case was disturbed: %+v", got["plain"])
		}
		func() {
			defer func() {
				r := recover()
				if s, _ := r.(string); !strings.Contains(s, "ctx.Cleanup called after") {
					t.Errorf("Cleanup on a finished case's ctx: recovered %v, want a specs: message", r)
				}
			}()
			leaked.Cleanup(func() { ran++ })
		}()
	})
}

func TestCtxCleanupRejectsANilFunction(t *testing.T) {
	ctx := acquireContext(&controlledBackend{})
	defer releaseContext(ctx)
	defer func() {
		if s, _ := recover().(string); !strings.Contains(s, "ctx.Cleanup requires a non-nil function") {
			t.Errorf("want an actionable specs: message, got %q", s)
		}
	}()
	ctx.Cleanup(nil)
}

func TestCtxCleanupRegisteredFromACtxGoTaskRunsAtTheEndOfTheSpec(t *testing.T) {
	forEachGoEngine(t, func(t *testing.T, run goRunFn) {
		var ev goEvents
		run(t, false, func(*Context) { ev.add("after") },
			goDeclSpec{"task", func(ctx *Context) {
				ctx.Go(func(c *Context) { c.Cleanup(func() { ev.add("task-cleanup") }) })
			}})
		if got := fmt.Sprint(ev.list); got != "[after task-cleanup]" {
			t.Errorf("events = %v, want the task's cleanup after AfterEach", ev.list)
		}
	})
}

// TestCtxCleanupCostsNothingWhenUnused pins the allocation contract: draining a Context that
// never registered a cleanup is a nil check, not an allocation.
func TestCtxCleanupCostsNothingWhenUnused(t *testing.T) {
	ctx := &Context{}
	ctx.Reset(&controlledBackend{})
	if allocs := testing.AllocsPerRun(100, func() { ctx.runCleanups() }); allocs != 0 {
		t.Errorf("runCleanups on a Context that never used Cleanup allocates %v times, want 0", allocs)
	}
	if ctx.gs != nil {
		t.Errorf("a Context that never used Cleanup or Go must not allocate its goState")
	}
}

func TestCtxCleanupWorksOnTestingB(t *testing.T) {
	var bodies, cleanups int
	res := testing.Benchmark(func(b *testing.B) {
		Describe(b, "S", func(s *Spec) {
			s.It("x", func(ctx *Context) {
				bodies++
				ctx.Cleanup(func() { cleanups++ })
			})
		})
		nb := NewBuilder()
		nb.Describe("S", func() {
			nb.It("y", func(ctx *Context) {
				bodies++
				ctx.Cleanup(func() { cleanups++ })
			})
		})
		NewRunner(nb.Build()).Run(b)
	})
	_ = res
	if bodies == 0 || bodies != cleanups {
		t.Errorf("on *testing.B: %d bodies ran but %d cleanups", bodies, cleanups)
	}
}
