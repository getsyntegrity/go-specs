package specs

import (
	"strings"
	"testing"
)

// A *Spec captured from a Describe/When callback and used after that scope closed used to reach a
// released compiler (nil plan) and die with a nil dereference, or, worse, write into a pooled
// compiler already reused by another suite (issue #317). Every registration method now panics with
// an actionable message naming itself.

func requireClosedPanic(t *testing.T, method string, fn func()) {
	t.Helper()
	msg := recoverMessage(t, fn)
	for _, want := range []string{"specs:", "Spec." + method, "closed"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("panic %q must contain %q", msg, want)
		}
	}
}

func TestSpecRegistrationAfterScopeClosedPanics(t *testing.T) {
	builds := map[string]func(fn func(*Spec)){
		"compiler": func(fn func(*Spec)) { BuildSuite(nil, "suite", fn) },
		"registry": func(fn func(*Spec)) { Analyze(func() { Describe(nil, "suite", fn) }) },
	}
	calls := map[string]func(s *Spec){
		"It":         func(s *Spec) { s.It("late", func(*Context) {}) },
		"SkipIt":     func(s *Spec) { s.SkipIt("late", nil) },
		"PendingIt":  func(s *Spec) { s.PendingIt("late", nil) },
		"FIt":        func(s *Spec) { s.FIt("late", func(*Context) {}) },
		"ItParallel": func(s *Spec) { s.ItParallel("late", func(*Context) {}) },
		"BeforeEach": func(s *Spec) { s.BeforeEach(func(*Context) {}) },
		"AfterEach":  func(s *Spec) { s.AfterEach(func(*Context) {}) },
		"BeforeAll":  func(s *Spec) { s.BeforeAll(func(*Context) {}) },
		"AfterAll":   func(s *Spec) { s.AfterAll(func(*Context) {}) },
		"Describe":   func(s *Spec) { s.Describe("late", func(*Spec) {}) },
		"When":       func(s *Spec) { s.When("late", func(*Spec) {}) },
	}
	for buildName, build := range builds {
		for method, call := range calls {
			t.Run(buildName+"/top-level/"+method, func(t *testing.T) {
				var captured *Spec
				build(func(s *Spec) { captured = s; s.It("ok", func(*Context) {}) })
				requireClosedPanic(t, method, func() { call(captured) })
			})
			t.Run(buildName+"/nested/"+method, func(t *testing.T) {
				var nested *Spec
				build(func(s *Spec) {
					s.When("child", func(w *Spec) { nested = w; w.It("ok", func(*Context) {}) })
				})
				requireClosedPanic(t, method, func() { call(nested) })
			})
		}
	}
}

func TestSpecRegistrationFromSpecBodyPanics(t *testing.T) {
	var captured *Spec
	var msg string
	suite := BuildSuite(nil, "suite", func(s *Spec) {
		captured = s
		s.It("registers late", func(*Context) {
			defer func() { msg = recoverString(recover()) }()
			captured.It("from body", func(*Context) {})
		})
	})
	suite.Run(failFastFakeTB{})
	for _, want := range []string{"specs:", "Spec.It", "closed"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("panic %q must contain %q", msg, want)
		}
	}
}

func recoverString(r any) string {
	if r == nil {
		return ""
	}
	if s, ok := r.(string); ok {
		return s
	}
	return "non-string panic"
}

// A stale handle must not write into a pooled compiler that another suite has since acquired.
func TestClosedSpecDoesNotWriteIntoReusedCompiler(t *testing.T) {
	var stale *Spec
	BuildSuite(nil, "first", func(s *Spec) { stale = s; s.It("ok", func(*Context) {}) })
	BuildSuite(nil, "second", func(s *Spec) {
		s.It("one", func(*Context) {})
		requireClosedPanic(t, "It", func() { stale.It("intruder", func(*Context) {}) })
	})
}

// The open handle keeps working: nested handles stay valid while their own scope runs, and the
// zero-value diagnostic from #151 is unchanged.
func TestOpenSpecStillRegisters(t *testing.T) {
	suite := BuildSuite(nil, "suite", func(s *Spec) {
		s.When("child", func(w *Spec) { w.It("a", func(*Context) {}) })
		s.It("b", func(*Context) {})
	})
	if got := len(suite.Plan.Names); got != 2 {
		t.Fatalf("specs = %d, want 2", got)
	}
	msg := recoverMessage(t, func() { (&Spec{}).It("x", func(*Context) {}) })
	if !strings.Contains(msg, "no build target") {
		t.Fatalf("zero-value diagnostic changed: %q", msg)
	}
}
