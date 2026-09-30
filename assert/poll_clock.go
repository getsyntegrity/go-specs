package assert

import (
	"sort"
	"sync"
	"time"
)

// Clock is the source of time Eventually and Consistently poll against. The default is the real
// clock; WithClock swaps it so a test can make time pass explicitly, with no sleeping. ManualClock
// is the ready-made deterministic implementation.
type Clock interface {
	// Now returns the current time.
	Now() time.Time
	// NewTimer returns a timer that delivers one value on its channel after d. A d <= 0 fires at once.
	NewTimer(d time.Duration) Timer
}

// Timer is the subset of *time.Timer the polling helpers use.
type Timer interface {
	// C is the channel the timer fires on. It has a buffer of one, so a fire is never lost.
	C() <-chan time.Time
	// Stop cancels the timer and reports whether it was still pending.
	Stop() bool
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realClock) NewTimer(d time.Duration) Timer { return realTimer{time.NewTimer(d)} }

type realTimer struct{ t *time.Timer }

func (r realTimer) C() <-chan time.Time { return r.t.C }
func (r realTimer) Stop() bool          { return r.t.Stop() }

// ManualClock is a Clock whose time only moves when Advance is called. It starts at an arbitrary
// fixed instant. It is safe for concurrent use and starts no goroutines.
//
// The simplest deterministic test drives it from inside the polled callback, which runs on the
// polling goroutine: the callback calls Advance to model time passing during an attempt, so no
// sleep or second goroutine is needed.
type ManualClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*manualTimer
}

// NewManualClock returns a ManualClock at a fixed, arbitrary start time.
func NewManualClock() *ManualClock {
	return &ManualClock{now: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)}
}

// Now returns the clock's current time.
func (c *ManualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// NewTimer returns a timer that fires once the clock has advanced by d. A d <= 0 fires at once.
func (c *ManualClock) NewTimer(d time.Duration) Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &manualTimer{clock: c, due: c.now.Add(d), ch: make(chan time.Time, 1)}
	if d <= 0 {
		t.ch <- c.now
		return t
	}
	t.pending = true
	c.timers = append(c.timers, t)
	return t
}

// Advance moves the clock forward by d and fires every timer that has come due, earliest first.
// A non-positive d is a no-op.
func (c *ManualClock) Advance(d time.Duration) {
	if d <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	sort.SliceStable(c.timers, func(i, j int) bool { return c.timers[i].due.Before(c.timers[j].due) })
	kept := c.timers[:0]
	for _, t := range c.timers {
		if t.due.After(c.now) {
			kept = append(kept, t)
			continue
		}
		t.pending = false
		t.ch <- t.due
	}
	for i := len(kept); i < len(c.timers); i++ {
		c.timers[i] = nil
	}
	c.timers = kept
}

// Pending reports how many timers have neither fired nor been stopped.
func (c *ManualClock) Pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.timers)
}

type manualTimer struct {
	clock   *ManualClock
	due     time.Time
	ch      chan time.Time
	pending bool
}

func (t *manualTimer) C() <-chan time.Time { return t.ch }

func (t *manualTimer) Stop() bool {
	c := t.clock
	c.mu.Lock()
	defer c.mu.Unlock()
	if !t.pending {
		return false
	}
	t.pending = false
	for i, other := range c.timers {
		if other == t {
			c.timers = append(c.timers[:i], c.timers[i+1:]...)
			break
		}
	}
	return true
}
