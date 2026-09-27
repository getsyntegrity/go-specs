package specs

import (
	"fmt"
	"strings"
	"testing"
)

// group_hook_validation_test.go pins the construction-time rule for hooked groups (issue #207,
// docs/SUITE_HOOKS_CONTRACT.md H1/H7): a group that registers BeforeAll or AfterAll runs as its own
// Go subtest, so it must have an explicit, non-empty name that is unique — the way go test names
// subtests — among the specs and groups declared next to it. Every shape that would break that is
// rejected while the suite is built, before any spec runs, with a panic naming the group — the same
// convention requireBuildTarget uses for a Spec that cannot accept a registration.

// invalidHookedGroupShapes are the suites that must be rejected, with a fragment the panic message
// must contain to identify the offending group and the reason.
var invalidHookedGroupShapes = []struct {
	name   string
	build  func(*Spec)
	expect []string
}{
	{
		name: "empty group name",
		build: func(s *Spec) {
			s.When("", func(w *Spec) {
				w.BeforeAll(func(*Context) {})
				w.It("spec", func(*Context) {})
			})
		},
		expect: []string{`["suite" ""]`, "empty name"},
	},
	{
		name: "It with an empty name directly inside",
		build: func(s *Spec) {
			s.When("group", func(w *Spec) {
				w.AfterAll(func(*Context) {})
				w.It("", func(*Context) {})
			})
		},
		expect: []string{`["suite" "group"]`, "It with an empty name"},
	},
	{
		name: "two sibling hooked groups with the same name",
		build: func(s *Spec) {
			for range 2 {
				s.When("twin", func(w *Spec) {
					w.BeforeAll(func(*Context) {})
					w.It("spec", func(*Context) {})
				})
			}
		},
		expect: []string{`["suite" "twin"]`, "another group"},
	},
	{
		name: "a sibling spec with the same name",
		build: func(s *Spec) {
			s.It("dup", func(*Context) {})
			s.When("dup", func(w *Spec) {
				w.BeforeAll(func(*Context) {})
				w.It("spec", func(*Context) {})
			})
		},
		expect: []string{`["suite" "dup"]`, "a spec"},
	},
	{
		name: "names equal once go test normalizes them",
		build: func(s *Spec) {
			s.It("sp_ace", func(*Context) {})
			s.When("sp ace", func(w *Spec) {
				w.BeforeAll(func(*Context) {})
				w.It("spec", func(*Context) {})
			})
		},
		expect: []string{`["suite" "sp ace"]`, "sp_ace"},
	},
	{
		name: "a slash name that equals a nested hooked group",
		build: func(s *Spec) {
			s.When("a/b", func(w *Spec) {
				w.BeforeAll(func(*Context) {})
				w.It("spec", func(*Context) {})
			})
			s.When("a", func(w *Spec) {
				w.When("b", func(n *Spec) {
					n.AfterAll(func(*Context) {})
					n.It("spec", func(*Context) {})
				})
			})
		},
		expect: []string{`["suite" "a" "b"]`, "another group"},
	},
	{
		name: "empty root Describe name",
		build: func(s *Spec) {
			s.BeforeAll(func(*Context) {})
			s.It("spec", func(*Context) {})
		},
		expect: []string{`[""]`, "empty name"},
	},
}

// hookedGroupEntryPoints are every way a suite gets built; each must reject the shapes above before
// any spec runs.
var hookedGroupEntryPoints = []struct {
	name  string
	build func(t *testing.T, suiteName string, fn func(*Spec))
}{
	{"Describe", func(t *testing.T, n string, fn func(*Spec)) { Describe(t, n, fn) }},
	{"DescribeWithReporter", func(t *testing.T, n string, fn func(*Spec)) {
		DescribeWithReporter(t, n, &recordingReporter{}, fn)
	}},
	{"BuildSuite", func(_ *testing.T, n string, fn func(*Spec)) { BuildSuite(nil, n, fn) }},
	{"Analyze+Describe", func(t *testing.T, n string, fn func(*Spec)) { Analyze(func() { Describe(t, n, fn) }) }},
	{"Analyze+BuildSuite", func(_ *testing.T, n string, fn func(*Spec)) { Analyze(func() { BuildSuite(nil, n, fn) }) }},
}

func TestHookedGroupsMustHaveAUniqueNonEmptyName(t *testing.T) {
	for _, entry := range hookedGroupEntryPoints {
		for _, shape := range invalidHookedGroupShapes {
			t.Run(entry.name+"/"+shape.name, func(t *testing.T) {
				suiteName := "suite"
				if shape.name == "empty root Describe name" {
					suiteName = ""
				}
				var ran bool
				body := func(s *Spec) {
					shape.build(s)
					s.When("canary", func(w *Spec) { w.It("canary", func(*Context) { ran = true }) })
				}
				msg := panicMessage(func() { entry.build(t, suiteName, body) })
				if msg == "" {
					t.Fatalf("%s accepted an invalid hooked group (%s); want a construction-time panic", entry.name, shape.name)
				}
				if ran {
					t.Fatalf("a spec ran before the invalid hooked group was rejected")
				}
				for _, want := range append([]string{"BeforeAll/AfterAll", "its own Go subtest", "unique"}, shape.expect...) {
					if !strings.Contains(msg, want) {
						t.Fatalf("panic message does not contain %q:\n%s", want, msg)
					}
				}
			})
		}
	}
}

// TestGroupsWithoutHooksKeepEveryName proves the rule applies only to hooked groups: the same
// shapes without BeforeAll/AfterAll build and run exactly as before.
func TestGroupsWithoutHooksKeepEveryName(t *testing.T) {
	var ran int
	Describe(t, "", func(s *Spec) {
		s.When("", func(w *Spec) { w.It("", func(*Context) { ran++ }) })
		s.When("twin", func(w *Spec) { w.It("spec", func(*Context) { ran++ }) })
		s.When("twin", func(w *Spec) { w.It("spec", func(*Context) { ran++ }) })
		s.It("dup", func(*Context) { ran++ })
		s.When("dup", func(w *Spec) { w.It("spec", func(*Context) { ran++ }) })
	})
	if ran != 5 {
		t.Fatalf("ran %d specs, want 5", ran)
	}
}

func panicMessage(fn func()) (msg string) {
	defer func() {
		if r := recover(); r != nil {
			msg = fmt.Sprint(r)
		}
	}()
	fn()
	return ""
}
