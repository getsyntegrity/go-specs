package specs

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestJoinSubtestPath pins the package-internal mapping from a declared Describe/When/It breadcrumb
// to the Go subtest identity go-specs hands testing.T.Run (#102): a plain join on "/", never a
// rewrite. It exercises joinSubtestPath because that is the function both compilers actually call —
// compiler.fullName and Builder.fullName — so the contract is pinned on the executed code path. The
// cases for spaces, slashes and empty names document what the mapping itself does; what testing then
// makes of those names is proven end-to-end by the real-process tests below.
func TestJoinSubtestPath(t *testing.T) {
	cases := []struct {
		name   string
		scopes []string
		leaf   string
		want   string
	}{
		{name: "nested breadcrumb", scopes: []string{"Cart", "when empty"}, leaf: "has no items", want: "Cart/when empty/has no items"},
		{name: "leaf only", scopes: nil, leaf: "has no items", want: "has no items"},
		{name: "no path at all", scopes: nil, leaf: "", want: ""},
		{name: "spaces are preserved by the mapping", scopes: []string{"a b"}, leaf: "c d", want: "a b/c d"},
		{name: "a slash inside a name is not escaped", scopes: []string{"D"}, leaf: "a/b", want: "D/a/b"},
		{name: "an empty leaf keeps its element", scopes: []string{"D"}, leaf: "", want: "D/"},
		{name: "an empty scope keeps its element", scopes: []string{"D", ""}, leaf: "it", want: "D//it"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := joinSubtestPath(tc.scopes, tc.leaf); got != tc.want {
				t.Fatalf("joinSubtestPath(%q, %q) = %q, want %q", tc.scopes, tc.leaf, got, tc.want)
			}
		})
	}
}

// TestSpecSubtestNamePrefersFullName proves the Spec/ExecutionPlan model derives subtest identity
// from the breadcrumb, and still degrades to the leaf name for a plan built without one — a
// hand-built ExecutionPlan, exactly like the ones several tests in this package construct directly.
func TestSpecSubtestNamePrefersFullName(t *testing.T) {
	plan := &ExecutionPlan{
		Names:     []string{"leaf", "bare", "unnamed"},
		FullNames: []string{"Root/When/leaf", "", ""},
	}
	if got := specSubtestName(plan, 0); got != "Root/When/leaf" {
		t.Fatalf("expected the full breadcrumb, got %q", got)
	}
	if got := specSubtestName(plan, 1); got != "bare" {
		t.Fatalf("expected a fallback to the leaf name, got %q", got)
	}
	if got := specSubtestName(plan, 5); got != "" {
		t.Fatalf("expected an out-of-range index to be empty, got %q", got)
	}
}

// TestBuilderCapturesDescribeBreadcrumb proves Builder now records the Describe scope names it used
// to discard, so the Runner/Program model can give a spec the same identity the ExecutionPlan model
// does. group.names — the reported identity — must stay the bare leaf name.
func TestBuilderCapturesDescribeBreadcrumb(t *testing.T) {
	b := NewBuilder()
	b.Describe("Cart", func() {
		b.Describe("when empty", func() {
			b.It("has no items", func(*Context) {})
		})
		b.It("exists", func(*Context) {})
	})
	b.It("runs outside any Describe", func(*Context) {})

	program := b.Build()
	var names, fullNames []string
	for i := range program.Groups {
		g := &program.Groups[i]
		names = append(names, g.names...)
		for j := range g.specs {
			fullNames = append(fullNames, g.subtestName(j))
		}
	}
	wantNames := []string{"has no items", "exists", "runs outside any Describe"}
	wantFull := []string{"Cart/when empty/has no items", "Cart/exists", "runs outside any Describe"}
	if strings.Join(names, "|") != strings.Join(wantNames, "|") {
		t.Fatalf("reported names = %q, want %q", names, wantNames)
	}
	if strings.Join(fullNames, "|") != strings.Join(wantFull, "|") {
		t.Fatalf("subtest names = %q, want %q", fullNames, wantFull)
	}
}

// TestGroupSubtestNameFallsBackToLeaf proves a group built without breadcrumbs — every group
// hand-constructed in this package's older tests — still names its subtests, instead of collapsing
// to "" and letting testing number them.
func TestGroupSubtestNameFallsBackToLeaf(t *testing.T) {
	g := &group{specs: []step{func(*Context) {}}, names: []string{"leaf"}}
	if got := g.subtestName(0); got != "leaf" {
		t.Fatalf("expected a fallback to the leaf name, got %q", got)
	}
	if got := g.subtestName(3); got != "" {
		t.Fatalf("expected an out-of-range index to be empty, got %q", got)
	}
}

// identityHelperSuite declares the spec tree both real-process identity tests below run. It
// deliberately contains every shape acceptance criterion 4 calls out: the same leaf It name under
// two different When scopes (the duplicate that used to collapse into "#01"), a name with spaces, a
// name containing a slash, and an empty name.
func identityHelperSuite(s *Spec) {
	s.When("when a", func(s *Spec) {
		s.It("does a thing", func(*Context) { fmt.Println("RAN a/does a thing") })
	})
	s.When("when b", func(s *Spec) {
		s.It("does a thing", func(*Context) { fmt.Println("RAN b/does a thing") })
		s.It("slash/inside", func(*Context) { fmt.Println("RAN b/slash inside") })
		s.It("", func(*Context) { fmt.Println("RAN b/empty") })
	})
}

// identityHelperProgram is identityHelperSuite's Builder/Program equivalent, so both sequential
// execution models are proven against the same shapes (acceptance criterion 6).
func identityHelperProgram() *Program {
	b := NewBuilder()
	b.Describe("suite", func() {
		b.Describe("when a", func() {
			b.It("does a thing", func(*Context) { fmt.Println("RAN a/does a thing") })
		})
		b.Describe("when b", func() {
			b.It("does a thing", func(*Context) { fmt.Println("RAN b/does a thing") })
			b.It("slash/inside", func(*Context) { fmt.Println("RAN b/slash inside") })
			b.It("", func(*Context) { fmt.Println("RAN b/empty") })
		})
	})
	return b.Build()
}

// wantVerboseSubtests are the subtest names `go test -v` must print for either helper suite above,
// with testing's own rewrite applied: spaces become "_", a "/" inside a name adds a level, and an
// empty name leaves a trailing empty element. The two "does a thing" specs differ by their When
// scope alone — before #102 they were the same subtest name, told apart only by an incidental
// "#01" suffix.
var wantVerboseSubtests = []string{
	"/suite/when_a/does_a_thing",
	"/suite/when_b/does_a_thing",
	"/suite/when_b/slash/inside",
	"/suite/when_b/",
}

// assertVerboseIdentity checks a `go test -v` transcript names every spec by its full breadcrumb and
// never falls back to testing's duplicate-name numbering. Every breadcrumb in the helper suite
// normalizes to a distinct string, which is the condition the guarantee is stated over; the
// collision tests at the end of this file pin what happens when it does not hold.
func assertVerboseIdentity(t *testing.T, output string) {
	t.Helper()
	for _, want := range wantVerboseSubtests {
		if !strings.Contains(output, want) {
			t.Fatalf("expected -v output to name the subtest %q, got:\n%s", want, output)
		}
	}
	if strings.Contains(output, "#01") {
		t.Fatalf("expected no duplicate-name suffixing now that scopes disambiguate specs, got:\n%s", output)
	}
}

// TestSpecRunNestedSubtestIdentityRealProcess proves acceptance criteria 1, 2 and 4 for the
// Spec/ExecutionPlan model against a genuine `go test -v` run: every spec's subtest carries its full
// Describe/When/It breadcrumb, two specs sharing only a leaf name — and so normalizing to different
// breadcrumbs — stay distinct without "#01", and spaces, slashes and empty names land exactly where
// joinSubtestPath's contract says they do.
//
// A subprocess is required, not incidental: the assertions are about this process's own -v
// transcript, which only a child re-exec can produce and read back.
func TestSpecRunNestedSubtestIdentityRealProcess(t *testing.T) {
	if os.Getenv("GO_SPECS_SUBTEST_IDENTITY_HELPER") == "1" {
		Describe(t, "suite", identityHelperSuite)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.v", "-test.run=^TestSpecRunNestedSubtestIdentityRealProcess$")
	cmd.Env = append(os.Environ(), "GO_SPECS_SUBTEST_IDENTITY_HELPER=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helper run failed: %v\n%s", err, output)
	}
	assertVerboseIdentity(t, string(output))
}

// TestRunnerRunNestedSubtestIdentityRealProcess is TestSpecRunNestedSubtestIdentityRealProcess for
// the Runner/Program model: both sequential execution models obey one identity contract (acceptance
// criterion 6).
func TestRunnerRunNestedSubtestIdentityRealProcess(t *testing.T) {
	if os.Getenv("GO_SPECS_SUBTEST_IDENTITY_RUNNER_HELPER") == "1" {
		NewRunner(identityHelperProgram()).Run(t)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.v", "-test.run=^TestRunnerRunNestedSubtestIdentityRealProcess$")
	cmd.Env = append(os.Environ(), "GO_SPECS_SUBTEST_IDENTITY_RUNNER_HELPER=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helper run failed: %v\n%s", err, output)
	}
	assertVerboseIdentity(t, string(output))
}

// TestSpecRunSubtestSelectionByContextRealProcess proves acceptance criterion 3 for the
// Spec/ExecutionPlan model: `go test -run` selects one behaviour by its declared Describe/When/It
// context and its same-named sibling under another When does not run.
func TestSpecRunSubtestSelectionByContextRealProcess(t *testing.T) {
	if os.Getenv("GO_SPECS_SUBTEST_SELECT_HELPER") == "1" {
		Describe(t, "suite", identityHelperSuite)
		return
	}
	cmd := exec.Command(os.Args[0],
		"-test.v",
		"-test.run=^TestSpecRunSubtestSelectionByContextRealProcess$/^suite$/^when_b$/^does_a_thing$",
	)
	cmd.Env = append(os.Environ(), "GO_SPECS_SUBTEST_SELECT_HELPER=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helper run failed: %v\n%s", err, output)
	}
	assertSelectedOnlyWhenB(t, string(output))
}

// TestRunnerRunSubtestSelectionByContextRealProcess is the -run selection proof for the
// Runner/Program model.
func TestRunnerRunSubtestSelectionByContextRealProcess(t *testing.T) {
	if os.Getenv("GO_SPECS_SUBTEST_SELECT_RUNNER_HELPER") == "1" {
		NewRunner(identityHelperProgram()).Run(t)
		return
	}
	cmd := exec.Command(os.Args[0],
		"-test.v",
		"-test.run=^TestRunnerRunSubtestSelectionByContextRealProcess$/^suite$/^when_b$/^does_a_thing$",
	)
	cmd.Env = append(os.Environ(), "GO_SPECS_SUBTEST_SELECT_RUNNER_HELPER=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helper run failed: %v\n%s", err, output)
	}
	assertSelectedOnlyWhenB(t, string(output))
}

// assertSelectedOnlyWhenB checks a context-scoped -run pattern executed the "when b" spec body and
// left its identically-named "when a" sibling — and every other spec — unrun.
func assertSelectedOnlyWhenB(t *testing.T, output string) {
	t.Helper()
	if !strings.Contains(output, "RAN b/does a thing") {
		t.Fatalf("expected the selected spec's body to run, got:\n%s", output)
	}
	for _, unwanted := range []string{"RAN a/does a thing", "RAN b/slash inside", "RAN b/empty"} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("expected %q not to run under a context-scoped -run pattern, got:\n%s", unwanted, output)
		}
	}
}

// TestSpecRunHierarchicalSubtestsKeepReporterIdentityVerbatim proves acceptance criterion 5 for the
// now-hierarchical subtest names: the reporter still sees the framework's own leaf Name and its
// unsanitized Path, with no trace of what testing did to the subtest name. Its sibling in
// execution_plan_isolation_test.go covers the flat-name case; this one covers a nested tree with
// spaces, where testing's rewrite would be visible if it ever leaked.
func TestSpecRunHierarchicalSubtestsKeepReporterIdentityVerbatim(t *testing.T) {
	var rep recordingReporter
	DescribeWithReporter(t, "suite", &rep, func(s *Spec) {
		s.When("when a", func(s *Spec) {
			s.It("does a thing", func(*Context) {})
		})
	})
	if len(rep.specStarted) != 1 {
		t.Fatalf("expected 1 SpecStarted, got %d", len(rep.specStarted))
	}
	if got := rep.specStarted[0].Name; got != "does a thing" {
		t.Fatalf("expected the reported Name to stay the unsanitized leaf name, got %q", got)
	}
	wantPath := []string{"suite", "when a", "does a thing"}
	if strings.Join(rep.specStarted[0].Path, "|") != strings.Join(wantPath, "|") {
		t.Fatalf("expected the reported Path to stay %q, got %q", wantPath, rep.specStarted[0].Path)
	}
}

// collisionHelperSuite declares two pairs of specs whose declared breadcrumbs are different but
// whose *normalized* breadcrumbs — what testing.T.Run makes of them — are identical, so they are the
// exact shapes joinSubtestPath's contract says stay ambiguous:
//
//   - "suite/a/b/does it" reached through a single When("a/b") and through Describe("a")/When("b"),
//     because a "/" inside one declared name is not escaped and simply adds a pattern element;
//   - "suite/when c/does that" and "suite/when_c/does that", because testing rewrites spaces to "_".
//
// Each body prints a distinct marker, so a test can tell "both specs ran" (execution is never
// ambiguous) apart from "both specs are separately addressable" (identity is).
func collisionHelperSuite(s *Spec) {
	s.When("a/b", func(s *Spec) {
		s.It("does it", func(*Context) { fmt.Println("RAN slash-in-one-name") })
	})
	s.Describe("a", func(s *Spec) {
		s.When("b", func(s *Spec) {
			s.It("does it", func(*Context) { fmt.Println("RAN two-nested-scopes") })
		})
	})
	s.When("when c", func(s *Spec) {
		s.It("does that", func(*Context) { fmt.Println("RAN spaced-scope") })
	})
	s.When("when_c", func(s *Spec) {
		s.It("does that", func(*Context) { fmt.Println("RAN underscored-scope") })
	})
}

// collisionHelperProgram is collisionHelperSuite's Builder/Program equivalent, so the accepted
// ambiguity is pinned for both sequential execution models, exactly as the non-colliding identity
// contract already is.
func collisionHelperProgram() *Program {
	b := NewBuilder()
	b.Describe("suite", func() {
		b.Describe("a/b", func() {
			b.It("does it", func(*Context) { fmt.Println("RAN slash-in-one-name") })
		})
		b.Describe("a", func() {
			b.Describe("b", func() {
				b.It("does it", func(*Context) { fmt.Println("RAN two-nested-scopes") })
			})
		})
		b.Describe("when c", func() {
			b.It("does that", func(*Context) { fmt.Println("RAN spaced-scope") })
		})
		b.Describe("when_c", func() {
			b.It("does that", func(*Context) { fmt.Println("RAN underscored-scope") })
		})
	})
	return b.Build()
}

// assertNormalizedCollisionFallsBackToSuffix checks a `go test -v` transcript for the two accepted
// collisions: each colliding pair is named once plainly and once with testing's own "#01" suffix,
// and every body still ran. It asserts the observable consequence rather than the mapping's output,
// because the consequence — the "#01" — is what a developer actually meets.
func assertNormalizedCollisionFallsBackToSuffix(t *testing.T, output string) {
	t.Helper()
	for _, want := range []string{
		"/suite/a/b/does_it",
		"/suite/a/b/does_it#01",
		"/suite/when_c/does_that",
		"/suite/when_c/does_that#01",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected -v output to name the subtest %q, got:\n%s", want, output)
		}
	}
	for _, want := range []string{
		"RAN slash-in-one-name",
		"RAN two-nested-scopes",
		"RAN spaced-scope",
		"RAN underscored-scope",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected every colliding spec to still run, missing %q in:\n%s", want, output)
		}
	}
}

// TestSpecRunNormalizedBreadcrumbCollisionRealProcess pins the limitation go-specs accepts for the
// Spec/ExecutionPlan model: the identity guarantee holds for breadcrumbs that *normalize* to
// different strings, and two specs whose breadcrumbs normalize to the same string are told apart
// only by testing's own "#01" suffix — the same ambiguity a bare testing.T.Run has, since t.Run("a/b")
// and a nested t.Run("a")/t.Run("b") are indistinguishable there too. This is pinned as accepted
// behaviour, not as a known defect: escaping "/" or " " would remove the collision but break the
// readable `go test -run 'TestX/suite/when_a/does_it'` pattern the mapping exists to provide.
//
// A subprocess is required, not incidental: the claim is about this process's own -v transcript,
// which only a child re-exec can produce and read back.
func TestSpecRunNormalizedBreadcrumbCollisionRealProcess(t *testing.T) {
	if os.Getenv("GO_SPECS_SUBTEST_COLLISION_HELPER") == "1" {
		Describe(t, "suite", collisionHelperSuite)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.v", "-test.run=^TestSpecRunNormalizedBreadcrumbCollisionRealProcess$")
	cmd.Env = append(os.Environ(), "GO_SPECS_SUBTEST_COLLISION_HELPER=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helper run failed: %v\n%s", err, output)
	}
	assertNormalizedCollisionFallsBackToSuffix(t, string(output))
}

// TestRunnerRunNormalizedBreadcrumbCollisionRealProcess is
// TestSpecRunNormalizedBreadcrumbCollisionRealProcess for the Runner/Program model: both sequential
// execution models share one mapping, so both accept the same inherited ambiguity.
func TestRunnerRunNormalizedBreadcrumbCollisionRealProcess(t *testing.T) {
	if os.Getenv("GO_SPECS_SUBTEST_COLLISION_RUNNER_HELPER") == "1" {
		NewRunner(collisionHelperProgram()).Run(t)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.v", "-test.run=^TestRunnerRunNormalizedBreadcrumbCollisionRealProcess$")
	cmd.Env = append(os.Environ(), "GO_SPECS_SUBTEST_COLLISION_RUNNER_HELPER=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helper run failed: %v\n%s", err, output)
	}
	assertNormalizedCollisionFallsBackToSuffix(t, string(output))
}
