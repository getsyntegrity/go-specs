// polling_test.go shows Eventually and Consistently: assertions about something that changes over
// time (a background worker, a cache, a queue) without sleeping for a fixed period.
//
//   - Eventually(fn, matcher) calls fn again and again until the matcher passes or the timeout ends.
//     Use it for "this will become true soon".
//   - Consistently(fn, matcher) calls fn for the whole timeout and passes only if the matcher holds
//     on every attempt. Use it for "this must stay true", for example that nothing was sent.
//
// In a spec, call ctx.Eventually or ctx.Consistently. They report one failure at the end, never one
// per attempt. Outside a spec, call assert.Eventually / assert.Consistently, which return a
// PollResult (Passed, Termination, Attempts, Elapsed, Last, Failure) and never fail anything
// themselves. That is what the runnable examples use.
//
// Options: WithTimeout (default 1s), WithInterval (default 10ms), WithContext, WithClock.
//
// Semantics:
//   - fn is called again on every attempt. Pass a function that reads the current value, never a
//     value captured once. The first attempt runs immediately.
//   - The callback runs on the calling goroutine and is never interrupted. A callback that can block
//     must watch the context you give to WithContext. The timeout is checked between attempts.
//   - A cancelled context ends the poll: Eventually fails, and so does Consistently, because the
//     condition was not observed for the whole interval.
//   - A panic in the callback or the matcher is recovered and is the final result (TerminatedPanic).
//   - An invalid option (a non-positive timeout or interval, a nil context or clock) returns
//     TerminatedInvalid without calling fn.
//   - WithClock(assert.NewManualClock()) makes time explicit: the clock moves only when Advance is
//     called, so tests need no sleeping. Advance it from inside the callback, which runs on the
//     polling goroutine, to model time passing during an attempt. The examples below do exactly that.
package examples_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/assert"
	"github.com/getsyntegrity/go-specs/specs"
)

// A callback that becomes true on its third call. Each call advances the manual clock by one interval.
func Example_eventuallyMatches() {
	clock := assert.NewManualClock()
	calls := 0
	res := assert.Eventually(func() any {
		calls++
		clock.Advance(10 * time.Millisecond)
		return calls
	}, assert.Equal(3),
		assert.WithClock(clock), assert.WithTimeout(time.Second), assert.WithInterval(10*time.Millisecond))

	fmt.Println(res.Passed, res.Termination, "attempts:", res.Attempts, "last:", res.Last)
	// Output:
	// true matched attempts: 3 last: 3
}

// A timeout is a failed result that says what was last seen.
func Example_eventuallyTimeout() {
	clock := assert.NewManualClock()
	res := assert.Eventually(func() any {
		clock.Advance(10 * time.Millisecond)
		return "pending"
	}, assert.Equal("done"),
		assert.WithClock(clock), assert.WithTimeout(50*time.Millisecond), assert.WithInterval(10*time.Millisecond))

	fmt.Println(res.Passed, res.Termination)
	fmt.Println(res.Message())
	// Output:
	// false timeout
	// Eventually: timed out after 50ms (5 attempts)
	//   last observed: "pending"
	//   matcher failure: expected pending to equal done
}

func Example_consistentlyHolds() {
	clock := assert.NewManualClock()
	res := assert.Consistently(func() any {
		clock.Advance(10 * time.Millisecond)
		return 0
	}, assert.BeZero(),
		assert.WithClock(clock), assert.WithTimeout(50*time.Millisecond), assert.WithInterval(10*time.Millisecond))

	fmt.Println(res.Passed, res.Termination, "attempts:", res.Attempts)
	// Output:
	// true held attempts: 5
}

// Consistently fails on the first attempt that does not match, without waiting out the rest.
func Example_consistentlyBreaks() {
	clock := assert.NewManualClock()
	calls := 0
	res := assert.Consistently(func() any {
		calls++
		clock.Advance(10 * time.Millisecond)
		return calls
	}, assert.BeLessThan(3),
		assert.WithClock(clock), assert.WithTimeout(time.Second), assert.WithInterval(10*time.Millisecond))

	fmt.Println(res.Passed, res.Termination, "attempts:", res.Attempts)
	fmt.Println(res.Message())
	// Output:
	// false mismatch attempts: 3
	// Consistently: condition failed on attempt 3 after 30ms
	//   last observed: 3
	//   matcher failure: expected 3 to be less than 3
}

// A cancelled context ends the poll. For Consistently that is a failure too.
func Example_pollingCancellation() {
	clock := assert.NewManualClock()
	ctx, cancel := context.WithCancel(context.Background())
	res := assert.Consistently(func() any {
		cancel() // the caller gives up during the first attempt
		return "ok"
	}, assert.Equal("ok"), assert.WithClock(clock), assert.WithContext(ctx))
	fmt.Println(res.Passed, res.Termination, res.Err)

	// An already cancelled context runs no attempt at all.
	res = assert.Eventually(func() any { return "never called" }, assert.Equal("x"),
		assert.WithClock(clock), assert.WithContext(ctx))
	fmt.Println(res.Passed, res.Termination, "attempts:", res.Attempts)
	// Output:
	// false cancelled context canceled
	// false cancelled attempts: 0
}

// A panic in the callback is the final result, with the panic value; the stack is in res.Stack.
func Example_pollingPanic() {
	res := assert.Eventually(func() any { panic("backend exploded") }, assert.Equal(1),
		assert.WithClock(assert.NewManualClock()))
	fmt.Println(res.Passed, res.Termination, res.Panic, "attempts:", res.Attempts)
	// Output:
	// false panic backend exploded attempts: 1
}

// Bad options are reported without calling the callback.
func Example_pollingInvalidOptions() {
	called := false
	res := assert.Eventually(func() any { called = true; return 1 }, assert.Equal(1),
		assert.WithTimeout(0), assert.WithInterval(-time.Second))
	fmt.Println(res.Passed, res.Termination, "called:", called)
	fmt.Println(res.Message())
	// Output:
	// false invalid called: false
	// Eventually: invalid arguments: timeout must be positive, got 0s
	// interval must be positive, got -1s
}

// In a spec: ctx.Eventually and ctx.Consistently fail the spec once, with the final result.
// A ManualClock keeps these two specs instant and deterministic.
func TestPolling_inASpecWithManualClock(t *testing.T) {
	specs.Describe(t, "polling", func(s *specs.Spec) {
		s.It("Eventually waits for a value to appear", func(ctx *specs.Context) {
			clock := specs.NewManualClock()
			attempts := 0
			ctx.Eventually(func() any {
				attempts++
				clock.Advance(10 * time.Millisecond)
				return attempts >= 4
			}, specs.BeTrue(), specs.WithClock(clock), specs.WithTimeout(time.Second), specs.WithInterval(10*time.Millisecond))
			ctx.Expect(attempts).To(specs.Equal(4))
		})
		s.It("Consistently checks that nothing changes", func(ctx *specs.Context) {
			clock := specs.NewManualClock()
			ctx.Consistently(func() any {
				clock.Advance(10 * time.Millisecond)
				return "stable"
			}, specs.Equal("stable"), specs.WithClock(clock), specs.WithTimeout(100*time.Millisecond), specs.WithInterval(10*time.Millisecond))
		})
	})
}

// With the real clock, a background goroutine changes the value and Eventually observes it. The
// poll is bounded by WithTimeout, so a bug ends in a failure rather than a hung test.
func TestPolling_realClockWithBackgroundWork(t *testing.T) {
	specs.Describe(t, "background worker", func(s *specs.Spec) {
		s.It("becomes ready", func(ctx *specs.Context) {
			var ready atomic.Bool
			go func() {
				time.Sleep(20 * time.Millisecond)
				ready.Store(true)
			}()
			ctx.Eventually(func() any { return ready.Load() }, specs.BeTrue(),
				specs.WithTimeout(5*time.Second), specs.WithInterval(5*time.Millisecond))
		})
	})
}

// WithContext ties the poll to a cancellation you control, such as the test's own deadline.
func TestPolling_withContext(t *testing.T) {
	specs.Describe(t, "polling with a context", func(s *specs.Spec) {
		s.It("stops when the context ends", func(ctx *specs.Context) {
			pollCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var value atomic.Int64
			value.Store(9)
			ctx.Eventually(func() any { return value.Load() }, specs.Equal(int64(9)), specs.WithContext(pollCtx))
		})
	})
}
