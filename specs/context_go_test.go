// context_go_test.go pins issue #318's contract for ctx.Go: a spec waits for every task it started,
// failures and panics inside a task are charged to that spec, and AfterEach runs only once the tasks
// are done. It runs each behavior against both engines that hand a *Context to spec bodies: the
// compiled Describe/Spec plan and the Builder/Program runner. Failures are observed through fake
// backends and a recording reporter, so a deliberately failing spec does not fail this test; the one
// property that needs a real *testing.T (the reported source line) runs in a subprocess.
package specs

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/report"
)

type goDeclSpec struct {
	name string
	body func(*Context)
}

// goRunFn declares specs (with an optional AfterEach) on one engine, runs them to completion, and
// returns what the reporter saw. The suite scope is always named "S".
type goRunFn func(t *testing.T, failFast bool, after func(*Context), specs ...goDeclSpec) *recordingReporter

func runCompiledGoSpecs(t *testing.T, failFast bool, after func(*Context), specs ...goDeclSpec) *recordingReporter {
	t.Helper()
	c := newBytecodeCompiler()
	c.PushScope("S")
	s := &Spec{name: "S", compiler: c}
	if after != nil {
		s.AfterEach(after)
	}
	for _, sp := range specs {
		s.It(sp.name, sp.body)
	}
	plan := c.TakePlan()
	rep := &recordingReporter{}
	runPlanSpecsInOrder(&planBackend{}, rep, plan, failFast, nil)
	return rep
}

func runBuilderGoSpecs(t *testing.T, failFast bool, after func(*Context), specs ...goDeclSpec) *recordingReporter {
	t.Helper()
	b := NewBuilder()
	b.Describe("S", func() {
		if after != nil {
			b.AfterEach(after)
		}
		for _, sp := range specs {
			b.It(sp.name, sp.body)
		}
	})
	prog := b.Build()
	rep := &recordingReporter{}
	ctx := acquireContext(&controlledBackend{})
	ctx.execObserver = &reporterObserver{rep: rep}
	ctx.SetFailFast(failFast)
	runGroups(ctx, prog.Groups)
	releaseContext(ctx)
	return rep
}

func forEachGoEngine(t *testing.T, fn func(t *testing.T, run goRunFn)) {
	t.Helper()
	t.Run("Spec", func(t *testing.T) { fn(t, runCompiledGoSpecs) })
	t.Run("Builder", func(t *testing.T) { fn(t, runBuilderGoSpecs) })
}

// goEvents is a mutex-guarded ordered log, written from spec and task goroutines alike.
type goEvents struct {
	mu   sync.Mutex
	list []string
}

func (e *goEvents) add(s string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.list = append(e.list, s)
}

func (e *goEvents) index(s string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i, v := range e.list {
		if v == s {
			return i
		}
	}
	return -1
}

func finishedByName(t *testing.T, rep *recordingReporter) map[string]report.SpecResultEvent {
	t.Helper()
	out := map[string]report.SpecResultEvent{}
	for _, e := range rep.specFinished {
		out[e.Name] = e
	}
	return out
}

func TestCtxGoTaskAssertionFailureFailsItsOwnSpecOnly(t *testing.T) {
	forEachGoEngine(t, func(t *testing.T, run goRunFn) {
		rep := run(t, false, nil,
			goDeclSpec{"direct", func(ctx *Context) { ctx.Expect(1).ToEqual(2) }},
			goDeclSpec{"viaTask", func(ctx *Context) {
				ctx.Go(func(c *Context) { c.Expect(1).ToEqual(2) })
			}},
			goDeclSpec{"next", func(ctx *Context) { ctx.Expect(1).ToEqual(1) }},
		)
		got := finishedByName(t, rep)
		if len(got) != 3 {
			t.Fatalf("expected 3 finished specs, got %+v", rep.specFinished)
		}
		if o := classifyOutcome(got["viaTask"]); o != "failed" {
			t.Fatalf("viaTask outcome = %q, want failed (%+v)", o, got["viaTask"])
		}
		if got["viaTask"].Message == "" || got["viaTask"].Message != got["direct"].Message {
			t.Errorf("viaTask message = %q, want the same text a direct failure reports (%q)",
				got["viaTask"].Message, got["direct"].Message)
		}
		if o := classifyOutcome(got["next"]); o != "passed" {
			t.Errorf("next outcome = %q, want passed: the task's failure must not leak into the next spec", o)
		}
	})
}

func TestCtxGoSpecWaitsForTasksStillRunningWhenTheBodyReturns(t *testing.T) {
	forEachGoEngine(t, func(t *testing.T, run goRunFn) {
		var ev goEvents
		release := make(chan struct{})
		afterStarted := make(chan struct{})
		var once sync.Once
		// The releaser frees the blocked task only once an AfterEach has started (which a spec that
		// did not wait would reach immediately, and which then orders "after" before "task-end" and
		// fails the assertions below). A correct implementation never reaches AfterEach while the task
		// is blocked, so the timeout is only the liveness fallback that lets it proceed.
		go func() {
			select {
			case <-afterStarted:
			case <-time.After(30 * time.Millisecond):
			}
			close(release)
		}()
		rep := run(t, false, func(*Context) {
			ev.add("after")
			once.Do(func() { close(afterStarted) })
		},
			goDeclSpec{"a", func(ctx *Context) {
				ctx.Go(func(c *Context) {
					<-release
					ev.add("task-end")
					c.Expect(1).ToEqual(2)
				})
				ev.add("a-body-returned")
			}},
			goDeclSpec{"b", func(*Context) { ev.add("b-body") }},
		)
		got := finishedByName(t, rep)
		if o := classifyOutcome(got["a"]); o != "failed" {
			t.Errorf("a outcome = %q, want failed: the outcome must reflect the late task", o)
		}
		if o := classifyOutcome(got["b"]); o != "passed" {
			t.Errorf("b outcome = %q, want passed", o)
		}
		taskEnd, firstAfter, bBody := ev.index("task-end"), ev.index("after"), ev.index("b-body")
		if taskEnd < 0 || taskEnd >= firstAfter || taskEnd >= bBody {
			t.Errorf("events = %v: want task-end before AfterEach and before the next spec starts", ev.list)
		}
	})
}

func TestCtxGoAfterEachRunsOnlyAfterEveryTaskFinished(t *testing.T) {
	forEachGoEngine(t, func(t *testing.T, run goRunFn) {
		var done atomic.Int32
		var seen []int32
		rep := run(t, false, func(*Context) { seen = append(seen, done.Load()) },
			goDeclSpec{"a", func(ctx *Context) {
				for i := 0; i < 3; i++ {
					ctx.Go(func(c *Context) {
						// A nested task is awaited too.
						c.Go(func(*Context) { done.Add(1) })
						done.Add(1)
					})
				}
			}},
		)
		if len(seen) != 1 || seen[0] != 6 {
			t.Errorf("AfterEach observed completed tasks %v, want [6]", seen)
		}
		if o := classifyOutcome(finishedByName(t, rep)["a"]); o != "passed" {
			t.Errorf("a outcome = %q, want passed", o)
		}
	})
}

func TestCtxGoTaskPanicIsReportedAsAnErrorOnItsOwnSpec(t *testing.T) {
	forEachGoEngine(t, func(t *testing.T, run goRunFn) {
		rep := run(t, false, nil,
			goDeclSpec{"boom", func(ctx *Context) {
				ctx.Go(func(*Context) { panic("task-boom") })
			}},
			goDeclSpec{"next", func(*Context) {}},
		)
		got := finishedByName(t, rep)
		if o := classifyOutcome(got["boom"]); o != "error" {
			t.Fatalf("boom outcome = %q, want error (%+v)", o, got["boom"])
		}
		if !strings.Contains(got["boom"].Message, "task-boom") {
			t.Errorf("boom message = %q, want the panic value", got["boom"].Message)
		}
		if o := classifyOutcome(got["next"]); o != "passed" {
			t.Errorf("next outcome = %q, want passed", o)
		}
	})
}

func TestCtxGoFailFastStopsAfterASpecWhoseTaskFailed(t *testing.T) {
	forEachGoEngine(t, func(t *testing.T, run goRunFn) {
		var ranNext atomic.Bool
		run(t, true, nil,
			goDeclSpec{"a", func(ctx *Context) {
				ctx.Go(func(c *Context) { c.Expect(1).ToEqual(2) })
			}},
			goDeclSpec{"next", func(*Context) { ranNext.Store(true) }},
		)
		if ranNext.Load() {
			t.Error("the next spec ran, want FailFast to stop after the spec whose task failed")
		}
	})
}

func TestCtxGoAfterTheSpecFinishedPanicsWithAnActionableMessage(t *testing.T) {
	forEachGoEngine(t, func(t *testing.T, run goRunFn) {
		var leaked, leakedTask *Context
		run(t, false, nil, goDeclSpec{"a", func(ctx *Context) {
			leaked = ctx
			ctx.Go(func(c *Context) { leakedTask = c })
		}})
		for name, c := range map[string]*Context{"spec ctx": leaked, "task ctx": leakedTask} {
			func() {
				defer func() {
					msg := fmt.Sprint(recover())
					if !strings.HasPrefix(msg, "specs: ") || !strings.Contains(msg, "ctx.Go") {
						t.Errorf("%s: ctx.Go after the spec finished panicked with %q, want an actionable specs: message naming ctx.Go", name, msg)
					}
				}()
				c.Go(func(*Context) {})
			}()
		}
	})
}

func TestCtxGoRejectsANilTask(t *testing.T) {
	ctx := acquireContext(&controlledBackend{})
	defer releaseContext(ctx)
	defer func() {
		if msg := fmt.Sprint(recover()); !strings.HasPrefix(msg, "specs: ") || !strings.Contains(msg, "nil") {
			t.Errorf("ctx.Go(nil) panicked with %q, want a specs: message about the nil function", msg)
		}
	}()
	ctx.Go(nil)
}

func TestCtxGoBuilderItParallelTasksAreAttributedToTheirOwnSpec(t *testing.T) {
	var seen atomic.Int32
	b := NewBuilder()
	b.Describe("S", func() {
		b.AfterEach(func(*Context) {})
		b.ItParallel("fails", func(ctx *Context) {
			ctx.Go(func(c *Context) { c.Expect(1).ToEqual(2) })
		})
		b.ItParallel("panics", func(ctx *Context) {
			ctx.Go(func(*Context) { panic("par-boom") })
		})
		b.ItParallel("passes", func(ctx *Context) {
			ctx.Go(func(*Context) { seen.Add(1) })
			ctx.Go(func(*Context) { seen.Add(1) })
		})
	})
	rep := &recordingReporter{}
	ctx := acquireContext(&controlledBackend{})
	ctx.execObserver = &reporterObserver{rep: rep}
	runGroups(ctx, b.Build().Groups)
	releaseContext(ctx)

	got := finishedByName(t, rep)
	if o := classifyOutcome(got["fails"]); o != "failed" {
		t.Errorf("fails outcome = %q, want failed (%+v)", o, got["fails"])
	}
	if o := classifyOutcome(got["panics"]); o != "error" || !strings.Contains(got["panics"].Message, "par-boom") {
		t.Errorf("panics outcome = %q message %q, want error naming par-boom", o, got["panics"].Message)
	}
	if o := classifyOutcome(got["passes"]); o != "passed" || seen.Load() != 2 {
		t.Errorf("passes outcome = %q with %d tasks done, want passed and 2", o, seen.Load())
	}
}

// TestCtxGoCostsNothingWhenUnused pins the allocation contract: settling a Context that never used
// ctx.Go is a nil check, not an allocation.
func TestCtxGoCostsNothingWhenUnused(t *testing.T) {
	ctx := acquireContext(&controlledBackend{})
	defer releaseContext(ctx)
	if allocs := testing.AllocsPerRun(100, func() { ctx.settleTasks(true) }); allocs != 0 {
		t.Errorf("settleTasks on a Context that never used ctx.Go allocates %v times, want 0", allocs)
	}
}

// --- real *testing.T attribution (subprocess) ---

const ctxGoHelperEnv = "GO_SPECS_CTXGO_HELPER"

func ctxGoRealTHelperBody(t *testing.T, engine string) {
	taskFails := func(ctx *Context) {
		ctx.Go(func(c *Context) {
			_, _, line, _ := runtime.Caller(0)
			fmt.Printf("ASSERT_LINE=%d\n", line+2) // the assertion is two lines below Caller(0)
			c.Expect(1).ToEqual(2)
		})
	}
	taskUsesT := func(ctx *Context) {
		ctx.Go(func(c *Context) { c.T.Errorf("task-t-error") })
	}
	next := func(*Context) { fmt.Println("NEXT_RAN") }
	switch engine {
	case "spec":
		Describe(t, "suite", func(s *Spec) {
			s.It("taskFails", taskFails)
			s.It("taskUsesT", taskUsesT)
			s.It("nextPasses", next)
		})
	case "builder":
		b := NewBuilder()
		b.Describe("suite", func() {
			b.It("taskFails", taskFails)
			b.It("taskUsesT", taskUsesT)
			b.It("nextPasses", next)
		})
		NewRunner(b.Build()).Run(t)
	}
}

func runCtxGoRealT(t *testing.T, testName, engine string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.v", "-test.run=^"+testName+"$")
	cmd.Env = append(os.Environ(), ctxGoHelperEnv+"="+engine)
	raw, err := cmd.CombinedOutput()
	output := string(raw)
	if err == nil {
		t.Fatalf("expected the run to fail (two specs fail through ctx.Go tasks), got a passing run:\n%s", output)
	}
	if crash := regexp.MustCompile(`(?m)^panic: .*`); crash.MatchString(output) {
		t.Fatalf("a ctx.Go task crashed the test binary:\n%s", output)
	}
	m := regexp.MustCompile(`ASSERT_LINE=(\d+)`).FindStringSubmatch(output)
	if m == nil {
		t.Fatalf("the task never ran:\n%s", output)
	}
	if want := "context_go_test.go:" + m[1] + ": "; !strings.Contains(output, want) {
		t.Errorf("failure is not attributed to the task's assertion line (want %q):\n%s", want, output)
	}
	for _, want := range []struct{ verdict, spec string }{
		{"FAIL", "taskFails"}, {"FAIL", "taskUsesT"}, {"PASS", "nextPasses"},
	} {
		re := regexp.MustCompile(`--- ` + want.verdict + `: ` + testName + `/\S*` + want.spec + ` \(`)
		if !re.MatchString(output) {
			t.Errorf("expected subtest %q to report %s:\n%s", want.spec, want.verdict, output)
		}
	}
	if !strings.Contains(output, "task-t-error") {
		t.Errorf("expected the ctx.T failure raised inside the task in the transcript:\n%s", output)
	}
	if !strings.Contains(output, "NEXT_RAN") {
		t.Errorf("the spec after the failing tasks did not run:\n%s", output)
	}
	if strings.Contains(output, "Log in goroutine after") {
		t.Errorf("a task reported after its spec finished:\n%s", output)
	}
}

func TestCtxGoRealT_Spec(t *testing.T) {
	if os.Getenv(ctxGoHelperEnv) == "spec" {
		ctxGoRealTHelperBody(t, "spec")
		return
	}
	runCtxGoRealT(t, "TestCtxGoRealT_Spec", "spec")
}

func TestCtxGoRealT_Builder(t *testing.T) {
	if os.Getenv(ctxGoHelperEnv) == "builder" {
		ctxGoRealTHelperBody(t, "builder")
		return
	}
	runCtxGoRealT(t, "TestCtxGoRealT_Builder", "builder")
}

// TestCtxGoStaleSpecContextBindsToTheSpecThatOwnsItNow_DocumentedLimitation pins a documented
// LIMITATION, not desired behavior. A spec's *Context is pooled, so once a later spec has reused it the
// framework cannot tell a stale handle (kept by a raw `go` goroutine) from that spec's own. A ctx.Go
// issued through such a handle therefore neither panics nor stays with the spec that leaked it: the task
// is attributed to whichever spec owns the Context at that moment. The panic on ctx.Go after a spec
// finished is only guaranteed until the Context is reused; retaining ctx past its spec is unsupported.
func TestCtxGoStaleSpecContextBindsToTheSpecThatOwnsItNow_DocumentedLimitation(t *testing.T) {
	forEachGoEngine(t, func(t *testing.T, run goRunFn) {
		var leaked *Context
		var reused atomic.Bool
		gate := make(chan struct{})   // closed by b once it is running
		called := make(chan struct{}) // closed by the stale goroutine after its ctx.Go returned
		var panicked atomic.Value
		rep := run(t, false, nil,
			goDeclSpec{"a", func(ctx *Context) {
				leaked = ctx
				go func() {
					<-gate
					defer close(called)
					defer func() {
						if r := recover(); r != nil {
							panicked.Store(fmt.Sprint(r))
						}
					}()
					leaked.Go(func(c *Context) { c.Expect(1).ToEqual(2) })
				}()
			}},
			goDeclSpec{"b", func(ctx *Context) {
				reused.Store(ctx == leaked)
				close(gate)
				<-called
			}},
		)
		if !reused.Load() {
			t.Skip("the engine did not reuse the Context between specs, so there is no stale handle to observe")
		}
		if p := panicked.Load(); p != nil {
			t.Fatalf("ctx.Go through a stale handle panicked (%v); after reuse the Context is indistinguishable from b's own, so the documented limitation is that it does not", p)
		}
		got := finishedByName(t, rep)
		if o := classifyOutcome(got["a"]); o != "passed" {
			t.Errorf("a outcome = %q, want passed: the stale task must not be charged to the spec that leaked ctx", o)
		}
		if o := classifyOutcome(got["b"]); o != "failed" {
			t.Errorf("b outcome = %q, want failed: the stale task is attributed to the spec that owns the Context now", o)
		}
	})
}

// TestCtxGoTaskContextIsNeverReusedSoItKeepsTheStrongGuarantee pins that, unlike a spec's pooled
// Context, a task Context is created per ctx.Go call and never recycled: after its task ended, ctx.Go
// on it panics even once the spec's own Context has been reused by later specs.
func TestCtxGoTaskContextIsNeverReusedSoItKeepsTheStrongGuarantee(t *testing.T) {
	forEachGoEngine(t, func(t *testing.T, run goRunFn) {
		var leakedTask *Context
		var later atomic.Int32
		run(t, false, nil,
			goDeclSpec{"a", func(ctx *Context) { ctx.Go(func(c *Context) { leakedTask = c }) }},
			goDeclSpec{"b", func(ctx *Context) { later.Add(1) }},
			goDeclSpec{"c", func(ctx *Context) { later.Add(1) }},
		)
		if later.Load() != 2 {
			t.Fatalf("later specs ran %d times, want 2", later.Load())
		}
		defer func() {
			msg := fmt.Sprint(recover())
			if !strings.HasPrefix(msg, "specs: ") || !strings.Contains(msg, "ctx.Go") {
				t.Errorf("ctx.Go on an ended task context panicked with %q, want an actionable specs: message", msg)
			}
		}()
		leakedTask.Go(func(*Context) {})
		t.Error("ctx.Go on an ended task context returned, want a panic")
	})
}
