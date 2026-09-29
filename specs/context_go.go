// context_go.go implements ctx.Go, the opt-in way to run concurrent assertions inside a spec (#318).
//
// The problem it solves: a *Context is pooled and reused spec to spec, so a goroutine that keeps
// using ctx after its spec ended asserts through whichever spec owns that pointer next — the first
// spec passes, the next one fails with the first one's message and source line. A raw `go`
// statement cannot be fixed transparently: the pointer is the same, and a goroutine has no identity
// the assertion path could check without adding cost to every assertion. So the framework offers a
// second door instead. ctx.Go starts a task that is bound to the spec: the spec waits for it, and
// what it reports is charged to the spec that started it.
//
// How the binding works, in one paragraph. Every ctx.Go call gives its task its own small *Context
// (a "task context"), never the spec's. A task context owns its failure record, so nothing a task
// asserts can touch the spec's state while the spec goroutine is running. When the task ends, its
// outcome is recorded in the spec's goState under a mutex; the spec goroutine folds it into the
// spec's own Context at a settle point, after wg.Wait — a happens-before edge, so folding needs no
// further synchronisation. Settle points are exactly where the engines already finish a spec: after
// the body and before AfterEach hooks (awaitTasks/settleTasks(false)), and once more after the hooks,
// which also closes the spec to new tasks (settleTasks(true)).
//
// Two backends, two reporting routes. When the spec runs on a real testing.TB (a *testing.T subtest),
// the task context shares that TB: it is safe for concurrent use, and reporting straight through it
// keeps testing's own file:line attribution, so the failure names the assertion inside the task. A
// fatal assertion there ends only the task's goroutine (runtime.Goexit), never the spec's. Otherwise
// (fake backends, and the parallelBackend of Builder.ItParallel and RunParallel, none of which are
// concurrency-safe) the task context gets a private goTaskBackend that records the failure with its
// captured location and replays it onto the spec's backend from the spec goroutine when folding.
//
// The cost model: a spec that never calls ctx.Go pays a nil check at each settle point and nothing
// else. goState is allocated on the first ctx.Go of a pooled Context and reused by every later spec
// that lands on it, exactly like the isoRun cache (#244).
package specs

import (
	"fmt"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"testing"
)

// goFinishedMessage is the panic raised when ctx.Go is called on a Context whose spec is over. Like
// #170's released-expectation diagnostic, it fails loudly at the offending call: silently starting
// the task would run it against a Context that is idle in the pool, or already owned by another spec.
const goFinishedMessage = "specs: ctx.Go called after its spec finished. " +
	"ctx.Go starts a task the spec waits for, so it must be called while the spec is running: " +
	"from the It body, a BeforeEach/AfterEach hook, or another ctx.Go task. " +
	"A ctx used by a goroutine launched with a plain `go` statement must not outlive the spec; " +
	"start that work with ctx.Go instead"

const goNilTaskMessage = "specs: ctx.Go requires a non-nil function; pass the task as func(*specs.Context)"

// goState is the per-Context bookkeeping for ctx.Go tasks. It is allocated lazily by the first
// ctx.Go and reused across the specs that share a pooled Context; reset clears it for each spec.
type goState struct {
	// wg counts running tasks. Add is only ever called by the spec goroutine or by a running task
	// (nested ctx.Go), so the counter is never zero while a nested Add races with Wait.
	wg sync.WaitGroup
	// mu guards every field below, all written by task goroutines when they finish.
	mu sync.Mutex
	// rec is the first assertion failure a task recorded (first write wins, like parallelBackend).
	rec failureRecord
	// panicMsg and panicOut are the first task panic, in the wire format of recoverSpecFailure.
	panicMsg, panicOut string
	// panicReported and replayed make folding idempotent: the engines settle more than once per spec.
	panicReported, replayed bool
	// closed is set by the final settle; a ctx.Go after it panics. Atomic because a task may read it
	// while the spec goroutine sets it.
	closed atomic.Bool
}

func (gs *goState) reset() {
	gs.mu.Lock()
	gs.rec = failureRecord{}
	gs.panicMsg, gs.panicOut = "", ""
	gs.panicReported, gs.replayed = false, false
	gs.mu.Unlock()
	gs.closed.Store(false)
}

// goTaskInfo marks a task context and records when its task ended, so a task context retained past
// its task (by a raw goroutine the task itself launched) is refused by ctx.Go instead of leaking.
type goTaskInfo struct{ done atomic.Bool }

// Go runs fn on its own goroutine as part of the current spec and returns immediately. It is the
// supported way to make assertions concurrently:
//
//	ctx.It("checks two services", func(ctx *specs.Context) {
//	    ctx.Go(func(ctx *specs.Context) { ctx.Expect(a.Ping()).ToEqual("pong") })
//	    ctx.Go(func(ctx *specs.Context) { ctx.Expect(b.Ping()).ToEqual("pong") })
//	})
//
// The contract:
//
//   - The spec waits for every task started with ctx.Go before it is finished: before its
//     AfterEach hooks run, before it is reported, and before its Context is reused.
//   - A failed assertion, a ctx.T failure or a panic inside a task is charged to the spec that
//     started it, with the task's own message and source line. A panic is reported like a panic in
//     the spec body (an error, with its stack); a fatal assertion or ctx.T.FailNow ends only that
//     task, not the spec body or the other tasks.
//   - AfterEach hooks run after all of the spec's tasks have finished.
//   - fn receives its own *Context. Use that one inside the task, not the outer ctx.
//   - A task may call ctx.Go itself; the spec waits for those tasks too.
//   - Tasks are never cancelled. A spec whose body fails, or that runs under FailFast, still waits
//     for its running tasks, so a task that blocks forever blocks the spec. FailFast reacts once the
//     spec has finished, not while its tasks are still running. In an ItParallel spec, tasks belong
//     to that spec alone.
//   - Calling ctx.Go after the spec has finished panics with a specs: message, but only until that
//     pooled Context is reused by a later spec. Once reused, a stale handle is indistinguishable from
//     the new spec's own, so a ctx.Go issued through it does not panic and its task is attributed to
//     whichever spec owns the Context at that moment. A task's own *Context is never pooled: ctx.Go
//     on it after its task ended always panics.
//
// This is the only supported way to run concurrent assertions. Retaining a spec's ctx past the end of
// that spec, including in a goroutine launched directly with a `go` statement, is unsupported:
// contexts are reused by later specs, and go-specs does not — and cannot — protect that pattern.
// Start such work with ctx.Go instead.
//
// ctx.Go allocates (the task context, a goroutine and its closure). Specs that never call it pay
// nothing.
func (c *Context) Go(fn func(*Context)) {
	if fn == nil {
		panic(goNilTaskMessage)
	}
	if c == nil || c.backend == nil || (c.goTask != nil && c.goTask.done.Load()) {
		panic(goFinishedMessage)
	}
	gs := c.gs
	if gs == nil {
		// Only a spec's own Context gets here: a task context is created with its state in place.
		gs = &goState{}
		c.gs = gs
	}
	if gs.closed.Load() {
		panic(goFinishedMessage)
	}
	child := &Context{T: c.T, tb: c.tb, failFast: c.failFast, gs: gs, goTask: &goTaskInfo{}}
	if c.tb != nil {
		// A real testing.TB is safe to use from several goroutines and attributes the failure to the
		// task's own line, so the task reports straight through it.
		child.backend = c.backend
	} else {
		child.backend = &goTaskBackend{gs: gs}
	}
	gs.wg.Add(1)
	go gs.run(child, fn)
}

// run executes one task. wg.Done is deferred first so it runs last, after finish has recorded the
// outcome — whether the task returned, panicked, or ended through runtime.Goexit (a fatal assertion
// on a real testing.T).
func (gs *goState) run(child *Context, fn func(*Context)) {
	defer gs.wg.Done()
	defer func() { gs.finish(child, recover()) }()
	fn(child)
}

// finish records a task's outcome. recovered is recover()'s result, passed in because recover only
// works when called by the deferred function itself; the stack is captured here, inside the deferred
// call, so it still shows the panicking frames.
func (gs *goState) finish(child *Context, recovered any) {
	child.goTask.done.Store(true)
	var msg, out string
	if recovered != nil && !isTaskAbort(recovered) {
		msg = fmt.Sprintf("panic: %v", recovered)
		out = string(debug.Stack())
	}
	gs.mu.Lock()
	if msg != "" && gs.panicMsg == "" {
		gs.panicMsg, gs.panicOut = msg, out
	}
	if child.failure.Failed && !gs.rec.Failed {
		gs.rec = child.failure
	}
	gs.mu.Unlock()
}

// isTaskAbort reports whether a recovered value is one of the sentinels a backend panics with to stop
// a fatal assertion — a stop that is already recorded, not a panic to report.
func isTaskAbort(recovered any) bool {
	switch recovered {
	case taskAbort{}, parallelAbort{}:
		return true
	}
	return isExpectedAbort(recovered)
}

// awaitTasks blocks until every task of this spec has finished and folds their failures into c. It
// returns the first task panic's message and stack, without reporting it — for engines that record
// panics themselves (the parallel ones). It never closes the spec to new tasks.
func (c *Context) awaitTasks() (message, output string) {
	if c == nil || c.gs == nil {
		return "", ""
	}
	return c.gs.await(c)
}

// settleTasks is awaitTasks for the sequential engines: a task panic is also reported through
// reportRecoveredPanic — once per panic, however often the engine settles — exactly as a panic in the
// spec body would be, and the returned message/output feed the engine's specResult the same way.
// closeAfter is true at the last settle of a spec: later ctx.Go calls then panic.
//
// The nil check comes first and the rest lives in settle, so the call inlines to a single load and
// branch for a spec that never used ctx.Go.
func (c *Context) settleTasks(closeAfter bool) (message, output string) {
	if c == nil || c.gs == nil {
		return "", ""
	}
	return c.gs.settle(c, closeAfter)
}

func (gs *goState) await(c *Context) (message, output string) {
	gs.wg.Wait()
	return gs.fold(c)
}

func (gs *goState) settle(c *Context, closeAfter bool) (message, output string) {
	message, output = gs.await(c)
	if message != "" {
		gs.mu.Lock()
		first := !gs.panicReported
		gs.panicReported = true
		gs.mu.Unlock()
		if first {
			reportRecoveredPanic(c, message, output)
		}
	}
	if closeAfter {
		gs.closed.Store(true)
	}
	return message, output
}

// recycleTasks clears the ctx.Go bookkeeping so the next spec on this Context starts clean. It is
// called by resetFailure (every spec start) and by the engines that reuse a Context across specs with
// no such reset, right after their last settle; tasks are never running by then.
func (c *Context) recycleTasks() {
	if c != nil && c.gs != nil {
		c.gs.reset()
	}
}

// fold applies the tasks' recorded failure to c, on the spec goroutine after wg.Wait. It is
// idempotent: the failure bit and message are only set if c has not already failed (first failure
// explains the spec), and the replay onto a non-testing backend happens once.
func (gs *goState) fold(c *Context) (message, output string) {
	gs.mu.Lock()
	rec := gs.rec
	replay := rec.Failed && !gs.replayed
	if replay {
		gs.replayed = true
	}
	message, output = gs.panicMsg, gs.panicOut
	gs.mu.Unlock()
	if rec.Failed {
		if !c.failure.Failed {
			c.failure.Failed = true
			c.failure.Message = rec.Message
		}
		// With a real testing.TB the task already reported through it; nothing to replay.
		if replay && c.tb == nil {
			c.replayTaskFailure(rec)
		}
	}
	return message, output
}

// replayTaskFailure delivers a failure a task recorded privately to the spec's own backend. The
// parallelBackend keeps the captured location; any other backend gets the text, location included,
// through Errorf — never Fatalf, which would end the spec goroutine in the middle of its settle point
// and skip the AfterEach hooks that follow.
func (c *Context) replayTaskFailure(rec failureRecord) {
	if pb, ok := c.backend.(*parallelBackend); ok {
		pb.recordFailure(rec)
		return
	}
	b := c.reportingBackend()
	if !liveBackend(b) {
		return
	}
	defer func() { _ = recover() }()
	b.Errorf("%s", rec.text())
}

// settleParallelTasks is the parallel engines' settle point, called from a spec's deferred recovery
// after recoverParallelSpecFailure has classified the body's own outcome. A task's assertion failure
// was folded into results[idx] by awaitTasks; a task panic becomes the spec's failure only if nothing
// failed first, and its stack is returned so the caller can keep it as Output (which is what makes
// the report classify it as an error, not a failure).
func settleParallelTasks(ctx *Context, results *[]failureRecord, idx int) (output string) {
	message, out := ctx.awaitTasks()
	// Worker engines reuse one Context across the specs of a chunk, so recycle rather than close.
	ctx.recycleTasks()
	if message != "" && !(*results)[idx].Failed {
		(*results)[idx] = failureRecord{Failed: true, Message: message}
		return out
	}
	return ""
}

// taskAbort is the sentinel goTaskBackend panics with to end a task on a fatal assertion, mirroring
// parallelAbort: the failure is already recorded, so recovery must not report it a second time.
type taskAbort struct{}

// goTaskBackend is the private backend of a task context whose spec runs without a real testing.TB.
// It records the first failure under gs.mu, with the caller's location captured while the task's stack
// is live (see parallelCallerLocation), and touches nothing of the spec's backend.
type goTaskBackend struct{ gs *goState }

func (b *goTaskBackend) record(msg string) {
	f := failureRecord{Failed: true, Message: msg}
	f.File, f.Line, _ = parallelCallerLocation()
	b.gs.mu.Lock()
	if !b.gs.rec.Failed {
		b.gs.rec = f
	}
	b.gs.mu.Unlock()
}

func (b *goTaskBackend) Helper() {}
func (b *goTaskBackend) FailNow() {
	b.record("fail now")
	panic(taskAbort{})
}
func (b *goTaskBackend) Fatal(args ...any) {
	b.record(fmt.Sprint(args...))
	panic(taskAbort{})
}
func (b *goTaskBackend) Fatalf(format string, args ...any) {
	b.record(fmt.Sprintf(format, args...))
	panic(taskAbort{})
}
func (b *goTaskBackend) Error(args ...any)                 { b.record(fmt.Sprint(args...)) }
func (b *goTaskBackend) Errorf(format string, args ...any) { b.record(fmt.Sprintf(format, args...)) }
func (b *goTaskBackend) Log(...any)                        {}
func (b *goTaskBackend) Logf(string, ...any)               {}
func (b *goTaskBackend) Name() string                      { return "" }
func (b *goTaskBackend) Cleanup(func())                    {}
func (b *goTaskBackend) Run(name string, fn func(testing.TB)) {
	b.Fatalf("t.Run(%q, ...) is not supported inside a ctx.Go task without a real testing.T; the subtest was not run", name)
}
