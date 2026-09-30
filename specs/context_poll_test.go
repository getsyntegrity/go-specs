package specs

// Tests for ctx.Eventually and ctx.Consistently (#356): they turn the final verdict of the assert
// polling helpers into a spec failure, never an intermediate one. Time is a ManualClock advanced from
// inside the polled callback, and cross-task data is synchronized in memory, so no test sleeps.

import (
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// messageBackend is a controlledBackend that also keeps the message of the fatal report.
type messageBackend struct {
	controlledBackend
	msg string
}

func (b *messageBackend) Fatalf(format string, args ...any) {
	b.msg = fmt.Sprintf(format, args...)
	b.FailNow()
}

// runPollBody runs body against a fresh context and returns the failure message ("" when the body
// passed) and the backend's error count.
func runPollBody(t *testing.T, body func(ctx *Context)) (msg string, b *messageBackend) {
	t.Helper()
	b = &messageBackend{}
	ctx := acquireContext(b)
	defer releaseContext(ctx)
	func() {
		defer func() {
			if r := recover(); r != nil {
				if _, ok := r.(isolatedCaseAbort); !ok {
					panic(r)
				}
			}
		}()
		body(ctx)
	}()
	return b.msg, b
}

func TestCtxEventuallyPassesWithoutRecordingAnything(t *testing.T) {
	clock := NewManualClock()
	n := 0
	msg, b := runPollBody(t, func(ctx *Context) {
		ctx.Eventually(func() any { n++; clock.Advance(time.Millisecond); return n }, Equal(3), WithClock(clock), WithInterval(time.Millisecond))
	})
	if msg != "" || b.failNow || len(b.errors) != 0 || n != 3 {
		t.Fatalf("msg=%q failNow=%v errors=%v attempts=%d, want a clean pass after 3 attempts", msg, b.failNow, b.errors, n)
	}
}

func TestCtxEventuallyTimeoutFailsTheSpecOnce(t *testing.T) {
	clock := NewManualClock()
	msg, b := runPollBody(t, func(ctx *Context) {
		ctx.Eventually(func() any { clock.Advance(10 * time.Millisecond); return "pending" }, Equal("ready"),
			WithClock(clock), WithInterval(10*time.Millisecond), WithTimeout(50*time.Millisecond))
	})
	if !b.failNow {
		t.Fatal("a timed-out Eventually must fail the spec")
	}
	for _, want := range []string{"Eventually: timed out after 50ms", "5 attempts", `last observed: "pending"`, "matcher failure:"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q lacks %q", msg, want)
		}
	}
}

func TestCtxConsistentlyFailsOnTheFinalVerdictOnly(t *testing.T) {
	clock := NewManualClock()
	n := 0
	msg, b := runPollBody(t, func(ctx *Context) {
		ctx.Consistently(func() any { n++; clock.Advance(time.Millisecond); return n < 3 }, BeTrue(), WithClock(clock), WithInterval(time.Millisecond))
	})
	if !b.failNow || n != 3 || !strings.Contains(msg, "Consistently: condition failed on attempt 3") {
		t.Fatalf("failNow=%v attempts=%d msg=%q, want a failure on attempt 3", b.failNow, n, msg)
	}

	clock = NewManualClock()
	msg, b = runPollBody(t, func(ctx *Context) {
		ctx.Consistently(func() any { clock.Advance(time.Millisecond); return true }, BeTrue(),
			WithClock(clock), WithInterval(time.Millisecond), WithTimeout(5*time.Millisecond))
	})
	if msg != "" || b.failNow {
		t.Fatalf("a held condition must pass, got failNow=%v msg=%q", b.failNow, msg)
	}
}

func TestCtxPollInvalidOptionFailsTheSpec(t *testing.T) {
	msg, b := runPollBody(t, func(ctx *Context) {
		ctx.Eventually(func() any { return 1 }, Equal(1), WithTimeout(0))
	})
	if !b.failNow || !strings.Contains(msg, "timeout must be positive") {
		t.Fatalf("failNow=%v msg=%q, want a spec failure naming the bad timeout", b.failNow, msg)
	}
}

func TestCtxPollCallbackPanicIsAFailureNotACrash(t *testing.T) {
	msg, b := runPollBody(t, func(ctx *Context) {
		ctx.Eventually(func() any { panic("kaboom") }, Equal(1), WithClock(NewManualClock()))
	})
	if !b.failNow || !strings.Contains(msg, "panicked") || !strings.Contains(msg, "kaboom") {
		t.Fatalf("failNow=%v msg=%q, want the panic reported as the spec failure", b.failNow, msg)
	}
}

func TestCtxPollOnNilContextIsANoOp(t *testing.T) {
	var ctx *Context
	called := false
	ctx.Eventually(func() any { called = true; return 1 }, Equal(1))
	ctx.Consistently(func() any { called = true; return 1 }, Equal(1))
	if called {
		t.Fatal("a nil *Context has no backend to report to, so it must not poll")
	}
}

// TestCtxEventuallyInsideCtxGoWaitsForAnotherTask is the documented use: one task publishes a value
// and another polls for it. The poller's virtual clock is advanced by the callback, and Gosched only
// yields to the publisher; nothing waits on wall-clock time.
func TestCtxEventuallyInsideCtxGoWaitsForAnotherTask(t *testing.T) {
	forEachGoEngine(t, func(t *testing.T, run goRunFn) {
		var ready atomic.Bool
		poll := func(want any, within time.Duration) func(*Context) {
			return func(ctx *Context) {
				clock := NewManualClock()
				ctx.Go(func(c *Context) { ready.Store(true) })
				ctx.Go(func(c *Context) {
					c.Eventually(func() any {
						clock.Advance(time.Millisecond)
						runtime.Gosched()
						return ready.Load()
					}, Equal(want), WithClock(clock), WithInterval(time.Millisecond), WithTimeout(within))
				})
			}
		}
		rep := run(t, false, nil,
			goDeclSpec{"published", poll(true, time.Hour)},
			goDeclSpec{"never", func(ctx *Context) {
				clock := NewManualClock()
				ctx.Go(func(c *Context) {
					c.Eventually(func() any { clock.Advance(time.Millisecond); return false }, BeTrue(),
						WithClock(clock), WithInterval(time.Millisecond), WithTimeout(10*time.Millisecond))
				})
			}},
		)
		got := finishedByName(t, rep)
		if o := classifyOutcome(got["published"]); o != "passed" {
			t.Errorf("published outcome = %q (%+v), want passed", o, got["published"])
		}
		if o := classifyOutcome(got["never"]); o != "failed" || !strings.Contains(got["never"].Message, "Eventually: timed out") {
			t.Errorf("never outcome = %q message %q, want the timeout charged to the spec", o, got["never"].Message)
		}
	})
}

func TestCtxPollLeavesNoGoroutinesBehind(t *testing.T) {
	before := runtime.NumGoroutine()
	for i := 0; i < 10; i++ {
		clock := NewManualClock()
		runPollBody(t, func(ctx *Context) {
			ctx.Eventually(func() any { clock.Advance(time.Second); return 0 }, Equal(1), WithClock(clock))
		})
		runPollBody(t, func(ctx *Context) {
			ctx.Consistently(func() any { clock.Advance(time.Second); return true }, BeTrue(), WithClock(clock))
		})
	}
	if after := runtime.NumGoroutine(); after > before {
		t.Fatalf("goroutines: %d before, %d after", before, after)
	}
}
