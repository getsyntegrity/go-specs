package assert

// Eventually and Consistently poll a callback until a matcher's verdict settles (#356).
//
// Both helpers call the callback again on every attempt: they never poll a value captured once. The
// matcher is evaluated with Evaluate, exactly once per attempt, and an attempt's failure is only
// remembered, never reported. Only the final verdict is returned, as a PollResult; the specs package
// turns a failing result into a spec failure (ctx.Eventually, ctx.Consistently).
//
// Semantics, in one place:
//
//   - Timing. The first attempt runs immediately. A timer for the interval starts before each
//     attempt, so the interval is measured start to start: a callback slower than the interval is
//     followed at once by the next attempt. One timer for the timeout starts before the first
//     attempt. When the interval and the timeout are due together, the timeout wins.
//   - Cancellation. The context is checked before every attempt (an already-cancelled context runs
//     none) and while waiting. Eventually then fails; so does Consistently, because the condition was
//     not observed for the whole interval.
//   - Blocking callbacks. The callback runs on the calling goroutine and is never interrupted: the
//     helper starts no goroutines, and Go cannot forcibly stop arbitrary code. A callback that blocks
//     blocks the helper, so a callback that can wait must watch the context passed to WithContext (or
//     its own deadline). The timeout is checked between attempts: an attempt that finishes after the
//     deadline still counts (a late match passes Eventually), then no further attempt runs.
//   - Panics. A panic in the callback or in the matcher is recovered and is the final verdict
//     (TerminatedPanic) with the panic value and stack; polling stops. runtime.Goexit (for example
//     t.FailNow inside the callback) is not recoverable and unwinds through the helper, which
//     releases its timers with defer.

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"strings"
	"time"
)

// Defaults used when the matching option is not given.
const (
	DefaultPollTimeout  = time.Second
	DefaultPollInterval = 10 * time.Millisecond
)

// Termination says why a poll ended.
type Termination int

const (
	// TerminatedMatched: Eventually saw the matcher pass.
	TerminatedMatched Termination = iota + 1
	// TerminatedHeld: Consistently saw the matcher pass on every attempt until the timeout.
	TerminatedHeld
	// TerminatedTimeout: Eventually reached its timeout without a pass.
	TerminatedTimeout
	// TerminatedMismatch: Consistently saw the matcher fail.
	TerminatedMismatch
	// TerminatedCancelled: the context ended the poll.
	TerminatedCancelled
	// TerminatedPanic: the callback or the matcher panicked.
	TerminatedPanic
	// TerminatedInvalid: the arguments or options were invalid; nothing ran.
	TerminatedInvalid
)

func (t Termination) String() string {
	switch t {
	case TerminatedMatched:
		return "matched"
	case TerminatedHeld:
		return "held"
	case TerminatedTimeout:
		return "timeout"
	case TerminatedMismatch:
		return "mismatch"
	case TerminatedCancelled:
		return "cancelled"
	case TerminatedPanic:
		return "panic"
	case TerminatedInvalid:
		return "invalid"
	}
	return fmt.Sprintf("Termination(%d)", int(t))
}

// PollResult is the final verdict of Eventually or Consistently.
type PollResult struct {
	// Passed is the verdict: Eventually matched, or Consistently held.
	Passed bool
	// Termination says why the poll ended.
	Termination Termination
	// Attempts counts callback invocations, including one that panicked.
	Attempts int
	// Elapsed is how long the poll ran, by the poll's Clock.
	Elapsed time.Duration
	// Last is the last value the callback returned; nil when no attempt returned one.
	Last any
	// Failure is the matcher's failure message for Last; empty when the last evaluation matched.
	Failure string
	// Err is the context error (TerminatedCancelled) or the joined option errors (TerminatedInvalid).
	Err error
	// Panic and Stack describe a recovered panic (TerminatedPanic).
	Panic any
	Stack string

	mode string
}

// Message renders a failing result for a spec report. It is empty for a passing result.
func (r PollResult) Message() string {
	if r.Passed {
		return ""
	}
	var b strings.Builder
	attempts := plural(r.Attempts, "attempt")
	switch r.Termination {
	case TerminatedInvalid:
		fmt.Fprintf(&b, "%s: invalid arguments: %s", r.mode, userValue(r.Err))
		return b.String()
	case TerminatedTimeout:
		fmt.Fprintf(&b, "%s: timed out after %v (%s)", r.mode, r.Elapsed, attempts)
	case TerminatedMismatch:
		fmt.Fprintf(&b, "%s: condition failed on attempt %d after %v", r.mode, r.Attempts, r.Elapsed)
	case TerminatedCancelled:
		fmt.Fprintf(&b, "%s: cancelled after %v (%s): %s", r.mode, r.Elapsed, attempts, userValue(r.Err))
	case TerminatedPanic:
		fmt.Fprintf(&b, "%s: callback or matcher panicked on attempt %d after %v: %s\n%s", r.mode, r.Attempts, r.Elapsed, userValue(r.Panic), r.Stack)
	default:
		fmt.Fprintf(&b, "%s: failed (%s) after %v (%s)", r.mode, r.Termination, r.Elapsed, attempts)
	}
	if r.Attempts > 0 && (r.Termination != TerminatedPanic || r.Last != nil) {
		fmt.Fprintf(&b, "\n  last observed: %s", renderObserved(r.Last))
	}
	if r.Failure != "" {
		fmt.Fprintf(&b, "\n  matcher failure: %s", r.Failure)
	}
	return b.String()
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// PollOption configures Eventually or Consistently. An invalid option makes the poll return a
// TerminatedInvalid result without running the callback.
type PollOption func(*pollConfig)

type pollConfig struct {
	timeout, interval time.Duration
	ctx               context.Context
	clock             Clock
	errs              []error
}

// WithTimeout bounds the poll: how long Eventually keeps trying, and how long Consistently must
// hold. It must be positive. Default DefaultPollTimeout.
func WithTimeout(d time.Duration) PollOption {
	return func(c *pollConfig) {
		if d <= 0 {
			c.errs = append(c.errs, fmt.Errorf("timeout must be positive, got %v", d))
			return
		}
		c.timeout = d
	}
}

// WithInterval sets the time between attempts. It must be positive. Default DefaultPollInterval.
func WithInterval(d time.Duration) PollOption {
	return func(c *pollConfig) {
		if d <= 0 {
			c.errs = append(c.errs, fmt.Errorf("interval must be positive, got %v", d))
			return
		}
		c.interval = d
	}
}

// WithContext ends the poll when ctx is done. The helper does not interrupt a running callback; see
// the package notes above. It must not be nil. Default context.Background().
func WithContext(ctx context.Context) PollOption {
	return func(c *pollConfig) {
		if ctx == nil {
			c.errs = append(c.errs, errors.New("context must not be nil"))
			return
		}
		c.ctx = ctx
	}
}

// WithClock replaces the real clock, typically with a ManualClock. It must not be nil.
func WithClock(clock Clock) PollOption {
	return func(c *pollConfig) {
		if clock == nil {
			c.errs = append(c.errs, errors.New("clock must not be nil"))
			return
		}
		c.clock = clock
	}
}

// Eventually calls fn until m matches its result, the timeout elapses, the context ends, or fn or m
// panics. It passes on the first attempt that matches.
func Eventually(fn func() any, m Matcher, opts ...PollOption) PollResult {
	return poll("Eventually", true, fn, m, opts)
}

// Consistently calls fn for the whole timeout and passes only if m matches on every attempt. It fails
// on the first attempt that does not match, without waiting out the rest of the interval.
func Consistently(fn func() any, m Matcher, opts ...PollOption) PollResult {
	return poll("Consistently", false, fn, m, opts)
}

func poll(mode string, untilMatch bool, fn func() any, m Matcher, opts []PollOption) PollResult {
	res := PollResult{mode: mode}
	cfg := pollConfig{timeout: DefaultPollTimeout, interval: DefaultPollInterval, ctx: context.Background(), clock: realClock{}}
	for _, opt := range opts {
		if opt == nil {
			cfg.errs = append(cfg.errs, errors.New("option must not be nil"))
			continue
		}
		opt(&cfg)
	}
	if fn == nil {
		cfg.errs = append(cfg.errs, errors.New("callback must not be nil"))
	}
	if isNilMatcher(m) {
		cfg.errs = append(cfg.errs, errors.New("matcher must not be nil"))
	}
	if len(cfg.errs) > 0 {
		res.Termination, res.Err = TerminatedInvalid, errors.Join(cfg.errs...)
		return res
	}

	start := cfg.clock.Now()
	deadline := cfg.clock.NewTimer(cfg.timeout)
	defer deadline.Stop()
	// The attempt timer is released by defer so that runtime.Goexit (t.FailNow inside the callback)
	// cannot leak it. Only the latest timer can still be pending: an earlier one fired to get here.
	var next Timer
	defer func() {
		if next != nil {
			next.Stop()
		}
	}()
	finish := func(t Termination, passed bool) PollResult {
		res.Termination, res.Passed = t, passed
		res.Elapsed = cfg.clock.Now().Sub(start)
		return res
	}
	timedOut := func() PollResult {
		if untilMatch {
			return finish(TerminatedTimeout, false)
		}
		return finish(TerminatedHeld, true)
	}

	for {
		if err := cfg.ctx.Err(); err != nil {
			res.Err = err
			return finish(TerminatedCancelled, false)
		}
		next = cfg.clock.NewTimer(cfg.interval)
		res.Attempts++
		matched, panicked := attempt(fn, m, &res)
		if panicked {
			return finish(TerminatedPanic, false)
		}
		if matched == untilMatch {
			if untilMatch {
				return finish(TerminatedMatched, true)
			}
			return finish(TerminatedMismatch, false)
		}
		select {
		case <-deadline.C():
			return timedOut()
		case <-cfg.ctx.Done():
			res.Err = cfg.ctx.Err()
			return finish(TerminatedCancelled, false)
		case <-next.C():
			select {
			case <-deadline.C():
				return timedOut()
			default:
			}
		}
	}
}

// attempt runs the callback and the matcher once, recording what it saw in res. It reports whether
// the matcher matched, and whether either one panicked.
func attempt(fn func() any, m Matcher, res *PollResult) (matched, panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			res.Panic, res.Stack = r, string(debug.Stack())
			matched, panicked = false, true
		}
	}()
	value := fn()
	res.Last = value
	matched, res.Failure = Evaluate(m, value)
	return matched, false
}
