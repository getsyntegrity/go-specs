package specs

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/getsyntegrity/go-specs/report"
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
//
// Each scope's BeforeEach/AfterEach prints its own marker so the selection tests can observe not
// just which spec bodies ran under a -run pattern, but which group hooks ran around them — the one
// place the two sequential execution models do not agree (see assertPlanSelectedOnlyWhenB and
// assertRunnerSelectedOnlyWhenB).
func identityHelperSuite(s *Spec) {
	s.When("when a", func(s *Spec) {
		s.BeforeEach(func(*Context) { fmt.Println("HOOK before a") })
		s.AfterEach(func(*Context) { fmt.Println("HOOK after a") })
		s.It("does a thing", func(*Context) { fmt.Println("RAN a/does a thing") })
	})
	s.When("when b", func(s *Spec) {
		s.BeforeEach(func(*Context) { fmt.Println("HOOK before b") })
		s.It("does a thing", func(*Context) { fmt.Println("RAN b/does a thing") })
		s.It("slash/inside", func(*Context) { fmt.Println("RAN b/slash inside") })
		s.It("", func(*Context) { fmt.Println("RAN b/empty") })
	})
}

// identityHelperProgram is identityHelperSuite's Builder/Program equivalent, so both sequential
// execution models are proven against the same shapes (acceptance criterion 6).
//
// The hook counts of "when a" (one BeforeEach, one AfterEach) and "when b" (one BeforeEach) must NOT
// be made equal. Builder.hookKey degrades to (depth, hook counts) because sibling Describe scopes
// reuse the same backing-array slot and so share a pointer (#109); two siblings at equal depth with
// equal hook counts coalesce into one group and the second's hooks are discarded. Equalising them
// here would leave these tests passing while measuring a collapsed tree rather than two scopes.
func identityHelperProgram() *Program {
	b := NewBuilder()
	b.Describe("suite", func() {
		b.Describe("when a", func() {
			b.BeforeEach(func(*Context) { fmt.Println("HOOK before a") })
			b.AfterEach(func(*Context) { fmt.Println("HOOK after a") })
			b.It("does a thing", func(*Context) { fmt.Println("RAN a/does a thing") })
		})
		b.Describe("when b", func() {
			b.BeforeEach(func(*Context) { fmt.Println("HOOK before b") })
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
	assertPlanSelectedOnlyWhenB(t, string(output))
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
	assertRunnerSelectedOnlyWhenB(t, string(output))
}

// assertSelectedOnlyWhenB checks a context-scoped -run pattern executed the "when b" spec body and
// left its identically-named "when a" sibling — and every other spec — unrun. This part of the
// contract is shared by both sequential execution models; what differs is the hooks around it, which
// the two callers below assert for themselves.
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

// assertPlanSelectedOnlyWhenB adds the Spec/ExecutionPlan model's hook behaviour to the shared
// selection contract: the compiler flattens each scope's BeforeEach/AfterEach into the selected
// spec's own instruction range, and that range runs inside the subtest, so a -run pattern that
// discards a spec's subtest discards its hooks with it. Only "when b"'s before hook runs.
func assertPlanSelectedOnlyWhenB(t *testing.T, output string) {
	t.Helper()
	assertSelectedOnlyWhenB(t, output)
	if !strings.Contains(output, "HOOK before b") {
		t.Fatalf("expected the selected spec's own before hook to run, got:\n%s", output)
	}
	for _, unwanted := range []string{"HOOK before a", "HOOK after a"} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("expected %q not to run for a spec no -run pattern selected, got:\n%s", unwanted, output)
		}
	}
}

// assertRunnerSelectedOnlyWhenB pins the Runner/Program model's hook behaviour under -run selection,
// now converged with the plan model's above (#109). runSpecWithHooks runs a spec's before/after
// hooks inside its own subtest, as one unit with its body, so a -run pattern that discards a spec's
// subtest discards its hooks with it exactly like the plan model — "when a"'s before/after never run
// because -run never selects "when a"'s spec, and coalescing "when a"/"when b" into groups at compile
// time (a memory optimization — see program.go) has no bearing on this, since each spec still runs
// its own group's hooks for itself rather than sharing one run across the group.
//
// This used to diverge (before hooks ran once per group, outside any subtest, so -run could narrow
// which spec bodies ran without narrowing which hooks did); the two models now share not just the
// subtest identity mapping (#102) but behaviour under filtering too.
func assertRunnerSelectedOnlyWhenB(t *testing.T, output string) {
	t.Helper()
	assertPlanSelectedOnlyWhenB(t, output)
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

// TestSpecRunReportedPathKeepsNamesContainingSeparator asserts the guarantee that replaced the
// defect this test used to pin (#113).
//
// SpecStartEvent.Path was rebuilt by splitting the joined breadcrumb on "/", so a "/" inside a
// single declared name was indistinguishable from a scope boundary: It("slash/inside") — a spec
// identityHelperSuite has declared since this file was written — reported four segments for three
// declared scopes, and its last segment was not the spec's Name. specEventPath now reads the scopes
// the compiler recorded and appends Names[i], so the join is never round-tripped.
//
// Name is still asserted alongside Path, for the opposite reason it used to be: the two must now
// agree on the leaf. This stays here, next to the subtest identity tests, because the two
// derivations are easy to conflate — FullNames is joined and normalized for `go test -run`, Path is
// not — and this is where that distinction is under test.
func TestSpecRunReportedPathKeepsNamesContainingSeparator(t *testing.T) {
	var rep recordingReporter
	DescribeWithReporter(t, "suite", &rep, identityHelperSuite)

	var got *report.SpecStartEvent
	for i := range rep.specStarted {
		if rep.specStarted[i].Name == "slash/inside" {
			got = &rep.specStarted[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("expected a reported spec named %q, got %d events", "slash/inside", len(rep.specStarted))
	}
	// Three declared scopes, three reported segments: the "/" inside the leaf is not a boundary.
	wantPath := []string{"suite", "when b", "slash/inside"}
	if strings.Join(got.Path, "|") != strings.Join(wantPath, "|") {
		t.Fatalf("expected the declared Path %q, got %q", wantPath, got.Path)
	}
	if got.Path[len(got.Path)-1] != got.Name {
		t.Fatalf("expected Path's last segment to be the spec's Name %q, got %q", got.Name, got.Path[len(got.Path)-1])
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
	// Anchored on the newline that ends the "=== RUN" line: "/suite/a/b/does_it" on its own is a
	// substring of "/suite/a/b/does_it#01", so an unanchored check would pass on the suffixed name
	// alone and prove nothing about the plain one.
	for _, want := range []string{
		"/suite/a/b/does_it\n",
		"/suite/a/b/does_it#01\n",
		"/suite/when_c/does_that\n",
		"/suite/when_c/does_that#01\n",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected -v output to name the subtest %q on its own line, got:\n%s", want, output)
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

// TestDescribeFlatSubtestIdentityRealProcess pins what DescribeFlat actually does, which is not what
// its name or its doc comment suggested: it creates one subtest per spec, named by the full
// Describe/When/It breadcrumb, exactly like Describe.
//
// The "flat" in DescribeFlat refers to the compiled plan — hooks are flattened into each spec's own
// instruction range instead of being resolved by walking a tree — not to the absence of subtests.
// runSpecProgram's only gate is whether the backend wraps a real *testing.T. A *testing.B does not,
// which is why benchmarks genuinely run without subtests; a *testing.T always does. DescribeFlat is
// now a documented alias for Describe (#110); it never had a distinct runtime behavior to lose.
//
// This test exists so that claim is checked rather than asserted in prose: it failed against no
// version of this package, because no version of this package ever skipped the subtest for a
// DescribeFlat run under go test.
func TestDescribeFlatSubtestIdentityRealProcess(t *testing.T) {
	if os.Getenv("GO_SPECS_SUBTEST_FLAT_HELPER") == "1" {
		DescribeFlat(t, "suite", identityHelperSuite)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.v", "-test.run=^TestDescribeFlatSubtestIdentityRealProcess$")
	cmd.Env = append(os.Environ(), "GO_SPECS_SUBTEST_FLAT_HELPER=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helper run failed: %v\n%s", err, output)
	}
	assertVerboseIdentity(t, string(output))
}

// TestDescribeFastSubtestIdentityRealProcess is TestDescribeFlatSubtestIdentityRealProcess for
// DescribeFast, which delegates to DescribeFlat and therefore inherits its subtests too.
func TestDescribeFastSubtestIdentityRealProcess(t *testing.T) {
	if os.Getenv("GO_SPECS_SUBTEST_FAST_HELPER") == "1" {
		DescribeFast(t, "suite", identityHelperSuite)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.v", "-test.run=^TestDescribeFastSubtestIdentityRealProcess$")
	cmd.Env = append(os.Environ(), "GO_SPECS_SUBTEST_FAST_HELPER=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helper run failed: %v\n%s", err, output)
	}
	assertVerboseIdentity(t, string(output))
}

// TestRunnerRunHierarchicalSubtestsKeepReporterIdentityVerbatim is
// TestSpecRunHierarchicalSubtestsKeepReporterIdentityVerbatim for the Runner/Program model, which
// reaches the reporter through a different path — reporterObserver.specStarted(g.names[i],
// g.specPath(i)) — and so needs its own proof that hierarchical subtest names did not leak into
// reported identity.
//
// Until #112 was fixed, the Runner model never carried a Path at all (Name was reported, Path
// always stayed empty), unlike the ExecutionPlan model. Both models now agree: Path is the declared
// scope names, outermost first, with Name as the last element — built from the builder's raw
// (unjoined) scope stack (see specItem.scopeNames), not by splitting the joined breadcrumb, which
// would reintroduce the #113/#114 defect for a declared name containing "/".
func TestRunnerRunHierarchicalSubtestsKeepReporterIdentityVerbatim(t *testing.T) {
	b := NewBuilder()
	b.Describe("suite", func() {
		b.Describe("when a", func() {
			b.It("does a thing", func(*Context) {})
		})
	})
	var rep recordingReporter
	NewRunnerWithReporter(b.Build(), "suite", &rep).Run(t)

	if len(rep.specStarted) != 1 {
		t.Fatalf("expected 1 SpecStarted, got %d", len(rep.specStarted))
	}
	if got := rep.specStarted[0].Name; got != "does a thing" {
		t.Fatalf("expected the reported Name to stay the unsanitized leaf name, got %q", got)
	}
	wantPath := []string{"suite", "when a", "does a thing"}
	if got := rep.specStarted[0].Path; strings.Join(got, "|") != strings.Join(wantPath, "|") {
		t.Fatalf("expected the reported Path to be %q, got %q", wantPath, got)
	}
}

// TestRunnerRunReportedPathKeepsNamesContainingSeparator is
// TestSpecRunReportedPathKeepsNamesContainingSeparator (#113) for the Runner/Program model: the
// fix for #112 must not resurrect that defect by rebuilding Path from the joined breadcrumb.
func TestRunnerRunReportedPathKeepsNamesContainingSeparator(t *testing.T) {
	var rep recordingReporter
	NewRunnerWithReporter(identityHelperProgram(), "suite", &rep).Run(t)

	var got *report.SpecStartEvent
	for i := range rep.specStarted {
		if rep.specStarted[i].Name == "slash/inside" {
			got = &rep.specStarted[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("expected a reported spec named %q, got %d events", "slash/inside", len(rep.specStarted))
	}
	// Three declared scopes, three reported segments: the "/" inside the leaf is not a boundary.
	wantPath := []string{"suite", "when b", "slash/inside"}
	if strings.Join(got.Path, "|") != strings.Join(wantPath, "|") {
		t.Fatalf("expected the declared Path %q, got %q", wantPath, got.Path)
	}
	if got.Path[len(got.Path)-1] != got.Name {
		t.Fatalf("expected Path's last segment to be the spec's Name %q, got %q", got.Name, got.Path[len(got.Path)-1])
	}
}

// TestSpecRunFilteredSpecsAreReportedAsFilteredRealProcess proves the fix for #111 on the
// Spec/ExecutionPlan model: when a -run pattern discards a spec's subtest, the reporter must not
// tell a consumer the spec passed. t.Run's own bool return can't be used for this — it reports
// true for a subtest the filter discarded, the same as a genuine pass — so runSpecProgramIsolated
// instead sets a local `ran` flag from the first statement inside the closure passed to t.Run,
// which only executes when the filter accepts the subtest. That flag threads back through
// specResult.Filtered, and reportSpecFinished reports that spec as SpecResultEvent{Filtered: true}
// instead of a bare pass — Failed stays false and Duration stays 0, same as a compile-time Skipped
// spec, but Skipped itself stays false: the cause here is external selection, not a declared skip
// (see report.SpecResultEvent.Filtered's doc comment for why the two are kept apart).
//
// This test used to pin the opposite: `REPORTED ... failed=false skipped=false` for all four
// declared specs, indistinguishable from a genuine pass. It now asserts the corrected transcript
// deliberately, per the original test's own doc comment.
func TestSpecRunFilteredSpecsAreReportedAsFilteredRealProcess(t *testing.T) {
	if os.Getenv("GO_SPECS_SUBTEST_FILTER_REPORT_HELPER") == "1" {
		var rep recordingReporter
		DescribeWithReporter(t, "suite", &rep, identityHelperSuite)
		for i, started := range rep.specStarted {
			finished := rep.specFinished[i]
			fmt.Printf("REPORTED name=%q failed=%v skipped=%v filtered=%v\n", started.Name, finished.Failed, finished.Skipped, finished.Filtered)
		}
		return
	}
	cmd := exec.Command(os.Args[0],
		"-test.v",
		"-test.run=^TestSpecRunFilteredSpecsAreReportedAsFilteredRealProcess$/^suite$/^when_b$/^does_a_thing$",
	)
	cmd.Env = append(os.Environ(), "GO_SPECS_SUBTEST_FILTER_REPORT_HELPER=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helper run failed: %v\n%s", err, output)
	}
	transcript := string(output)

	// Exactly one body ran, all four specs were reported, none of them failed or was marked
	// Skipped — but the three that never ran are now marked Filtered, and the one that did is not.
	if strings.Count(transcript, "RAN ") != 1 {
		t.Fatalf("expected exactly one spec body to run under the -run pattern, got:\n%s", transcript)
	}
	for _, want := range []string{
		`REPORTED name="does a thing" failed=false skipped=false filtered=true`,  // when a/does a thing
		`REPORTED name="does a thing" failed=false skipped=false filtered=false`, // when b/does a thing: ran
		`REPORTED name="slash/inside" failed=false skipped=false filtered=true`,
		`REPORTED name="" failed=false skipped=false filtered=true`,
	} {
		if !strings.Contains(transcript, want) {
			t.Fatalf("expected the reporter to emit %s, got:\n%s", want, transcript)
		}
	}
	if got := strings.Count(transcript, "REPORTED "); got != 4 {
		t.Fatalf("expected 4 reported specs for 4 declared specs, got %d in:\n%s", got, transcript)
	}
	if got := strings.Count(transcript, "filtered=true"); got != 3 {
		t.Fatalf("expected 3 specs reported as filtered, got %d in:\n%s", got, transcript)
	}
}

// TestRunnerRunFilteredSpecsAreReportedAsFilteredRealProcess is
// TestSpecRunFilteredSpecsAreReportedAsFilteredRealProcess for the Runner/Program model: #111's
// defect shape was identical in runSpecIsolated, which sets the same closure-scoped `ran` flag
// the same way as its ExecutionPlan counterpart, for the same reason — t.Run's own bool return
// doesn't distinguish a filtered-out subtest from a genuine pass.
func TestRunnerRunFilteredSpecsAreReportedAsFilteredRealProcess(t *testing.T) {
	if os.Getenv("GO_SPECS_SUBTEST_FILTER_REPORT_RUNNER_HELPER") == "1" {
		var rep recordingReporter
		NewRunnerWithReporter(identityHelperProgram(), "suite", &rep).Run(t)
		for i, started := range rep.specStarted {
			finished := rep.specFinished[i]
			fmt.Printf("REPORTED name=%q failed=%v skipped=%v filtered=%v\n", started.Name, finished.Failed, finished.Skipped, finished.Filtered)
		}
		return
	}
	cmd := exec.Command(os.Args[0],
		"-test.v",
		"-test.run=^TestRunnerRunFilteredSpecsAreReportedAsFilteredRealProcess$/^suite$/^when_b$/^does_a_thing$",
	)
	cmd.Env = append(os.Environ(), "GO_SPECS_SUBTEST_FILTER_REPORT_RUNNER_HELPER=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helper run failed: %v\n%s", err, output)
	}
	transcript := string(output)

	if strings.Count(transcript, "RAN ") != 1 {
		t.Fatalf("expected exactly one spec body to run under the -run pattern, got:\n%s", transcript)
	}
	for _, want := range []string{
		`REPORTED name="does a thing" failed=false skipped=false filtered=true`,  // when a/does a thing
		`REPORTED name="does a thing" failed=false skipped=false filtered=false`, // when b/does a thing: ran
		`REPORTED name="slash/inside" failed=false skipped=false filtered=true`,
		`REPORTED name="" failed=false skipped=false filtered=true`,
	} {
		if !strings.Contains(transcript, want) {
			t.Fatalf("expected the reporter to emit %s, got:\n%s", want, transcript)
		}
	}
	if got := strings.Count(transcript, "REPORTED "); got != 4 {
		t.Fatalf("expected 4 reported specs for 4 declared specs, got %d in:\n%s", got, transcript)
	}
	if got := strings.Count(transcript, "filtered=true"); got != 3 {
		t.Fatalf("expected 3 specs reported as filtered, got %d in:\n%s", got, transcript)
	}
}
