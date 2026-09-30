// spies_test.go shows mock.Spy, the smallest test double in go-specs.
//
// A spy records how it was called and does nothing else. Use it when you only need to answer
// "was this dependency called, how many times and with what?" and you do not need to script
// return values or fail on unexpected calls. When you do need that (return values, call counts as
// expectations, ordering), use mock.Controller instead, see mocks_test.go.
//
// Three ways to get a spy:
//
//   - mock.NewSpy() for a standalone spy;
//   - mock.New().Spy(name) for named spies grouped in one *mock.Mock (same name, same spy);
//   - mock.NewController(t).Spy(name), whose calls also join the controller's global call order.
//
// A spy is safe for concurrent use, copies the argument slice on Call, and never panics on a nil
// receiver (Call is a no-op and the queries report "no calls").
package examples_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/getsyntegrity/go-specs/mock"
	"github.com/getsyntegrity/go-specs/specs"
)

// A new spy has recorded nothing. Call appends one entry per invocation, in order, and Calls
// returns a copy of them.
func Example_spyRecordsCalls() {
	spy := mock.NewSpy()
	fmt.Println(spy.CallCount(), spy.WasCalled())

	spy.Call("hello")
	spy.Call("world", 2)

	fmt.Println(spy.CallCount(), spy.WasCalled())
	for i, c := range spy.Calls() {
		fmt.Println(i, c.Args)
	}
	// Output:
	// 0 false
	// 2 true
	// 0 [hello]
	// 1 [world 2]
}

// CalledWith is true when at least one recorded call matches every matcher. The matchers must be
// as many as the call arguments: a call with more or fewer arguments never matches.
func Example_spyCalledWith() {
	spy := mock.NewSpy()
	spy.Call("user@example.com", 42)

	fmt.Println(spy.CalledWith(mock.Equal("user@example.com"), mock.Equal(42)))
	fmt.Println(spy.CalledWith(mock.Equal("other@example.com"), mock.Any()))
	fmt.Println(spy.CalledWith(mock.Equal("user@example.com"))) // wrong number of arguments
	// Output:
	// true
	// false
	// false
}

// mock.Any accepts any value, mock.Match takes a predicate over any, and mock.MatchT takes a typed
// predicate (an argument of another type simply does not match). Use them when the exact value is
// irrelevant or is not comparable with Equal.
func Example_spyArgumentMatchers() {
	spy := mock.NewSpy()
	spy.Call(100, "ignored", 3.5)

	fmt.Println(spy.CalledWith(mock.Equal(100), mock.Any(), mock.Any()))
	fmt.Println(spy.CalledWith(
		mock.Match("a positive number", func(v any) bool { n, ok := v.(int); return ok && n > 0 }),
		mock.Any(),
		mock.Any(),
	))
	fmt.Println(spy.CalledWith(
		mock.MatchT("an even int", func(n int) bool { return n%2 == 0 }),
		mock.MatchT("a non-empty string", func(s string) bool { return s != "" }),
		mock.MatchT("a float below 1", func(f float64) bool { return f < 1 }),
	))
	// Output:
	// true
	// true
	// false
}

// spyFakeTB is a tiny testing.TB that records Fatalf messages instead of failing. It embeds
// testing.TB to satisfy the interface and overrides only what CalledTimes uses.
type spyFakeTB struct {
	testing.TB
	messages []string
}

func (f *spyFakeTB) Helper() {}
func (f *spyFakeTB) Fatalf(format string, args ...any) {
	f.messages = append(f.messages, fmt.Sprintf(format, args...))
}

// CalledTimes asserts an exact call count and fails the test through Fatalf when it differs. Inside
// a spec pass ctx.T. Here the fake records the message so the failure text is visible without
// failing anything.
func Example_spyCalledTimesDiagnostic() {
	spy := mock.NewSpy()
	spy.Call()
	spy.Call()

	tb := &spyFakeTB{}
	spy.CalledTimes(tb, 2) // passes: nothing recorded
	spy.CalledTimes(tb, 3) // fails: the count is reported
	fmt.Println(tb.messages)
	// Output: [expected 3 calls, got 2]
}

// The argument slice is copied on Call, so a caller that reuses and mutates its slice afterwards
// does not rewrite history. The elements are not deep-copied: a pointer, map or slice element is
// still shared with the caller.
func Example_spyCopiesArguments() {
	spy := mock.NewSpy()
	args := []any{"first"}
	spy.Call(args...)
	args[0] = "mutated"

	fmt.Println(spy.Calls()[0].Args)
	// Output: [first]
}

// A nil spy is safe: Call does nothing and every query reports "no calls". This lets an adapter
// hold an optional spy without nil checks.
func Example_spyNilIsSafe() {
	var spy *mock.Spy
	spy.Call("ignored")

	fmt.Println(spy.CallCount(), spy.WasCalled(), spy.CalledWith(mock.Any()))
	// Output: 0 false false
}

// Spies are safe for concurrent use: calls from several goroutines are all recorded. Only the
// count is deterministic, not the order between goroutines.
func Example_spyConcurrentCalls() {
	spy := mock.NewSpy()
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			spy.Call(i)
		}()
	}
	wg.Wait()

	fmt.Println(spy.CallCount())
	// Output: 20
}

// mock.New groups named spies: asking twice for the same name returns the same spy, so the code
// under test and the assertion can find it by name alone.
func Example_spyNamedSpies() {
	m := mock.New()
	notify := m.Spy("notify")
	notify.Call("payment received", 99)

	// A second lookup by name is the same spy.
	same := m.Spy("notify")
	same.Call("refund issued", 10)

	fmt.Println(notify.CallCount(), notify == same)
	fmt.Println(notify.CalledWith(mock.Equal("payment received"), mock.Equal(99)))
	fmt.Println(m.Spy("never-used").WasCalled())
	// Output:
	// 2 true
	// true
	// false
}

// A controller spy behaves like any other spy but its calls also join the controller's global
// sequence, next to the calls of mocked methods. That is what makes cross-dependency ordering
// visible in Controller.Calls (see mocks_test.go for InOrder).
func TestSpies_controllerSpyJoinsGlobalOrder(t *testing.T) {
	specs.Describe(t, "controller spy", func(s *specs.Spec) {
		s.It("shares one call log with mocked methods", func(ctx *specs.Context) {
			ctrl := mock.NewController(ctx)
			audit := ctrl.Spy("audit")
			ctrl.Method("Charge").Expect(mock.Any()).Return(nil)

			audit.Call("start")
			ctrl.Method("Charge").Call(10)
			audit.Call("done")

			var order []string
			for _, c := range ctrl.Calls() {
				order = append(order, c.Method)
			}
			ctx.Expect(order).To(specs.Equal([]string{"audit", "Charge", "audit"}))
			ctx.Expect(audit.CallCount()).ToEqual(2)
		})
	})
}

// --- A spy behind an interface -----------------------------------------------------------------
//
// The service under test depends on a small interface. The test injects an adapter that forwards
// every call to a spy, then asserts on what the service did. No other machinery is needed.

// spyNotifier is the port the alert service depends on. A real implementation would send email or
// publish an event.
type spyNotifier interface {
	Notify(msg string)
}

// recordingNotifier implements spyNotifier by recording every Notify call into a spy.
type recordingNotifier struct{ spy *mock.Spy }

func (n *recordingNotifier) Notify(msg string) {
	if n.spy != nil {
		n.spy.Call(msg)
	}
}

// spyAlertService raises alerts through its notifier. A nil notifier means "alerts are off".
type spyAlertService struct{ notifier spyNotifier }

func (svc *spyAlertService) RaiseAlert(msg string) {
	if svc.notifier != nil {
		svc.notifier.Notify(msg)
	}
}

func TestSpies_behindAnInterface(t *testing.T) {
	specs.Describe(t, "alert service", func(s *specs.Spec) {
		s.It("notifies once per alert", func(ctx *specs.Context) {
			spy := mock.NewSpy()
			svc := &spyAlertService{notifier: &recordingNotifier{spy: spy}}

			svc.RaiseAlert("payment failed")

			ctx.Expect(spy.CallCount()).ToEqual(1)
			ctx.Expect(spy.CalledWith(mock.Equal("payment failed"))).To(specs.BeTrue())
		})

		s.It("keeps every alert in order", func(ctx *specs.Context) {
			spy := mock.NewSpy()
			svc := &spyAlertService{notifier: &recordingNotifier{spy: spy}}

			svc.RaiseAlert("first")
			svc.RaiseAlert("second")

			ctx.Expect(spy.CallCount()).ToEqual(2)
			calls := spy.Calls()
			ctx.Expect(calls[0].Args).To(specs.Equal([]any{"first"}))
			ctx.Expect(calls[1].Args).To(specs.Equal([]any{"second"}))
		})

		s.It("asserts the exact count with CalledTimes", func(ctx *specs.Context) {
			spy := mock.NewSpy()
			svc := &spyAlertService{notifier: &recordingNotifier{spy: spy}}
			svc.RaiseAlert("a")
			svc.RaiseAlert("b")

			spy.CalledTimes(ctx.T, 2)
		})

		s.It("stays silent when alerts are off (nil notifier)", func(ctx *specs.Context) {
			svc := &spyAlertService{}
			svc.RaiseAlert("ignored") // must not panic
		})

		s.It("records nothing before any alert is raised", func(ctx *specs.Context) {
			spy := mock.NewSpy()
			_ = &spyAlertService{notifier: &recordingNotifier{spy: spy}}

			ctx.Expect(spy.WasCalled()).To(specs.BeFalse())
		})
	})
}

// A named spy from mock.New used directly inside a spec, with the assertions written through
// ctx.Expect.
func TestSpies_namedSpyInASpec(t *testing.T) {
	specs.Describe(t, "email service", func(s *specs.Spec) {
		s.It("sends an email to the user", func(ctx *specs.Context) {
			m := mock.New()
			sendEmail := m.Spy("sendEmail")

			sendEmail.Call("user@test.com")

			ctx.Expect(sendEmail.CallCount()).ToEqual(1)
			ctx.Expect(sendEmail.CalledWith(mock.Equal("user@test.com"))).To(specs.BeTrue())
		})
	})
}
