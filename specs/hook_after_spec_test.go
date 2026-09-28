package specs

import (
	"strings"
	"testing"
)

// Per-spec hooks (BeforeEach/AfterEach) are captured when each spec is registered, so one declared
// after an It or a nested scope would silently skip the earlier specs while BeforeAll/AfterAll cover
// the whole scope (issue #307). Every build path now fails the build with an actionable panic
// instead: hooks must come before the first spec or nested scope of their scope.

func requireLateHookPanic(t *testing.T, method string, fn func()) {
	t.Helper()
	msg := recoverMessage(t, fn)
	for _, want := range []string{"specs:", method, "before the first"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("panic %q must contain %q", msg, want)
		}
	}
}

func TestSpecHookAfterSpecOrScopePanics(t *testing.T) {
	builds := map[string]func(fn func(*Spec)){
		"compiler": func(fn func(*Spec)) { BuildSuite(nil, "suite", fn) },
		"registry": func(fn func(*Spec)) { Analyze(func() { Describe(nil, "suite", fn) }) },
	}
	for name, build := range builds {
		for _, method := range []string{"BeforeEach", "AfterEach"} {
			hook := func(s *Spec) {
				if method == "BeforeEach" {
					s.BeforeEach(func(*Context) {})
				} else {
					s.AfterEach(func(*Context) {})
				}
			}
			t.Run(name+"/"+method+"/after It", func(t *testing.T) {
				requireLateHookPanic(t, method, func() {
					build(func(s *Spec) {
						s.It("a", func(*Context) {})
						hook(s)
					})
				})
			})
			t.Run(name+"/"+method+"/after nested When", func(t *testing.T) {
				requireLateHookPanic(t, method, func() {
					build(func(s *Spec) {
						s.When("child", func(w *Spec) { w.It("a", func(*Context) {}) })
						hook(s)
					})
				})
			})
			t.Run(name+"/"+method+"/after SkipIt", func(t *testing.T) {
				requireLateHookPanic(t, method, func() {
					build(func(s *Spec) {
						s.SkipIt("a", func(*Context) {})
						hook(s)
					})
				})
			})
		}
	}
}

func TestSpecHooksBeforeSpecsRunForEveryItAndAfterEachFailureIsReported(t *testing.T) {
	var log []string
	suite := BuildSuite(nil, "suite", func(s *Spec) {
		s.BeforeEach(func(*Context) { log = append(log, "BE") })
		s.AfterEach(func(ctx *Context) { log = append(log, "AE"); EqualTo(ctx, 1, 2) })
		s.It("a", func(*Context) { log = append(log, "a") })
		s.When("child", func(w *Spec) {
			w.BeforeEach(func(*Context) { log = append(log, "cBE") })
			w.It("b", func(*Context) { log = append(log, "b") })
		})
	})
	rep := &recordingReporter{}
	suite.Reporter = rep
	suite.Run(failFastFakeTB{})
	if got, want := strings.Join(log, ","), "BE,a,AE,BE,cBE,b,AE"; got != want {
		t.Fatalf("log = %q, want %q", got, want)
	}
	for _, e := range rep.specFinished {
		if !e.Failed {
			t.Errorf("spec %q must fail through its AfterEach assertion", e.Name)
		}
	}
}

func TestBuilderHookAfterSpecOrScopePanics(t *testing.T) {
	for _, method := range []string{"BeforeEach", "AfterEach"} {
		hook := func(b *Builder) {
			if method == "BeforeEach" {
				b.BeforeEach(func(*Context) {})
			} else {
				b.AfterEach(func(*Context) {})
			}
		}
		t.Run(method+"/after It", func(t *testing.T) {
			requireLateHookPanic(t, method, func() {
				b := NewBuilder()
				b.It("a", func(*Context) {})
				hook(b)
			})
		})
		t.Run(method+"/after nested Describe", func(t *testing.T) {
			requireLateHookPanic(t, method, func() {
				b := NewBuilder()
				b.Describe("outer", func() {
					b.Describe("child", func() { b.It("a", func(*Context) {}) })
					hook(b)
				})
			})
		})
	}
}

func TestBuilderHooksBeforeSpecsRunForEveryItAndAfterEachFailureIsReported(t *testing.T) {
	var log []string
	b := NewBuilder()
	b.Describe("outer", func() {
		b.BeforeEach(func(*Context) { log = append(log, "BE") })
		b.AfterEach(func(ctx *Context) { log = append(log, "AE"); EqualTo(ctx, 1, 2) })
		b.It("a", func(*Context) { log = append(log, "a") })
		b.Describe("child", func() {
			b.BeforeEach(func(*Context) { log = append(log, "cBE") })
			b.It("b", func(*Context) { log = append(log, "b") })
		})
	})
	rep := &recordingReporter{}
	NewRunnerWithReporter(b.Build(), "suite", rep).Run(failFastFakeTB{})
	if got, want := strings.Join(log, ","), "BE,a,AE,BE,cBE,b,AE"; got != want {
		t.Fatalf("log = %q, want %q", got, want)
	}
	for _, e := range rep.specFinished {
		if !e.Failed {
			t.Errorf("spec %q must fail through its AfterEach assertion", e.Name)
		}
	}
}
