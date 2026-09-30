package mock

import "fmt"

// AtLeast requires at least n calls with no upper bound. It panics when n is negative.
//
// Count rules: AtLeast and AtMost combine into a range in either order, so AtLeast(2).AtMost(5)
// and AtMost(5).AtLeast(2) both mean 2..5 calls. Times, Never and AnyTimes replace whatever count
// was set before, and Times/AtLeast/AtMost called after them start over: the last count call wins
// except for that AtLeast/AtMost pairing. A range whose minimum exceeds its maximum panics.
func (e *Expectation) AtLeast(n int) *Expectation {
	if e == nil {
		return nil
	}
	if n < 0 {
		panic(fmt.Sprintf("mock: AtLeast requires n >= 0, got %d", n))
	}
	c := e.m.c
	c.mu.Lock()
	defer c.mu.Unlock()
	max := -1
	// A finite maximum with min 0 and max > 0 can only come from AtMost: keep it as the range top.
	if e.countSet && e.min == 0 && e.max > 0 {
		max = e.max
		if n > max {
			panic(fmt.Sprintf("mock: AtLeast(%d) exceeds the maximum of %d", n, max))
		}
	}
	e.countSet, e.min, e.max = true, n, max
	return e
}

// AtMost allows up to n calls, including none. A call beyond n is an unexpected call. AtMost(0)
// is Never. It combines with AtLeast into a range (see AtLeast) and panics when n is negative.
func (e *Expectation) AtMost(n int) *Expectation {
	if e == nil {
		return nil
	}
	if n < 0 {
		panic(fmt.Sprintf("mock: AtMost requires n >= 0, got %d", n))
	}
	c := e.m.c
	c.mu.Lock()
	defer c.mu.Unlock()
	min := 0
	// An unbounded minimum from AtLeast (or AnyTimes, whose min is 0) becomes the range bottom.
	if e.countSet && e.max < 0 {
		min = e.min
		if min > n {
			panic(fmt.Sprintf("mock: AtMost(%d) is below the minimum of %d", n, min))
		}
	}
	e.countSet, e.min, e.max = true, min, n
	return e
}

// Never declares that no call may match: a matching call is reported immediately as an unexpected
// call. It is equivalent to Times(0) and AtMost(0). To assert that a whole method was never called,
// declare Expect(...).Never() with one matcher per argument (mock.Any() for each), or check
// len(method.Calls()) == 0 after the code under test ran.
func (e *Expectation) Never() *Expectation {
	if e == nil {
		return nil
	}
	e.setCount(0, 0)
	return e
}

// AnyTimes accepts any number of calls, including none.
func (e *Expectation) AnyTimes() *Expectation {
	if e == nil {
		return nil
	}
	e.setCount(0, -1)
	return e
}

// capacityReason explains why a matching expectation cannot take another call; the caller holds
// the controller lock.
func (e *Expectation) capacityReason() string {
	if _, max := e.bounds(); max == 0 {
		return "matched but the expectation says never"
	}
	return fmt.Sprintf("matched but at capacity (want %s, got %d)", e.countText(), e.got)
}

// notifyClaim tells claim-aware matchers (captors) that the call was claimed by e. It runs outside
// the controller lock, after the claim, so a call rejected for capacity is never observed.
func (e *Expectation) notifyClaim(args []any) {
	for i, m := range e.matchers {
		if cl, ok := m.(claimObserver); ok && i < len(args) {
			cl.claimed(args[i])
		}
	}
}

type claimObserver interface{ claimed(v any) }
