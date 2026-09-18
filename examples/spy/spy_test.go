package spy_test

import (
	"testing"

	"github.com/getsyntegrity/go-specs/mock"
	"github.com/getsyntegrity/go-specs/specs"
)

// Example: using mock.Spy to record and assert on function invocations
// without replacing real implementations (spy wraps or observes calls).

func TestSpyExample(t *testing.T) {
	specs.Describe(t, "Spy", func(s *specs.Spec) {
		s.It("records no calls initially", func(ctx *specs.Context) {
			spy := mock.NewSpy()
			ctx.Expect(spy.CallCount()).ToEqual(0)
			ctx.Expect(spy.WasCalled()).To(specs.BeFalse())
		})

		s.It("records each Call with arguments", func(ctx *specs.Context) {
			spy := mock.NewSpy()
			spy.Call("hello")
			spy.Call("world")
			ctx.Expect(spy.CallCount()).ToEqual(2)
			ctx.Expect(spy.WasCalled()).To(specs.BeTrue())
		})

		s.It("CalledWith returns true when a call matches Equal matcher", func(ctx *specs.Context) {
			spy := mock.NewSpy()
			spy.Call("user@example.com", 42)
			ctx.Expect(spy.CalledWith(mock.Equal("user@example.com"), mock.Equal(42))).To(specs.BeTrue())
			ctx.Expect(spy.CalledWith(mock.Equal("other@example.com"))).To(specs.BeFalse())
		})

		s.It("CalledWith accepts Any matcher for wildcard args", func(ctx *specs.Context) {
			spy := mock.NewSpy()
			spy.Call(100, "ignored")
			ctx.Expect(spy.CalledWith(mock.Equal(100), mock.Any())).To(specs.BeTrue())
		})

		s.It("CalledTimes asserts exact call count", func(ctx *specs.Context) {
			spy := mock.NewSpy()
			spy.Call()
			spy.Call()
			spy.CalledTimes(ctx.T, 2)
		})
	})
}

func TestSpyWithMock(t *testing.T) {
	specs.Describe(t, "Mock.Spy", func(s *specs.Spec) {
		s.It("returns a named spy and records calls", func(ctx *specs.Context) {
			m := mock.New()
			notify := m.Spy("notify")
			notify.Call("payment received", 99)
			ctx.Expect(notify.CallCount()).ToEqual(1)
			ctx.Expect(notify.CalledWith(mock.Equal("payment received"), mock.Equal(99))).To(specs.BeTrue())
		})

		s.It("reuses the same spy for the same name", func(ctx *specs.Context) {
			m := mock.New()
			a := m.Spy("log")
			b := m.Spy("log")
			a.Call("first")
			b.Call("second")
			ctx.Expect(a.CallCount()).ToEqual(2)
			ctx.Expect(b.CallCount()).ToEqual(2)
		})
	})
}

// AlertService depends on Notifier; we inject a SpyNotifier in tests to verify it calls Notify.
type AlertService struct {
	Notifier Notifier
}

func (svc *AlertService) RaiseAlert(msg string) {
	if svc.Notifier != nil {
		svc.Notifier.Notify(msg)
	}
}

func TestInterfaceWithSpy(t *testing.T) {
	specs.Describe(t, "AlertService", func(s *specs.Spec) {
		s.It("calls Notifier.Notify when RaiseAlert is called", func(ctx *specs.Context) {
			spy := mock.NewSpy()
			svc := &AlertService{Notifier: &SpyNotifier{Spy: spy}}
			svc.RaiseAlert("payment failed")
			ctx.Expect(spy.CallCount()).ToEqual(1)
			ctx.Expect(spy.CalledWith(mock.Equal("payment failed"))).To(specs.BeTrue())
		})

		s.It("does not panic when Notifier is nil", func(ctx *specs.Context) {
			svc := &AlertService{Notifier: nil}
			svc.RaiseAlert("ignored")
		})

		s.It("records multiple RaiseAlert calls on the spy", func(ctx *specs.Context) {
			spy := mock.NewSpy()
			svc := &AlertService{Notifier: &SpyNotifier{Spy: spy}}
			svc.RaiseAlert("first")
			svc.RaiseAlert("second")
			ctx.Expect(spy.CallCount()).ToEqual(2)
			ctx.Expect(spy.CalledWith(mock.Equal("first"))).To(specs.BeTrue())
			ctx.Expect(spy.CalledWith(mock.Equal("second"))).To(specs.BeTrue())
		})
	})
}

// TestSpyWithPaths uses Paths() to automatically scan combinations; each run gets path values via ctx.Path().
func TestSpyWithPaths(t *testing.T) {
	t.Skip("paths combinatorial execution with top-level Describe deferred to post-v1.0.0")
	specs.Describe(t, "AlertService with paths", func(s *specs.Spec) {
		s.Paths(func(p *specs.PathBuilder) {
			p.Bool("notify")
			p.IntRange("severity", 1, 3) // 1=low, 2=medium, 3=high
		}).It("notifies with message built from path when notify is true", func(ctx *specs.Context) {
			notify := ctx.Path().Bool("notify")
			severity := ctx.Path().Int("severity")
			spy := mock.NewSpy()
			svc := &AlertService{Notifier: &SpyNotifier{Spy: spy}}
			msg := "alert"
			if notify {
				svc.RaiseAlert(msg)
			}
			if notify {
				ctx.Expect(spy.CallCount()).ToEqual(1)
				ctx.Expect(spy.CalledWith(mock.Equal(msg))).To(specs.BeTrue())
			} else {
				ctx.Expect(spy.CallCount()).ToEqual(0)
			}
			// severity is available for building richer messages or assertions
			ctx.Expect(severity >= 1 && severity <= 3).To(specs.BeTrue())
		})
	})
}
