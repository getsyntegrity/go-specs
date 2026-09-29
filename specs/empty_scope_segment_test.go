// empty_scope_segment_test.go pins that an empty Describe/When name adds no segment to the Go subtest
// name, so Describe(t, "", fn) yields TestX/<case> and `go test -run 'TestX/<case>'` selects it,
// while a non-empty name stays a segment exactly as before. Each case re-executes this test binary
// as a helper under a real -test.run selector, because the selector is a process flag: only a real
// run can show which bodies ran and which were filtered out.
package specs

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/getsyntegrity/go-specs/report"
)

const emptyScopeSegmentEnv = "GO_SPECS_EMPTY_SCOPE_SEGMENT_HELPER"

const emptyScopeSegmentTest = "TestEmptyScopeSegment_RealProcess"

// emptyScopeScenario declares one suite shape inside the helper process. Bodies print "RAN <id>".
func emptyScopeScenario(t *testing.T, mode string, rep report.EventReporter) {
	ran := func(id string) func(*Context) { return func(*Context) { fmt.Printf("RAN %s\n", id) } }
	switch mode {
	case "spec-empty", "spec-named":
		root := ""
		if mode == "spec-named" {
			root = "root"
		}
		body := func(s *Spec) {
			s.It("absent is unconditional", ran("absent"))
			s.It("zero is genesis", ran("zero"))
			s.Describe("nested", func(s *Spec) { s.It("leaf", ran("leaf")) })
		}
		if rep != nil {
			describeWithCompiler(t, root, rep, body)
		} else {
			Describe(t, root, body)
		}
	case "spec-parallel":
		Describe(t, "", func(s *Spec) {
			s.ItParallel("par a", ran("par-a"))
			s.ItParallel("par b", ran("par-b"))
		})
	case "spec-nested-empty":
		Describe(t, "outer", func(s *Spec) {
			s.Describe("", func(s *Spec) { s.It("x", ran("x")) })
			s.When("", func(s *Spec) { s.When("", func(s *Spec) { s.It("y", ran("y")) }) })
		})
	case "spec-collision":
		Describe(t, "", func(s *Spec) {
			s.It("a", ran("root-a"))
			s.Describe("", func(s *Spec) { s.It("a", ran("scoped-a")) })
			s.Describe("", func(s *Spec) { s.It("a", ran("scoped-a2")) })
		})
	case "spec-hooked":
		// An unnamed root around a named, hooked group: the group keeps its subtest, and the empty
		// root adds nothing in front of it.
		Describe(t, "", func(s *Spec) {
			s.When("g", func(s *Spec) {
				s.BeforeAll(func(*Context) { fmt.Println("RAN before-all") })
				s.It("case", ran("case"))
			})
		})
	case "builder-empty":
		b := NewBuilder()
		b.Describe("", func() {
			b.It("absent is unconditional", ran("absent"))
			b.ItParallel("par a", ran("par-a"))
			b.Describe("nested", func() { b.It("leaf", ran("leaf")) })
		})
		NewRunnerWithReporter(b.Build(), "suite", rep).Run(t)
	case "builder-collision":
		b := NewBuilder()
		b.It("a", ran("root-a"))
		b.Describe("", func() { b.It("a", ran("scoped-a")) })
		b.Describe("", func() { b.It("a", ran("scoped-a2")) })
		NewRunner(b.Build()).Run(t)
	default:
		t.Fatalf("unknown mode %q", mode)
	}
}

// TestEmptyScopeSegment_RealProcess is both the driver (no env) and the helper (env set).
func TestEmptyScopeSegment_RealProcess(t *testing.T) {
	if mode := os.Getenv(emptyScopeSegmentEnv); mode != "" {
		var rep report.EventReporter
		if strings.HasSuffix(mode, "+report") {
			mode = strings.TrimSuffix(mode, "+report")
			rec := &recordingReporter{}
			defer func() {
				for _, e := range rec.specStarted {
					fmt.Printf("PATH %q\n", e.Path)
				}
			}()
			rep = rec
		}
		emptyScopeScenario(t, mode, rep)
		return
	}

	run := func(t *testing.T, mode, selector string) (ranIDs, subtests []string, out string) {
		t.Helper()
		cmd := exec.Command(os.Args[0], "-test.v", "-test.run="+selector)
		cmd.Env = append(os.Environ(), emptyScopeSegmentEnv+"="+mode)
		b, err := cmd.CombinedOutput()
		out = string(b)
		if err != nil {
			t.Fatalf("helper failed: %v\n%s", err, out)
		}
		for _, l := range strings.Split(out, "\n") {
			if id, ok := strings.CutPrefix(l, "RAN "); ok {
				ranIDs = append(ranIDs, id)
			}
			if m := regexp.MustCompile(`^\s*--- (?:PASS|FAIL|SKIP): (\S+)`).FindStringSubmatch(l); m != nil && m[1] != emptyScopeSegmentTest {
				subtests = append(subtests, m[1])
			}
		}
		sort.Strings(ranIDs)
		sort.Strings(subtests)
		return
	}
	const T = emptyScopeSegmentTest
	P := "^" + T + "$"

	cases := []struct {
		name     string
		mode     string
		selector string
		wantRan  []string
		wantSubs []string // nil: not asserted
	}{
		{"empty root: all", "spec-empty", P, []string{"absent", "leaf", "zero"},
			[]string{T + "/absent_is_unconditional", T + "/nested/leaf", T + "/zero_is_genesis"}},
		{"empty root: select top-level case", "spec-empty", P + "/^absent_is_unconditional$", []string{"absent"},
			[]string{T + "/absent_is_unconditional"}},
		{"empty root: select nested case", "spec-empty", P + "/^nested$/^leaf$", []string{"leaf"},
			[]string{T + "/nested/leaf"}},
		{"empty root: unmatched selector runs nothing", "spec-empty", P + "/^nomatch$", nil, nil},
		{"named root: names unchanged", "spec-named", P, []string{"absent", "leaf", "zero"},
			[]string{T + "/root/absent_is_unconditional", T + "/root/nested/leaf", T + "/root/zero_is_genesis"}},
		{"named root: select by full path", "spec-named", P + "/^root$/^absent_is_unconditional$", []string{"absent"}, nil},
		{"named root: select nested by full path", "spec-named", P + "/^root$/^nested$/^leaf$", []string{"leaf"}, nil},
		{"named root: old selector without root matches nothing", "spec-named", P + "/^absent_is_unconditional$", nil, nil},
		{"empty root ItParallel: all", "spec-parallel", P, []string{"par-a", "par-b"},
			[]string{T + "/par_a", T + "/par_b"}},
		{"empty root ItParallel: select one", "spec-parallel", P + "/^par_b$", []string{"par-b"}, nil},
		{"nested empty scopes vanish", "spec-nested-empty", P, []string{"x", "y"},
			[]string{T + "/outer/x", T + "/outer/y"}},
		{"nested empty scopes: select", "spec-nested-empty", P + "/^outer$/^y$", []string{"y"}, nil},
		{"hooked group under empty root", "spec-hooked", P, []string{"before-all", "case"},
			[]string{T + "/g", T + "/g/case"}},
		{"hooked group under empty root: select", "spec-hooked", P + "/^g$/^case$", []string{"before-all", "case"}, nil},
		{"builder empty describe: all", "builder-empty", P, []string{"absent", "leaf", "par-a"},
			[]string{T + "/absent_is_unconditional", T + "/nested/leaf", T + "/par_a"}},
		{"builder empty describe: select", "builder-empty", P + "/^absent_is_unconditional$", []string{"absent"}, nil},
		{"builder empty describe: select parallel", "builder-empty", P + "/^par_a$", []string{"par-a"}, nil},
		{"spec sibling collision is deterministic", "spec-collision", P, []string{"root-a", "scoped-a", "scoped-a2"},
			[]string{T + "/a", T + "/a#01", T + "/a#02"}},
		{"builder sibling collision is deterministic", "builder-collision", P, []string{"root-a", "scoped-a", "scoped-a2"},
			[]string{T + "/a", T + "/a#01", T + "/a#02"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ranIDs, subs, out := run(t, tc.mode, tc.selector)
			if !slices.Equal(ranIDs, tc.wantRan) {
				t.Fatalf("bodies ran %v, want %v\n%s", ranIDs, tc.wantRan, out)
			}
			if tc.wantSubs != nil && !slices.Equal(subs, tc.wantSubs) {
				t.Fatalf("subtests %v, want %v\n%s", subs, tc.wantSubs, out)
			}
			for _, s := range subs {
				if strings.Contains(s, "//") {
					t.Fatalf("subtest %q has an empty segment", s)
				}
			}
		})
	}

	// The collision order is deterministic: the first declared spec keeps the bare name, and the
	// bodies are attributed to suffixes in declaration order.
	t.Run("collision suffixes follow declaration order", func(t *testing.T) {
		cmd := exec.Command(os.Args[0], "-test.v", "-test.run="+P+"/^a#01$")
		cmd.Env = append(os.Environ(), emptyScopeSegmentEnv+"=spec-collision")
		b, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("helper failed: %v\n%s", err, b)
		}
		if !strings.Contains(string(b), "RAN scoped-a\n") || strings.Contains(string(b), "RAN root-a") {
			t.Fatalf("a#01 must select the first empty-scoped spec only:\n%s", b)
		}
	})

	// Report paths carry no "" element for an empty scope.
	for _, tc := range []struct{ mode, want string }{
		{"spec-empty+report", `PATH ["absent is unconditional"]`},
		{"spec-empty+report", `PATH ["nested" "leaf"]`},
		{"builder-empty+report", `PATH ["absent is unconditional"]`},
		{"builder-empty+report", `PATH ["nested" "leaf"]`},
	} {
		t.Run("report path "+tc.mode+" "+tc.want, func(t *testing.T) {
			_, _, out := run(t, tc.mode, P)
			if !strings.Contains(out, tc.want+"\n") || strings.Contains(out, `PATH [""`) {
				t.Fatalf("want %s and no empty path element:\n%s", tc.want, out)
			}
		})
	}
}
