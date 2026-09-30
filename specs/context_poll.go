package specs

// ctx.Eventually and ctx.Consistently (#356): the spec-facing side of assert.Eventually and
// assert.Consistently. The assert package owns the polling semantics (timing, cancellation, panics,
// the final PollResult; see assert/poll.go). This file only decides what a result means for a spec:
// a passing result records nothing, and a failing one is reported exactly once, through the same
// failf path every built-in assertion uses, so it carries the spec's attribution, honours FailFast,
// and works inside ctx.Go tasks. Attempts in between are never reported.

import (
	"context"
	"time"

	"github.com/getsyntegrity/go-specs/assert"
)

// PollOption configures ctx.Eventually and ctx.Consistently. It is assert.PollOption.
type PollOption = assert.PollOption

// Clock is the time source polling uses. It is assert.Clock.
type Clock = assert.Clock

// ManualClock is a deterministic Clock that only moves when Advance is called. It is assert.ManualClock.
type ManualClock = assert.ManualClock

// NewManualClock returns a ManualClock, for polling tests that must not sleep.
func NewManualClock() *ManualClock { return assert.NewManualClock() }

// WithTimeout bounds a poll: how long Eventually keeps trying, how long Consistently must hold.
// It must be positive; the default is one second.
func WithTimeout(d time.Duration) PollOption { return assert.WithTimeout(d) }

// WithInterval sets the time between attempts. It must be positive; the default is 10ms.
func WithInterval(d time.Duration) PollOption { return assert.WithInterval(d) }

// WithContext ends the poll when ctx is done. A running callback is not interrupted.
func WithContext(ctx context.Context) PollOption { return assert.WithContext(ctx) }

// WithClock replaces the real clock, typically with NewManualClock().
func WithClock(clock Clock) PollOption { return assert.WithClock(clock) }

// Eventually calls fn again on every attempt, without polling a value captured once, until m matches
// what it returns. The spec fails only if the final verdict is a failure: the timeout elapsed, the
// context ended, an option was invalid, or fn or m panicked. The failure reports the last observed
// value, the matcher's failure message, the elapsed time and how many attempts ran.
//
// The first attempt is immediate. fn runs on the calling goroutine and is never interrupted, so a
// callback that can block must watch the context it passes to WithContext; the timeout is checked
// between attempts. See docs/DSL.md for the full contract and the use with ctx.Go.
func (c *Context) Eventually(fn func() any, m Matcher, opts ...PollOption) {
	if c == nil || c.backend == nil {
		return
	}
	if c.tb != nil {
		c.tb.Helper()
	}
	if res := assert.Eventually(fn, m, opts...); !res.Passed {
		c.failf("%s", res.Message())
	}
}

// Consistently calls fn again on every attempt for the whole timeout and passes only if m matches
// every time. The first mismatch fails the spec at once, without waiting out the interval; a
// cancelled context fails it too, because the condition was not observed for the whole interval.
// Timing, panics and blocking callbacks behave as in Eventually.
func (c *Context) Consistently(fn func() any, m Matcher, opts ...PollOption) {
	if c == nil || c.backend == nil {
		return
	}
	if c.tb != nil {
		c.tb.Helper()
	}
	if res := assert.Consistently(fn, m, opts...); !res.Passed {
		c.failf("%s", res.Message())
	}
}
