package mock

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

// TB is what the mock needs from a test: *testing.T, *testing.B, testing.TB and *specs.Context
// all satisfy it. The mock package never imports specs.
type TB interface {
	Helper()
	Cleanup(func())
	Errorf(format string, args ...any)
}

// RecordedCall is one entry of the controller's global call log.
type RecordedCall struct {
	Seq    uint64 // 1-based, unique and dense across every method and spy of the controller
	Method string
	Args   []any
}

// Controller owns one shared recorder: every call to any of its methods (and spies) gets a global
// sequence number, which is what InOrder checks. NewController registers Verify with t.Cleanup.
// It is safe for concurrent use; Reset should not race with calls in flight.
//
// Recorded arguments: the argument slice is copied, the elements are not deep-copied. A pointer,
// slice or map passed to a mocked method is shared with the caller, so mutating it after the call
// changes what the recorder reports. Tests that need a snapshot must copy inside the call.
type Controller struct {
	t TB

	mu       sync.Mutex
	seq      uint64
	methods  map[string]*Method
	names    []string // method names in creation order, for deterministic reports
	spies    map[string]*Spy
	calls    []RecordedCall
	orders   [][]*Expectation
	verified bool
}

// Method is one interface method of a Controller. Obtain it with Controller.Method.
type Method struct {
	c     *Controller
	name  string
	exps  []*Expectation
	calls []RecordedCall
}

// NewController returns a controller reporting to t and registers Verify with t.Cleanup.
func NewController(t TB) *Controller {
	if t == nil {
		panic("mock: NewController requires a non-nil TB")
	}
	c := &Controller{t: t, methods: map[string]*Method{}, spies: map[string]*Spy{}}
	t.Cleanup(c.Verify)
	return c
}

// Method returns the method with the given name, creating it if needed: the same name always
// yields the same *Method.
func (c *Controller) Method(name string) *Method {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if m, ok := c.methods[name]; ok {
		return m
	}
	m := &Method{c: c, name: name}
	c.methods[name] = m
	c.names = append(c.names, name)
	return m
}

// Spy returns a regular Spy for name whose calls also join the controller's global order. The same
// name yields the same spy. Spies do not take part in expectations.
func (c *Controller) Spy(name string) *Spy {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if s, ok := c.spies[name]; ok {
		return s
	}
	s := NewSpy()
	s.sink = func(args []any) { c.record(name, args, nil) }
	c.spies[name] = s
	return s
}

// InOrder declares that the calls matched by exps happened in this order: the first matched call
// of each expectation must come after the first matched call of the previous one.
func (c *Controller) InOrder(exps ...*Expectation) {
	if c == nil {
		return
	}
	kept := make([]*Expectation, 0, len(exps))
	for _, e := range exps {
		if e != nil {
			kept = append(kept, e)
		}
	}
	c.mu.Lock()
	c.orders = append(c.orders, kept)
	c.mu.Unlock()
}

// Calls returns the global call log in sequence order (copies).
func (c *Controller) Calls() []RecordedCall {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return copyCalls(c.calls)
}

// Reset drops expectations, recorded calls (including those of controller spies) and order
// constraints, and re-arms Verify. The controller stays registered for cleanup.
func (c *Controller) Reset() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq, c.calls, c.orders, c.verified = 0, nil, nil, false
	for _, m := range c.methods {
		m.exps, m.calls = nil, nil
	}
	for _, s := range c.spies {
		s.reset()
	}
}

// Verify reports unmet expectations and InOrder violations through t.Errorf. It is idempotent:
// only the first call after creation (or Reset) reports.
func (c *Controller) Verify() {
	if c == nil {
		return
	}
	testingTB(c.t).Helper()
	c.mu.Lock()
	if c.verified {
		c.mu.Unlock()
		return
	}
	c.verified = true
	var msgs []string
	for _, name := range c.names {
		for _, e := range c.methods[name].exps {
			if min, _ := e.bounds(); e.got < min {
				msgs = append(msgs, fmt.Sprintf("unmet expectation %s declared at %s: want %s, got %d",
					e.describe(), e.site, e.countText(), e.got))
			}
		}
	}
	for _, order := range c.orders {
		for i := 0; i+1 < len(order); i++ {
			a, b := order[i], order[i+1]
			if a.firstSeq != 0 && b.firstSeq != 0 && a.firstSeq > b.firstSeq {
				msgs = append(msgs, fmt.Sprintf("order violation: expected %s (declared at %s) before %s (declared at %s), observed sequence: %s",
					a.describe(), a.site, b.describe(), b.site, c.sequenceText()))
			}
		}
	}
	c.mu.Unlock()
	for _, msg := range msgs {
		c.t.Errorf("mock: %s", msg)
	}
}

// sequenceText renders the global log; the caller holds c.mu.
func (c *Controller) sequenceText() string {
	parts := make([]string, len(c.calls))
	for i, rc := range c.calls {
		parts[i] = fmt.Sprintf("#%d %s", rc.Seq, formatCall(rc.Method, rc.Args))
	}
	return strings.Join(parts, ", ")
}

// record appends a call to the global log (and to m when non-nil) and assigns its sequence number
// under the lock, so sequence numbers are unique and dense.
func (c *Controller) record(name string, args []any, m *Method) RecordedCall {
	recorded := append([]any(nil), args...)
	c.mu.Lock()
	c.seq++
	rc := RecordedCall{Seq: c.seq, Method: name, Args: recorded}
	c.calls = append(c.calls, rc)
	if m != nil {
		m.calls = append(m.calls, rc)
	}
	c.mu.Unlock()
	return rc
}

// Expect declares an expectation for calls whose arguments match: each arg is an ArgMatcher, or a
// plain value that is wrapped in Equal. Without a count method it defaults to Times(1).
func (m *Method) Expect(args ...any) *Expectation {
	if m == nil {
		return nil
	}
	matchers := make([]ArgMatcher, len(args))
	for i, a := range args {
		if am, ok := a.(ArgMatcher); ok {
			matchers[i] = am
		} else {
			matchers[i] = Equal(a)
		}
	}
	e := &Expectation{m: m, matchers: matchers, site: declarationSite()}
	m.c.mu.Lock()
	m.exps = append(m.exps, e)
	m.c.mu.Unlock()
	return e
}

// Calls returns this method's recorded calls in order (copies).
func (m *Method) Calls() []RecordedCall {
	if m == nil {
		return nil
	}
	m.c.mu.Lock()
	defer m.c.mu.Unlock()
	return copyCalls(m.calls)
}

// Call records a call and returns the Result of the expectation that took it. When no expectation
// matches (or every matching one is at capacity) the failure is reported immediately through
// t.Errorf and a zero Result is returned so the code under test keeps running.
func (m *Method) Call(args ...any) Result {
	if m == nil {
		return Result{}
	}
	c := m.c
	testingTB(c.t).Helper()
	rc := c.record(m.name, args, m)

	// Run matchers outside the lock on the recorded copy.
	c.mu.Lock()
	exps := append([]*Expectation(nil), m.exps...)
	c.mu.Unlock()
	reasons := make([]string, len(exps))
	for i, e := range exps {
		reasons[i] = e.mismatch(rc.Args)
	}

	c.mu.Lock()
	var claimed *Expectation
	var n int
	for i, e := range exps {
		if reasons[i] == "" && e.hasCapacity() {
			e.got++
			if e.firstSeq == 0 {
				e.firstSeq = rc.Seq
			}
			claimed, n = e, e.got
			break
		}
	}
	var lines []string
	if claimed == nil {
		for i, e := range exps {
			reason := reasons[i]
			if reason == "" {
				reason = e.capacityReason()
			}
			lines = append(lines, fmt.Sprintf("  expectation %d %s declared at %s: %s", i+1, e.describe(), e.site, reason))
		}
	}
	c.mu.Unlock()

	if claimed == nil {
		detail := ": no expectations declared for this method"
		if len(lines) > 0 {
			detail = ":\n" + strings.Join(lines, "\n")
		}
		c.t.Errorf("mock: unexpected call %s%s", formatCall(m.name, rc.Args), detail)
		return Result{}
	}
	claimed.notifyClaim(rc.Args)
	return claimed.respond(n, rc.Args)
}

func formatCall(name string, args []any) string {
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = formatValue(a)
	}
	return name + "(" + strings.Join(parts, ", ") + ")"
}

func copyCalls(in []RecordedCall) []RecordedCall {
	out := make([]RecordedCall, len(in))
	for i, rc := range in {
		out[i] = RecordedCall{Seq: rc.Seq, Method: rc.Method, Args: append([]any(nil), rc.Args...)}
	}
	return out
}

// testingTB returns the real testing.TB behind t when t exposes one through a Testing() method, as
// *specs.Context does, and t itself otherwise. Every mock function that reports calls
// testingTB(c.t).Helper() in its own body: Helper marks the function that calls it, so calling it on
// the wrapper would only mark the wrapper's own Helper method and leave the mock frame visible in the
// reported file:line on a real *testing.T. testingTB must not call Helper itself for the same reason.
func testingTB(t TB) interface{ Helper() } {
	if x, ok := t.(interface{ Testing() testing.TB }); ok {
		if tb := x.Testing(); tb != nil {
			return tb
		}
	}
	return t
}
