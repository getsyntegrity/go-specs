package specs

import (
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/getsyntegrity/go-specs/report"
)

// before_after_all_scope_test.go pins how once-per-group hooks interact with Go's own test tree
// (issue #207, docs/SUITE_HOOKS_CONTRACT.md H3, H5, H7 and "Hook lifetime"):
//
//   - `go test -run` can never filter a group's hooks independently of the specs they guard: a
//     group is entered exactly when one of its specs actually starts, and never otherwise;
//   - ctx.T inside a hook is the group's own subtest, so t.Cleanup/TempDir/Setenv registered in a
//     BeforeAll live through the group's AfterAll and end before the next sibling group runs;
//   - a hook failure is attributed to the group's subtest;
//   - none of this changes a spec's Go subtest name.
//
// The -run and failure scenarios need a real `go test` process — a filter is a process flag, and a
// deliberately failing hook would otherwise fail this test itself — so they re-run this test binary
// against groupHookScopeHelper, the same subprocess pattern execution_plan_isolation_test.go uses.

const groupHookScopeEnv = "GO_SPECS_GROUP_HOOK_SCOPE_SCENARIO"

// TestGroupHookScopeHelper is the child-process entry point; it does nothing in a normal run.
func TestGroupHookScopeHelper(t *testing.T) {
	switch os.Getenv(groupHookScopeEnv) {
	case "select":
		groupHookSelectScenario(t)
	case "lifetime":
		groupHookLifetimeScenario(t)
	case "failures":
		groupHookFailureScenario(t)
	case "parallel":
		Describe(t, "suite", func(s *Spec) {
			s.When("group", func(w *Spec) {
				w.BeforeAll(func(ctx *Context) { ctx.T.Parallel() })
				w.It("spec", func(*Context) {})
			})
			s.It("after", func(*Context) { fmt.Println("MARK after") })
		})
	case "spec-parallel":
		Describe(t, "suite", func(s *Spec) {
			s.When("outer", func(o *Spec) {
				o.BeforeAll(func(*Context) {})
				o.When("inner", func(w *Spec) {
					w.BeforeAll(func(*Context) {})
					w.It("parks", func(ctx *Context) { ctx.T.Parallel() })
				})
				o.It("after in outer", func(*Context) { fmt.Println("MARK after in outer") })
			})
			s.It("after", func(*Context) { fmt.Println("MARK after") })
		})
	}
}

// TestSpecBodyParallelInsideHookedGroupsStopsTheWholeRun proves the existing ctx.T.Parallel() guard
// for spec bodies (spec_body_parallel.go) still stops the whole run when the spec sits inside nested
// group subtests, not just the innermost group.
func TestSpecBodyParallelInsideHookedGroupsStopsTheWholeRun(t *testing.T) {
	out, ok := runGroupHookScopeHelper(t, "spec-parallel", "")
	if ok {
		t.Fatalf("child process passed, want ctx.T.Parallel() in a spec body to fail it:\n%s", out)
	}
	if !strings.Contains(out, "ctx.T.Parallel() is not supported in the sequential spec") {
		t.Fatalf("missing the unsupported-Parallel diagnostic:\n%s", out)
	}
	if got := marks(out); len(got) != 0 {
		t.Fatalf("the run continued after the unsupported Parallel call: %q\n%s", got, out)
	}
}

// TestGroupHookParallelStopsTheRun proves ctx.T.Parallel() inside a group hook — where ctx.T is the
// group's own subtest — is detected and stops the run with a diagnostic, the same way it already is
// inside a sequential spec body (spec_body_parallel.go), instead of silently detaching the group's
// hooks from its specs.
func TestGroupHookParallelStopsTheRun(t *testing.T) {
	out, ok := runGroupHookScopeHelper(t, "parallel", "")
	if ok {
		t.Fatalf("child process passed, want ctx.T.Parallel() in a BeforeAll to fail it:\n%s", out)
	}
	if !strings.Contains(out, "ctx.T.Parallel() is not supported in a BeforeAll/AfterAll hook") {
		t.Fatalf("missing the unsupported-Parallel diagnostic:\n%s", out)
	}
	if slices.Contains(marks(out), "after") {
		t.Fatalf("the run continued after the unsupported Parallel call:\n%s", out)
	}
}

// runGroupHookScopeHelper runs TestGroupHookScopeHelper in a child process with scenario and an
// extra -test.run sub-pattern, returning its combined output and whether it exited successfully.
func runGroupHookScopeHelper(t *testing.T, scenario, subPattern string) (string, bool) {
	t.Helper()
	run := "^TestGroupHookScopeHelper$"
	if subPattern != "" {
		run += "/" + subPattern
	}
	cmd := exec.Command(os.Args[0], "-test.run="+run, "-test.v", "-test.count=1")
	cmd.Env = append(os.Environ(), groupHookScopeEnv+"="+scenario)
	out, err := cmd.CombinedOutput()
	return string(out), err == nil
}

func groupHookSelectScenario(t *testing.T) {
	Describe(t, "suite", func(s *Spec) {
		s.When("group", func(w *Spec) {
			var setUp bool
			w.BeforeAll(func(*Context) { setUp = true; fmt.Println("MARK before-all") })
			w.AfterAll(func(*Context) { fmt.Println("MARK after-all") })
			w.It("selected", func(*Context) { fmt.Printf("MARK spec selected setUp=%v\n", setUp) })
			w.It("other", func(*Context) { fmt.Println("MARK spec other") })
		})
		s.It("outside", func(*Context) { fmt.Println("MARK spec outside") })
	})
}

// marks returns the "MARK ..." lines of out, in order.
func marks(out string) []string {
	var got []string
	for _, line := range strings.Split(out, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "MARK "); ok {
			got = append(got, rest)
		}
	}
	return got
}

// TestRunFilterSelectingOneSpecInsideAHookedGroupStillRunsItsHooks is regression (a): selecting a
// single spec with a hierarchical -run pattern must still run the group's BeforeAll exactly once
// before it and the AfterAll after it. When each hook was its own sibling subtest
// ("suite/group/[BeforeAll]"), ^selected$ filtered the hook out and the spec ran without setup.
func TestRunFilterSelectingOneSpecInsideAHookedGroupStillRunsItsHooks(t *testing.T) {
	out, ok := runGroupHookScopeHelper(t, "select", "^suite$/^group$/^selected$")
	if !ok {
		t.Fatalf("child process failed:\n%s", out)
	}
	want := []string{"before-all", "spec selected setUp=true", "after-all"}
	if got := marks(out); !slices.Equal(got, want) {
		t.Fatalf("marks = %q, want %q\n%s", got, want, out)
	}
}

// TestRunFilterSelectingOnlySpecsOutsideAHookedGroupNeverRunsItsHooks is regression (b): a group
// none of whose specs is selected is never entered (H3).
func TestRunFilterSelectingOnlySpecsOutsideAHookedGroupNeverRunsItsHooks(t *testing.T) {
	out, ok := runGroupHookScopeHelper(t, "select", "^suite$/^outside$")
	if !ok {
		t.Fatalf("child process failed:\n%s", out)
	}
	if got, want := marks(out), []string{"spec outside"}; !slices.Equal(got, want) {
		t.Fatalf("marks = %q, want %q\n%s", got, want, out)
	}
}

const groupHookScopeVar = "GO_SPECS_GROUP_HOOK_SCOPE_VAR"

func groupHookLifetimeScenario(t *testing.T) {
	Describe(t, "suite", func(s *Spec) {
		s.When("fixture", func(w *Spec) {
			var dir string
			w.BeforeAll(func(ctx *Context) {
				dir = ctx.T.TempDir()
				ctx.T.Setenv(groupHookScopeVar, "set")
				ctx.T.Cleanup(func() {
					_, err := os.Stat(dir)
					fmt.Printf("MARK cleanup dirExists=%v\n", err == nil)
				})
			})
			w.AfterAll(func(*Context) {
				_, err := os.Stat(dir)
				fmt.Printf("MARK after-all dirExists=%v env=%q\n", err == nil, os.Getenv(groupHookScopeVar))
			})
			for _, name := range []string{"a", "b"} {
				w.It(name, func(*Context) {
					_, err := os.Stat(dir)
					fmt.Printf("MARK spec %s dirExists=%v env=%q\n", name, err == nil, os.Getenv(groupHookScopeVar))
				})
			}
		})
		s.When("sibling", func(w *Spec) {
			w.BeforeAll(func(*Context) {})
			w.It("c", func(*Context) { fmt.Printf("MARK sibling env=%q\n", os.Getenv(groupHookScopeVar)) })
		})
	})
}

// TestBeforeAllCleanupTempDirAndSetenvLiveThroughTheGroup is regression (c): resources a BeforeAll
// registers through ctx.T stay alive for every spec of the group and its AfterAll, are released
// right after the AfterAll, and do not leak into a sibling group. When ctx.T was the hook's own
// short-lived subtest, all three were torn down before the group's first spec ran.
func TestBeforeAllCleanupTempDirAndSetenvLiveThroughTheGroup(t *testing.T) {
	out, ok := runGroupHookScopeHelper(t, "lifetime", "")
	if !ok {
		t.Fatalf("child process failed:\n%s", out)
	}
	want := []string{
		`spec a dirExists=true env="set"`,
		`spec b dirExists=true env="set"`,
		`after-all dirExists=true env="set"`,
		`cleanup dirExists=true`,
		`sibling env=""`,
	}
	if got := marks(out); !slices.Equal(got, want) {
		t.Fatalf("marks = %q, want %q\n%s", got, want, out)
	}
}

// printingReporter prints every SpecFinished event as a MARK line, so the child's structured
// results can be read back by the parent.
type printingReporter struct{}

func (printingReporter) SuiteStarted(report.SuiteStartEvent) {}
func (printingReporter) SuiteFinished(report.SuiteEndEvent)  {}
func (printingReporter) SpecStarted(report.SpecStartEvent)   {}
func (printingReporter) SpecFinished(e report.SpecResultEvent) {
	switch {
	case e.Hook != report.HookNone:
		fmt.Printf("MARK case %s %s failed=%v\n", strings.Join(e.Path, "/"), e.Name, e.Failed)
	case e.Skipped:
		fmt.Printf("MARK skipped %s\n", strings.Join(e.Path, "/"))
	default:
		fmt.Printf("MARK result %s failed=%v\n", strings.Join(e.Path, "/"), e.Failed)
	}
}

func groupHookFailureScenario(t *testing.T) {
	failures := []struct {
		name string
		fail func(*Context)
	}{
		{"assert", func(ctx *Context) { ctx.Expect(false).To(BeTrue()) }},
		{"fatal", func(ctx *Context) { ctx.T.Fatal("boom from ctx.T.Fatal") }},
		{"failnow", func(ctx *Context) { ctx.T.FailNow() }},
		{"panic", func(*Context) { panic("boom from BeforeAll") }},
	}
	DescribeWithReporter(t, "suite", printingReporter{}, func(s *Spec) {
		for _, f := range failures {
			s.When(f.name, func(w *Spec) {
				w.BeforeAll(f.fail)
				w.AfterAll(func(*Context) { fmt.Printf("MARK after-all %s\n", f.name) })
				w.It("first", func(*Context) { fmt.Printf("MARK body %s first\n", f.name) })
				w.When("nested", func(n *Spec) {
					n.BeforeAll(func(*Context) { fmt.Printf("MARK nested before-all %s\n", f.name) })
					n.It("second", func(*Context) { fmt.Printf("MARK body %s second\n", f.name) })
				})
			})
		}
		s.When("healthy", func(w *Spec) {
			w.BeforeAll(func(*Context) {})
			w.It("runs", func(*Context) {})
		})
	})
}

// TestBeforeAllFailuresAreAttributedToTheGroupSubtest is regression (d): an assertion failure, a
// direct ctx.T.Fatal, a direct ctx.T.FailNow and a panic in a BeforeAll each produce exactly one [BeforeAll]
// case, skip every descendant, still run the group's AfterAll, leave the sibling group untouched,
// and fail `go test` on the group's own subtest — not on a separate "[BeforeAll]" subtest.
func TestBeforeAllFailuresAreAttributedToTheGroupSubtest(t *testing.T) {
	out, ok := runGroupHookScopeHelper(t, "failures", "")
	if ok {
		t.Fatalf("child process passed, want it to fail on the failing BeforeAlls:\n%s", out)
	}
	var want []string
	for _, name := range []string{"assert", "fatal", "failnow", "panic"} {
		want = append(want,
			"case suite/"+name+" [BeforeAll] failed=true",
			"skipped suite/"+name+"/first",
			"skipped suite/"+name+"/nested/second",
			"after-all "+name,
		)
	}
	want = append(want, "result suite/healthy/runs failed=false")
	if got := marks(out); !slices.Equal(got, want) {
		t.Fatalf("marks =\n  %q\nwant\n  %q\n%s", got, want, out)
	}
	for _, name := range []string{"assert", "fatal", "failnow", "panic"} {
		if !strings.Contains(out, "--- FAIL: TestGroupHookScopeHelper/suite/"+name+" (") {
			t.Fatalf("go test did not report the %q group's subtest as failed:\n%s", name, out)
		}
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "=== RUN") && strings.Contains(line, "[") {
			t.Fatalf("a hook still runs as its own subtest (%q):\n%s", line, out)
		}
	}
	if !strings.Contains(out, "--- PASS: TestGroupHookScopeHelper/suite/healthy/runs (") {
		t.Fatalf("the sibling group's spec did not pass:\n%s", out)
	}
}

// subtestIdentitySuite declares the same tree with or without group hooks on every scope. The names
// deliberately include the shapes that make nested subtests diverge from one flat subtest name:
// spaces, a "/" inside a name, duplicate sibling group names, an empty spec name, and a spec whose
// breadcrumb equals a hooked group's.
func subtestIdentitySuite(hooks bool, names *[]string) func(*Spec) {
	record := func(ctx *Context) { *names = append(*names, ctx.T.Name()) }
	hook := func(s *Spec) {
		if hooks {
			s.BeforeAll(func(*Context) {})
			s.AfterAll(func(*Context) {})
		}
	}
	return func(s *Spec) {
		hook(s)
		s.It("root spec", record)
		s.It("dup", record)
		s.When("dup", func(w *Spec) {
			hook(w)
			w.It("leaf", record)
		})
		s.When("with space", func(w *Spec) {
			hook(w)
			w.It("leaf", record)
			w.It("", record)
			w.When("inner/slash", func(n *Spec) {
				hook(n)
				n.It("leaf", record)
			})
		})
		s.When("twin", func(w *Spec) {
			hook(w)
			w.It("leaf", record)
		})
		s.When("twin", func(w *Spec) {
			hook(w)
			w.It("leaf", record)
		})
		s.It("dup", record)
	}
}

// TestGroupHooksDoNotChangeSpecSubtestNames proves giving hooked groups their own subtest leaves
// every spec's Go subtest name byte-identical to the flat, hook-free run, so -run patterns and the
// #102 breadcrumb guarantees are unchanged.
func TestGroupHooksDoNotChangeSpecSubtestNames(t *testing.T) {
	var plain, hooked []string
	t.Run("run", func(t *testing.T) { Describe(t, "suite", subtestIdentitySuite(false, &plain)) })
	t.Run("hooked", func(t *testing.T) { Describe(t, "suite", subtestIdentitySuite(true, &hooked)) })

	strip := func(names []string, prefix string) []string {
		out := make([]string, len(names))
		for i, n := range names {
			out[i] = strings.TrimPrefix(n, prefix)
		}
		return out
	}
	got := strip(hooked, t.Name()+"/hooked/")
	want := strip(plain, t.Name()+"/run/")
	if !slices.Equal(got, want) {
		t.Fatalf("spec subtest names with group hooks:\n  %q\nwithout:\n  %q", got, want)
	}
	if len(want) != 9 {
		t.Fatalf("recorded %d spec names, want 9: %q", len(want), want)
	}
}
