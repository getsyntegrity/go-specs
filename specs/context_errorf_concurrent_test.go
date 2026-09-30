// context_errorf_concurrent_test.go pins the concurrency guarantees of ctx.Errorf (#357): it is safe
// to call from ctx.Go tasks, from the spec goroutine and from cleanups at the same time, and a failure
// reported on a task Context after its task ended, in particular from a cleanup, still fails the
// owning spec. CI's `go test -race` job is what proves the absence of data races; these tests make
// every engine exercise the concurrent path deterministically.
package specs

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	errorfTasks   = 16
	errorfPerTask = 50
	errorfBody    = 50
)

// errorfMessageOK reports whether msg is one of the messages the storm below can legitimately produce:
// a task's or the body's Errorf, each ending in its own text.
func errorfMessageOK(msg string) bool {
	return strings.Contains(msg, "task ") || strings.Contains(msg, "body ")
}

func TestCtxErrorfIsSafeFromCtxGoTasksRunningWhileTheBodyReportsFailures(t *testing.T) {
	forEachCleanupEngine(t, func(t *testing.T, e cleanupEngine) {
		out := e.run(t, cleanupCase{body: func(ctx *Context) {
			start := make(chan struct{})
			for i := 0; i < errorfTasks; i++ {
				i := i
				ctx.Go(func(*Context) {
					<-start
					for j := 0; j < errorfPerTask; j++ {
						ctx.Errorf("task %d-%d", i, j) // the SPEC's ctx, from a task goroutine
					}
				})
			}
			close(start)
			for j := 0; j < errorfBody; j++ {
				ctx.Errorf("body %d", j) // concurrently, from the spec goroutine
			}
		}})
		if !out.failed {
			t.Fatalf("the case did not fail: %+v", out)
		}
		if !errorfMessageOK(out.message) {
			t.Errorf("message = %q, want one of the Errorf messages", out.message)
		}
		if e.everyReport && out.reports != errorfTasks*errorfPerTask+errorfBody {
			t.Errorf("backend received %d Errorf calls, want %d", out.reports, errorfTasks*errorfPerTask+errorfBody)
		}
	})
}

// A failed assertion recorded on the spec goroutine before the settle point keeps its message: deferred
// Errorf failures are folded only afterwards, and never replace a message already recorded.
func TestCtxErrorfFromTasksNeverReplacesTheBodysAssertionMessage(t *testing.T) {
	forEachCleanupEngine(t, func(t *testing.T, e cleanupEngine) {
		out := e.run(t, cleanupCase{body: func(ctx *Context) {
			for i := 0; i < errorfTasks; i++ {
				i := i
				ctx.Go(func(*Context) {
					for j := 0; j < errorfPerTask; j++ {
						ctx.Errorf("task %d-%d", i, j)
					}
				})
			}
			ctx.Expect(1).ToEqual(2) // fatal on the spec goroutine, while the tasks run
		}})
		if !out.failed {
			t.Fatalf("the case did not fail: %+v", out)
		}
		if strings.Contains(out.message, "task ") {
			t.Errorf("message = %q, the body's assertion failed first and must be the one reported", out.message)
		}
	})
}

// Errorf from many goroutines in a cleanup-only case (no ctx.Go): the Context has cleanup state, so the
// concurrent path applies there too.
func TestCtxErrorfIsSafeFromRawGoroutinesJoinedInACleanup(t *testing.T) {
	forEachCleanupEngine(t, func(t *testing.T, e cleanupEngine) {
		out := e.run(t, cleanupCase{body: func(ctx *Context) {
			ctx.Cleanup(func() {
				done := make(chan struct{})
				for i := 0; i < errorfTasks; i++ {
					i := i
					go func() {
						defer func() { done <- struct{}{} }()
						for j := 0; j < errorfPerTask; j++ {
							ctx.Errorf("task %d-%d", i, j)
						}
					}()
				}
				for i := 0; i < errorfTasks; i++ {
					<-done
				}
			})
		}})
		if !out.failed || !errorfMessageOK(out.message) {
			t.Fatalf("outcome = %+v, want a failed case with one of the task messages", out)
		}
		if e.everyReport && out.reports != errorfTasks*errorfPerTask {
			t.Errorf("backend received %d Errorf calls, want %d", out.reports, errorfTasks*errorfPerTask)
		}
	})
}

// mock.NewController(task) inside ctx.Go registers its Verify on the SPEC's cleanup list but reports
// through the task Context, which is finished by the time the cleanup runs. That report must fail the
// spec on every engine.
func TestCtxErrorfFromACleanupRegisteredByATaskFailsTheSpec(t *testing.T) {
	forEachCleanupEngine(t, func(t *testing.T, e cleanupEngine) {
		out := e.run(t, cleanupCase{body: func(ctx *Context) {
			ctx.Go(func(task *Context) {
				task.Cleanup(func() { task.Errorf("unmet expectation: Save") })
			})
		}})
		if !out.failed {
			t.Fatalf("a failure reported from a task's cleanup did not fail the spec: %+v", out)
		}
		checkFirstMessage(t, e, out, "unmet expectation: Save", "")
	})
}

// Same, with AfterEach in play and a cleanup registered on the spec Context reporting through a
// finished task Context.
func TestCtxErrorfOnAFinishedTaskContextFailsTheSpec(t *testing.T) {
	forEachCleanupEngine(t, func(t *testing.T, e cleanupEngine) {
		out := e.run(t, cleanupCase{body: func(ctx *Context) {
			var saved atomic.Pointer[Context]
			ctx.Go(func(task *Context) { saved.Store(task) })
			deadline := time.Now().Add(5 * time.Second)
			for {
				if task := saved.Load(); task != nil && task.goTask.done.Load() {
					break
				}
				if time.Now().After(deadline) {
					panic("task did not finish")
				}
				time.Sleep(time.Millisecond)
			}
			saved.Load().Errorf("late %s", "report")
		}})
		if !out.failed {
			t.Fatalf("Errorf on a finished task Context did not fail the spec: %+v", out)
		}
		checkFirstMessage(t, e, out, "late report", "")
	})
}

func TestCtxErrorfMessageRankFollowsFoldOrder(t *testing.T) {
	// A single task's Errorf calls are folded in call order, so the first one wins.
	forEachCleanupEngine(t, func(t *testing.T, e cleanupEngine) {
		out := e.run(t, cleanupCase{body: func(ctx *Context) {
			ctx.Go(func(task *Context) {
				for j := 0; j < 5; j++ {
					task.Errorf("%s", fmt.Sprintf("step %d", j))
				}
			})
		}})
		if !out.failed {
			t.Fatalf("the case did not fail: %+v", out)
		}
		checkFirstMessage(t, e, out, "step 0", "step 1")
	})
}
