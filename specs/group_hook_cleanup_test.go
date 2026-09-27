package specs

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// groupHookCleanupCases registers, from a BeforeAll or an AfterAll, a cleanup in the idiomatic
// form: it reads ctx.T and ctx inside the cleanup instead of capturing them first. testing runs the
// group subtest's Cleanup functions only when that subtest ends, after the group's specs and
// AfterAlls, so these pin that a hook's Context stays bound to its own group subtest until then
// (#256). Before the fix the hook's Context was back in the pool by then: ctx.T was nil and the
// binary crashed, and ctx.Expect went through a backend already handed back to its pool.
//
// Each cleanup first prints the subtest ctx.T names, so a Context that was handed to one of the
// group's specs in the meantime shows up as the wrong name instead of passing silently.
var groupHookCleanupCases = map[string]func(s *Spec){
	"before-all-ctx-T-error": func(s *Spec) {
		s.BeforeAll(func(ctx *Context) {
			ctx.T.Cleanup(func() {
				printCleanupT(ctx)
				ctx.T.Error("boom from BeforeAll cleanup")
			})
		})
	},
	"after-all-ctx-T-error": func(s *Spec) {
		s.AfterAll(func(ctx *Context) {
			ctx.T.Cleanup(func() {
				printCleanupT(ctx)
				ctx.T.Error("boom from AfterAll cleanup")
			})
		})
	},
	"before-all-expect": func(s *Spec) {
		s.BeforeAll(func(ctx *Context) {
			ctx.T.Cleanup(func() {
				printCleanupT(ctx)
				ctx.Expect(1).ToEqual(2)
			})
		})
	},
	"after-all-expect": func(s *Spec) {
		s.AfterAll(func(ctx *Context) {
			ctx.T.Cleanup(func() {
				printCleanupT(ctx)
				ctx.Expect(1).ToEqual(2)
			})
		})
	},
}

func printCleanupT(ctx *Context) {
	name := "<nil>"
	if ctx.T != nil {
		name = ctx.T.Name()
	}
	fmt.Printf("CLEANUP_T=%s\n", name)
}

// TestGroupHookCleanupRunsAgainstTheGroupRealProcess proves #256: a cleanup a BeforeAll or AfterAll
// registers runs against the group's own subtest, fails the group, and is reported as the group's
// [AfterAll] case — the group's teardown — instead of crashing on a released Context.
//
// A subprocess, because the cleanup's real failure would otherwise fail this test itself, and
// because the bug being pinned crashes the whole test binary.
func TestGroupHookCleanupRunsAgainstTheGroupRealProcess(t *testing.T) {
	const helperEnv = "GO_SPECS_GROUP_HOOK_CLEANUP_HELPER"
	const testName = "TestGroupHookCleanupRunsAgainstTheGroupRealProcess"

	if name := os.Getenv(helperEnv); name != "" {
		var rep recordingReporter
		DescribeWithReporter(t, "suite", &rep, func(s *Spec) {
			groupHookCleanupCases[name](s)
			s.It("a", func(*Context) {})
			s.It("b", func(*Context) {})
		})
		for _, e := range rep.specFinished {
			fmt.Printf("SPEC_FINISHED name=%s failed=%t hook=%s\n", e.Name, e.Failed, e.Hook)
		}
		for _, e := range rep.suiteFinished {
			fmt.Printf("SUITE_FINISHED total=%d failed=%d\n", e.TotalSpecs, e.FailedSpecs)
		}
		return
	}

	want := []string{
		"CLEANUP_T=" + testName + "/suite",
		"SPEC_FINISHED name=a failed=false hook=",
		"SPEC_FINISHED name=b failed=false hook=",
		"SPEC_FINISHED name=[AfterAll] failed=true hook=AfterAll",
		"SUITE_FINISHED total=3 failed=1",
	}
	for name := range groupHookCleanupCases {
		t.Run(name, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^"+testName+"$")
			cmd.Env = append(os.Environ(), helperEnv+"="+name)
			output, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("expected the failing cleanup to fail the process, but it passed: %s", output)
			}
			if strings.Contains(string(output), "panic:") {
				t.Fatalf("expected the cleanup to run against the group subtest, but the binary panicked:\n%s", output)
			}
			var got []string
			for _, line := range strings.Split(string(output), "\n") {
				for _, prefix := range []string{"CLEANUP_T=", "SPEC_FINISHED ", "SUITE_FINISHED "} {
					if strings.HasPrefix(line, prefix) {
						got = append(got, line)
					}
				}
			}
			if strings.Join(got, "\n") != strings.Join(want, "\n") {
				t.Fatalf("got:\n%s\n\nwant:\n%s\n\nfull output:\n%s",
					strings.Join(got, "\n"), strings.Join(want, "\n"), output)
			}
			if !strings.Contains(string(output), "--- FAIL: "+testName+"/suite ") {
				t.Fatalf("expected the group subtest to fail, got:\n%s", output)
			}
		})
	}
}
