package assert

// Tests for Eventually and Consistently (#356). Every test runs on a ManualClock and on one
// goroutine: the callback under test advances the clock itself, so "time passing" is an explicit,
// deterministic step of the test rather than a sleep, and nothing here can race or flake.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

const (
	tick    = 10 * time.Millisecond
	timeout = 100 * time.Millisecond
)

// stepper returns a callback that yields values[i] on attempt i (the last value repeats) and
// advances clock by step after computing it, and a pointer to the attempt counter.
func stepper(clock *ManualClock, step time.Duration, values ...any) (func() any, *int) {
	calls := new(int)
	return func() any {
		i := *calls
		*calls++
		if i >= len(values) {
			i = len(values) - 1
		}
		clock.Advance(step)
		return values[i]
	}, calls
}

func TestEventuallyImmediateSuccess(t *testing.T) {
	clock := NewManualClock()
	fn, calls := stepper(clock, 0, 7)
	res := Eventually(fn, Equal(7), WithClock(clock))
	if !res.Passed || res.Termination != TerminatedMatched || res.Attempts != 1 || *calls != 1 {
		t.Fatalf("got %+v after %d calls, want an immediate match on attempt 1", res, *calls)
	}
	if res.Last != 7 || res.Elapsed != 0 || res.Err != nil {
		t.Fatalf("Last=%v Elapsed=%v Err=%v, want 7, 0, nil", res.Last, res.Elapsed, res.Err)
	}
	if res.Message() != "" {
		t.Fatalf("a passing result has message %q, want none", res.Message())
	}
}

func TestEventuallyDelayedSuccessReevaluatesTheCallback(t *testing.T) {
	clock := NewManualClock()
	fn, calls := stepper(clock, tick, 0, 0, 3)
	res := Eventually(fn, Equal(3), WithClock(clock), WithInterval(tick), WithTimeout(timeout))
	if !res.Passed || res.Attempts != 3 || *calls != 3 {
		t.Fatalf("got %+v, want a pass on the third attempt", res)
	}
	if res.Elapsed != 3*tick {
		t.Fatalf("Elapsed = %v, want %v", res.Elapsed, 3*tick)
	}
}

func TestEventuallyTimeoutReportsLastValueFailureAndElapsed(t *testing.T) {
	clock := NewManualClock()
	fn, calls := stepper(clock, tick, 1, 2)
	res := Eventually(fn, Equal(99), WithClock(clock), WithInterval(tick), WithTimeout(timeout))
	if res.Passed || res.Termination != TerminatedTimeout {
		t.Fatalf("got %+v, want a timeout", res)
	}
	if res.Attempts != *calls || res.Attempts != 10 || res.Elapsed != timeout {
		t.Fatalf("Attempts=%d calls=%d Elapsed=%v, want 10 attempts over %v", res.Attempts, *calls, res.Elapsed, timeout)
	}
	if res.Last != 2 || !strings.Contains(res.Failure, "99") {
		t.Fatalf("Last=%v Failure=%q, want the last observation and its matcher failure", res.Last, res.Failure)
	}
	msg := res.Message()
	for _, want := range []string{"Eventually", "timed out after 100ms", "10 attempts", "last observed: 2", "matcher failure: "} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q lacks %q", msg, want)
		}
	}
}

func TestEventuallyCancellation(t *testing.T) {
	clock := NewManualClock()
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	fn := func() any {
		calls++
		clock.Advance(tick)
		if calls == 3 {
			cancel()
		}
		return calls
	}
	res := Eventually(fn, Equal(99), WithClock(clock), WithContext(ctx), WithInterval(tick), WithTimeout(timeout))
	if res.Passed || res.Termination != TerminatedCancelled || res.Attempts != 3 {
		t.Fatalf("got %+v, want cancellation after 3 attempts", res)
	}
	if !errors.Is(res.Err, context.Canceled) {
		t.Fatalf("Err = %v, want context.Canceled", res.Err)
	}
	if !strings.Contains(res.Message(), "cancelled") {
		t.Fatalf("message %q does not say cancelled", res.Message())
	}
}

func TestPollAlreadyCancelledContextRunsNoAttempt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for name, poll := range map[string]func(func() any, Matcher, ...PollOption) PollResult{
		"Eventually": Eventually, "Consistently": Consistently,
	} {
		called := false
		res := poll(func() any { called = true; return 1 }, Equal(1), WithClock(NewManualClock()), WithContext(ctx))
		if called || res.Passed || res.Termination != TerminatedCancelled || res.Attempts != 0 {
			t.Errorf("%s: called=%v %+v, want cancelled with no attempt", name, called, res)
		}
	}
}

func TestPollDeadlineContextIsReportedAsCancelled(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	res := Eventually(func() any { return 1 }, Equal(1), WithClock(NewManualClock()), WithContext(ctx))
	if res.Termination != TerminatedCancelled || !errors.Is(res.Err, context.DeadlineExceeded) {
		t.Fatalf("got %+v, want cancelled with DeadlineExceeded", res)
	}
}

func TestConsistentlyHoldsForTheWholeInterval(t *testing.T) {
	clock := NewManualClock()
	fn, calls := stepper(clock, tick, 5)
	res := Consistently(fn, Equal(5), WithClock(clock), WithInterval(tick), WithTimeout(timeout))
	if !res.Passed || res.Termination != TerminatedHeld || res.Attempts != 10 || *calls != 10 {
		t.Fatalf("got %+v, want a pass after 10 attempts", res)
	}
	if res.Elapsed != timeout || res.Last != 5 || res.Failure != "" {
		t.Fatalf("Elapsed=%v Last=%v Failure=%q", res.Elapsed, res.Last, res.Failure)
	}
}

func TestConsistentlyFailsEarlyOnFirstMismatch(t *testing.T) {
	clock := NewManualClock()
	fn, calls := stepper(clock, tick, 5, 5, 6)
	res := Consistently(fn, Equal(5), WithClock(clock), WithInterval(tick), WithTimeout(timeout))
	if res.Passed || res.Termination != TerminatedMismatch {
		t.Fatalf("got %+v, want an early mismatch", res)
	}
	if res.Attempts != 3 || *calls != 3 || res.Elapsed != 3*tick {
		t.Fatalf("Attempts=%d calls=%d Elapsed=%v, want to stop at attempt 3 after 30ms", res.Attempts, *calls, res.Elapsed)
	}
	msg := res.Message()
	for _, want := range []string{"Consistently", "attempt 3", "last observed: 6", "matcher failure: "} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q lacks %q", msg, want)
		}
	}
}

func TestConsistentlyCancellationFails(t *testing.T) {
	clock := NewManualClock()
	ctx, cancel := context.WithCancel(context.Background())
	fn := func() any { clock.Advance(tick); cancel(); return 5 }
	res := Consistently(fn, Equal(5), WithClock(clock), WithContext(ctx), WithInterval(tick), WithTimeout(timeout))
	if res.Passed || res.Termination != TerminatedCancelled || res.Attempts != 1 {
		t.Fatalf("got %+v, want a failure: the condition was not observed for the whole interval", res)
	}
}

func TestPollPanicInCallbackBecomesAFinalVerdict(t *testing.T) {
	for name, poll := range map[string]func(func() any, Matcher, ...PollOption) PollResult{
		"Eventually": Eventually, "Consistently": Consistently,
	} {
		res := poll(func() any { panic("boom") }, Equal(1), WithClock(NewManualClock()))
		if res.Passed || res.Termination != TerminatedPanic || res.Attempts != 1 {
			t.Fatalf("%s: got %+v, want a panic verdict", name, res)
		}
		if res.Panic != "boom" || !strings.Contains(res.Stack, "TestPollPanicInCallback") {
			t.Errorf("%s: Panic=%v Stack lacks the test frame:\n%s", name, res.Panic, res.Stack)
		}
		if !strings.Contains(res.Message(), "panicked on attempt 1 after 0s: boom") {
			t.Errorf("%s: message %q lacks the panic value", name, res.Message())
		}
	}
}

type panickingMatcher struct{}

func (panickingMatcher) Match(any) bool            { panic("matcher boom") }
func (panickingMatcher) FailureMessage(any) string { return "unused" }

func TestPollPanicInMatcherIsRecovered(t *testing.T) {
	res := Eventually(func() any { return 1 }, panickingMatcher{}, WithClock(NewManualClock()))
	if res.Termination != TerminatedPanic || res.Panic != "matcher boom" || res.Last != 1 {
		t.Fatalf("got %+v, want the matcher panic recovered with the observed value kept", res)
	}
}

func TestPollBlockingCallbackIsNotInterruptedButIsBounded(t *testing.T) {
	// The callback "blocks" past the deadline (it advances the clock by 5x the timeout). The helper
	// cannot and does not interrupt it: the attempt finishes, its verdict counts, and then the
	// deadline ends the poll without a further attempt.
	clock := NewManualClock()
	calls := 0
	fn := func() any { calls++; clock.Advance(5 * timeout); return 0 }
	res := Eventually(fn, Equal(1), WithClock(clock), WithInterval(tick), WithTimeout(timeout))
	if calls != 1 || res.Termination != TerminatedTimeout || res.Elapsed != 5*timeout {
		t.Fatalf("calls=%d %+v, want exactly one attempt and a timeout", calls, res)
	}
	// A late success is still a success.
	clock = NewManualClock()
	res = Eventually(func() any { clock.Advance(5 * timeout); return 1 }, Equal(1), WithClock(clock), WithTimeout(timeout))
	if !res.Passed {
		t.Fatalf("got %+v, want a late match to count", res)
	}
}

func TestPollInvalidOptions(t *testing.T) {
	ok := func() any { return 1 }
	cases := map[string]struct {
		fn   func() any
		m    Matcher
		opts []PollOption
		want string
	}{
		"zero timeout":      {ok, Equal(1), []PollOption{WithTimeout(0)}, "timeout must be positive"},
		"negative timeout":  {ok, Equal(1), []PollOption{WithTimeout(-1)}, "timeout must be positive"},
		"zero interval":     {ok, Equal(1), []PollOption{WithInterval(0)}, "interval must be positive"},
		"negative interval": {ok, Equal(1), []PollOption{WithInterval(-time.Second)}, "interval must be positive"},
		"nil context":       {ok, Equal(1), []PollOption{WithContext(nil)}, "context must not be nil"}, //nolint:staticcheck // nil is the case under test
		"nil clock":         {ok, Equal(1), []PollOption{WithClock(nil)}, "clock must not be nil"},
		"nil option":        {ok, Equal(1), []PollOption{nil}, "option must not be nil"},
		"nil callback":      {nil, Equal(1), nil, "callback must not be nil"},
		"nil matcher":       {ok, nil, nil, "matcher must not be nil"},
		"typed nil matcher": {ok, (*equalMatcher)(nil), nil, "matcher must not be nil"},
	}
	for name, tc := range cases {
		for mode, poll := range map[string]func(func() any, Matcher, ...PollOption) PollResult{
			"Eventually": Eventually, "Consistently": Consistently,
		} {
			res := poll(tc.fn, tc.m, tc.opts...)
			if res.Passed || res.Termination != TerminatedInvalid || res.Attempts != 0 || res.Err == nil {
				t.Errorf("%s/%s: got %+v, want an invalid verdict with no attempt", mode, name, res)
				continue
			}
			if !strings.Contains(res.Err.Error(), tc.want) || !strings.Contains(res.Message(), tc.want) {
				t.Errorf("%s/%s: Err=%v Message=%q, want %q", mode, name, res.Err, res.Message(), tc.want)
			}
		}
	}
}

func TestPollInvalidOptionsAreAllReported(t *testing.T) {
	res := Eventually(func() any { return 1 }, Equal(1), WithTimeout(0), WithInterval(0))
	if !strings.Contains(res.Message(), "timeout") || !strings.Contains(res.Message(), "interval") {
		t.Fatalf("message %q should name both invalid options", res.Message())
	}
}

func TestPollIntervalLongerThanTimeoutAttemptsOnce(t *testing.T) {
	clock := NewManualClock()
	fn, calls := stepper(clock, timeout, 1)
	res := Consistently(fn, Equal(1), WithClock(clock), WithInterval(time.Hour), WithTimeout(timeout))
	if !res.Passed || *calls != 1 {
		t.Fatalf("got %+v after %d calls, want one attempt then a pass", res, *calls)
	}
}

func TestPollEvaluatesMatcherOncePerAttempt(t *testing.T) {
	clock := NewManualClock()
	calls := 0
	m := Satisfy("counted", func(any) bool { calls++; return false })
	fn, _ := stepper(clock, tick, 1)
	res := Eventually(fn, m, WithClock(clock), WithInterval(tick), WithTimeout(timeout))
	if calls != res.Attempts {
		t.Fatalf("matcher ran %d times for %d attempts, want exactly one per attempt", calls, res.Attempts)
	}
}

func TestPollLeavesNoGoroutinesBehind(t *testing.T) {
	before := runtime.NumGoroutine()
	clock := NewManualClock()
	for i := 0; i < 20; i++ {
		fn, _ := stepper(clock, tick, 1)
		Eventually(fn, Equal(2), WithClock(clock), WithInterval(tick), WithTimeout(timeout))
		Consistently(fn, Equal(1), WithClock(clock), WithInterval(tick), WithTimeout(timeout))
		Eventually(func() any { panic("x") }, Equal(1), WithClock(clock))
	}
	if after := runtime.NumGoroutine(); after > before {
		t.Fatalf("goroutines: %d before, %d after", before, after)
	}
	if n := clock.Pending(); n != 0 {
		t.Fatalf("%d timers still pending after the helpers returned", n)
	}
}

func TestPollDefaultsUseTheRealClock(t *testing.T) {
	res := Eventually(func() any { return true }, BeTrue())
	if !res.Passed || res.Attempts != 1 {
		t.Fatalf("got %+v, want an immediate pass on the real clock", res)
	}
	res = Consistently(func() any { return false }, BeTrue())
	if res.Passed || res.Termination != TerminatedMismatch {
		t.Fatalf("got %+v, want an immediate mismatch on the real clock", res)
	}
}

func TestManualClockTimers(t *testing.T) {
	c := NewManualClock()
	start := c.Now()
	early, late := c.NewTimer(time.Second), c.NewTimer(2*time.Second)
	immediate := c.NewTimer(0)
	select {
	case <-immediate.C():
	default:
		t.Fatal("a zero-duration timer must fire immediately")
	}
	c.Advance(time.Second)
	select {
	case fired := <-early.C():
		if !fired.Equal(start.Add(time.Second)) {
			t.Fatalf("fired at %v, want %v", fired, start.Add(time.Second))
		}
	default:
		t.Fatal("early timer did not fire")
	}
	select {
	case <-late.C():
		t.Fatal("late timer fired early")
	default:
	}
	if !late.Stop() || late.Stop() {
		t.Fatal("Stop must report true once for a pending timer, then false")
	}
	if c.Pending() != 0 {
		t.Fatalf("Pending = %d, want 0", c.Pending())
	}
	c.Advance(time.Hour)
	select {
	case <-late.C():
		t.Fatal("a stopped timer fired")
	default:
	}
	if !c.Now().Equal(start.Add(time.Hour + time.Second)) {
		t.Fatalf("Now = %v", c.Now())
	}
}

func TestPollGoexitInCallbackStopsTheAttemptTimer(t *testing.T) {
	clock := NewManualClock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		Eventually(func() any { runtime.Goexit(); return nil }, Equal(1), WithClock(clock))
	}()
	<-done
	if got := clock.Pending(); got != 0 {
		t.Fatalf("Pending() = %d after the callback called runtime.Goexit, want 0 (a timer leaked)", got)
	}
}

func TestPollReleasesEveryTimerOnEachExitPath(t *testing.T) {
	clock := NewManualClock()
	step, _ := stepper(clock, tick, 1)
	Eventually(step, Equal(1), WithClock(clock))
	if got := clock.Pending(); got != 0 {
		t.Errorf("Pending() = %d after a matching Eventually, want 0", got)
	}
	step, _ = stepper(clock, tick, 1)
	Consistently(step, Equal(1), WithClock(clock), WithTimeout(timeout), WithInterval(tick))
	if got := clock.Pending(); got != 0 {
		t.Errorf("Pending() = %d after a held Consistently, want 0", got)
	}
	Eventually(func() any { panic("boom") }, Equal(1), WithClock(clock))
	if got := clock.Pending(); got != 0 {
		t.Errorf("Pending() = %d after a panicking callback, want 0", got)
	}
}

// neverMatches fails with a fixed message, so these tests exercise PollResult.Message alone and not
// the failure message of a matcher (Satisfy, for one, renders its actual with a plain %v).
type neverMatches struct{}

func (neverMatches) Match(any) bool            { return false }
func (neverMatches) FailureMessage(any) string { return "never matches" }

const cycleChildEnv = "GO_SPECS_POLL_CYCLE_CHILD"

// runCycleChild re-executes this test binary running only the calling test with cycleChildEnv set, so a
// stack overflow (which no recover can catch) fails one test instead of killing the whole suite.
func runCycleChild(t *testing.T, name string, observe func() any) {
	t.Helper()
	if os.Getenv(cycleChildEnv) == name {
		clock := NewManualClock()
		res := Eventually(func() any { clock.Advance(tick); return observe() }, neverMatches{}, WithClock(clock), WithTimeout(tick), WithInterval(tick))
		fmt.Println("MESSAGE:", res.Message())
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$")
	cmd.Env = append(os.Environ(), cycleChildEnv+"="+name)
	out, err := cmd.CombinedOutput()
	head := string(out)
	if len(head) > 2000 {
		head = head[:2000]
	}
	if err != nil {
		t.Fatalf("child failed: %v\n%s", err, head)
	}
	if !strings.Contains(string(out), "<cycle>") {
		t.Fatalf("message lacks the cycle marker:\n%s", head)
	}
}

func TestPollMessageRendersACyclicMapSafely(t *testing.T) {
	runCycleChild(t, "map", func() any {
		m := map[string]any{}
		m["self"] = m
		return m
	})
}

func TestPollMessageRendersACyclicSliceSafely(t *testing.T) {
	runCycleChild(t, "slice", func() any {
		s := make([]any, 1)
		s[0] = s
		return s
	})
}

func TestPollMessageRendersACyclicPointerStructSafely(t *testing.T) {
	type node struct{ next *node }
	runCycleChild(t, "struct", func() any {
		n := &node{}
		n.next = n
		return n
	})
}

func TestPollMessageKeepsGoSyntaxForOrdinaryValues(t *testing.T) {
	clock := NewManualClock()
	res := Eventually(func() any { clock.Advance(tick); return map[string]int{"a": 1} }, neverMatches{},
		WithClock(clock), WithTimeout(tick), WithInterval(tick))
	if want := `last observed: map[string]int{"a":1}`; !strings.Contains(res.Message(), want) {
		t.Fatalf("message = %q, want it to contain %q", res.Message(), want)
	}
}
