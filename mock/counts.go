package mock

import "fmt"

// AtLeast requires at least n calls and sets only the minimum. It panics when n is negative or when
// the resulting range would have a minimum above its maximum.
//
// Count rules:
//   - Times, Never and AnyTimes set a complete configuration (both bounds) and replace whatever
//     count was set before.
//   - AtLeast sets only the minimum and AtMost only the maximum. When the current configuration is
//     a range built by earlier AtLeast/AtMost calls, the other bound is kept, so AtLeast(2).AtMost(5)
//     and AtMost(5).AtLeast(2) both mean 2..5 and AtLeast(2).AtMost(5).AtLeast(3) means 3..5.
//   - When the previous configuration came from Times, Never or AnyTimes, or nothing was set (the
//     default of exactly 1), AtLeast/AtMost start a new range: the other bound resets to its default
//     (minimum 0, maximum unbounded), so Times(2).AtLeast(1) means 1 or more.
//   - Every AtLeast/AtMost validates the resulting range, zero bounds included, and panics when the
//     minimum exceeds the maximum (AtMost(0).AtLeast(1), AtLeast(3).AtMost(2)). A panicking call
//     leaves the expectation unchanged.
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
	if e.kind == countRange {
		max = e.max
	}
	if max >= 0 && n > max {
		panic(fmt.Sprintf("mock: AtLeast(%d) exceeds the maximum of %d", n, max))
	}
	e.kind, e.min, e.max = countRange, n, max
	return e
}

// AtMost allows up to n calls, including none, and sets only the maximum. A call beyond n is an
// unexpected call. AtMost(0), also as the final bound of a range such as AtLeast(0).AtMost(0), is
// Never: a prohibition that takes precedence over permissive expectations. It combines with AtLeast into a range under the rules
// documented on AtLeast, and panics when n is negative or below the range minimum.
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
	if e.kind == countRange {
		min = e.min
	}
	if n < min {
		panic(fmt.Sprintf("mock: AtMost(%d) is below the minimum of %d", n, min))
	}
	e.kind, e.min, e.max = countRange, min, n
	return e
}

// Never declares that no call may match: a matching call is reported immediately as a forbidden
// call. It is equivalent to Times(0) and AtMost(0) (any expectation whose effective maximum is 0)
// and acts as a prohibition: it takes precedence over every permissive expectation matching the
// same call, whatever the declaration order, so Expect(mock.Any()).AnyTimes() cannot swallow it.
// A forbidden call runs no Return/Do, feeds no captor and yields a zero Result. To assert that a whole method was never called,
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
