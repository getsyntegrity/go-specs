package specs

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/pablogore/go-specs/report"
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

// assertRunnerSelectedOnlyWhenB pins the Runner/Program model's hook behaviour, which diverges from
// the plan model's above. This divergence is known and deliberately pinned, not an oversight in
// either test: runGroup runs a group's before hooks and defers its after hooks around
// runSpecsRecovered, which is what calls t.Run, so the hooks live *outside* the subtest. testing can
// only discard what is inside a subtest, so a -run pattern narrows which spec bodies execute but not
// which group hooks do — "when a"'s before and after still run even though its only spec does not.
//
// -run only makes the difference visible; it does not create it. The same placement means the two
// models already disagree with no pattern at all: for a scope with three specs the plan model runs
// its BeforeEach three times and the Runner runs it once, so BeforeEach does not carry per-spec
// semantics in this model (#109).
//
// The two models therefore share the subtest identity mapping, not their behaviour under filtering.
// Filed separately; #102 changed neither model's hook placement.
func assertRunnerSelectedOnlyWhenB(t *testing.T, output string) {
	t.Helper()
	assertSelectedOnlyWhenB(t, output)
	for _, want := range []string{"HOOK before a", "HOOK after a", "HOOK before b"} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected %q to run — group hooks sit outside the subtest in this model, got:\n%s", want, output)
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
// Spec.flat is recorded at declaration time and never read again; CompiledSuite does not carry it,
// and runSpecProgram's only gate is whether the backend wraps a real *testing.T. A *testing.B does
// not, which is why benchmarks genuinely run without subtests; a *testing.T always does.
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
// reaches the reporter through a different path — reporterObserver.specStarted(g.names[i]) — and so
// needs its own proof that hierarchical subtest names did not leak into reported identity.
//
// The two models report different things, and both are pinned here on purpose. The plan model
// carries a Path built from the breadcrumb; the Runner model has never carried one, so Path stays
// empty and Name is the bare leaf It name. What #102 had to preserve is that neither field picks up
// the breadcrumb or testing's rewrite of it.
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
	if got := rep.specStarted[0].Path; len(got) != 0 {
		t.Fatalf("expected the Runner model to report no Path, got %q", got)
	}
}

// TestSpecRunFilteredSpecsAreReportedAsPassedRealProcess pins a defect, not a guarantee: when a -run
// pattern discards a spec's subtest, the reporter is still told the spec started and finished
// without failing, so a spec whose body never executed is counted as passed.
//
// Neither runSpecProgramIsolated nor runSpecIsolated inspects testing.T.Run's bool return, which is
// false exactly when the filter discarded the subtest, and the SpecStarted/SpecFinished pair is
// emitted around that call with ctx.failed still at its reset value. The behaviour predates #102 —
// it arrived with the per-spec isolation of #74 — but #102 turns -run into a documented way to
// select specs, so it is now reachable on first use rather than latent. Pinned so the fix, when it
// lands, has to change this test deliberately.
func TestSpecRunFilteredSpecsAreReportedAsPassedRealProcess(t *testing.T) {
	if os.Getenv("GO_SPECS_SUBTEST_FILTER_REPORT_HELPER") == "1" {
		var rep recordingReporter
		DescribeWithReporter(t, "suite", &rep, identityHelperSuite)
		for i, started := range rep.specStarted {
			finished := rep.specFinished[i]
			fmt.Printf("REPORTED name=%q failed=%v skipped=%v\n", started.Name, finished.Failed, finished.Skipped)
		}
		return
	}
	cmd := exec.Command(os.Args[0],
		"-test.v",
		"-test.run=^TestSpecRunFilteredSpecsAreReportedAsPassedRealProcess$/^suite$/^when_b$/^does_a_thing$",
	)
	cmd.Env = append(os.Environ(), "GO_SPECS_SUBTEST_FILTER_REPORT_HELPER=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helper run failed: %v\n%s", err, output)
	}
	transcript := string(output)

	// Exactly one body ran, but all four specs were reported, none of them failed, and none of them
	// was marked skipped.
	if strings.Count(transcript, "RAN ") != 1 {
		t.Fatalf("expected exactly one spec body to run under the -run pattern, got:\n%s", transcript)
	}
	for _, want := range []string{
		`REPORTED name="does a thing" failed=false skipped=false`,
		`REPORTED name="slash/inside" failed=false skipped=false`,
		`REPORTED name="" failed=false skipped=false`,
	} {
		if !strings.Contains(transcript, want) {
			t.Fatalf("expected the reporter to emit %s, got:\n%s", want, transcript)
		}
	}
	if got := strings.Count(transcript, "REPORTED "); got != 4 {
		t.Fatalf("expected 4 reported specs for 4 declared specs, got %d in:\n%s", got, transcript)
	}
}

// TestCandidateSubtestNameDuplicateValuesStayDistinct proves acceptance criterion 4 of #103 directly
// against candidateSubtestName: two candidates whose PathValues are byte-for-byte equal must still
// get distinct Go subtest names, because the guarantee rests on AttemptIndex — proposalController's
// own monotonically increasing count (exploration_controller.go), unique for the whole run — not on
// the values themselves. It also pins that equal Values still produce an equal hash fingerprint,
// which is the point of carrying one: a developer can tell two differently-numbered candidates
// proposed the same inputs.
func TestCandidateSubtestNameDuplicateValuesStayDistinct(t *testing.T) {
	plan := &ExecutionPlan{Names: []string{"case"}, FullNames: []string{"suite/case"}}
	values := PathValues{index: map[string]int{"n": 0}, values: []any{7}, present: []bool{true}}
	fingerprint := uint32(values.Hash())

	first := candidateSubtestName(plan, 0, report.CandidateIdentity{AttemptIndex: 1, AcceptedIndex: 1, Fingerprint: fingerprint})
	second := candidateSubtestName(plan, 0, report.CandidateIdentity{AttemptIndex: 2, AcceptedIndex: 2, Fingerprint: fingerprint})
	if first == second {
		t.Fatalf("expected two candidates with equal Values but distinct AttemptIndex to get distinct names, both were %q", first)
	}
	if !strings.HasPrefix(first, "suite/case/generated#1_") {
		t.Fatalf("expected the owning breadcrumb and attempt index in the name, got %q", first)
	}
	if !strings.HasPrefix(second, "suite/case/generated#2_") {
		t.Fatalf("expected the owning breadcrumb and attempt index in the name, got %q", second)
	}

	firstHash := first[strings.LastIndex(first, "_")+1:]
	secondHash := second[strings.LastIndex(second, "_")+1:]
	if firstHash != secondHash {
		t.Fatalf("expected equal PathValues to produce equal hash fingerprints, got %q and %q", firstHash, secondHash)
	}
}

// TestCandidateSubtestNameFallsBackWithoutBreadcrumb proves candidateSubtestName degrades the same
// way specSubtestName already does for a plan without breadcrumbs (a hand-built ExecutionPlan, or a
// compiler with an empty name stack): the candidate's own generated#<attempt>_<hash> element alone,
// with no leading "/" left dangling from a missing owner.
func TestCandidateSubtestNameFallsBackWithoutBreadcrumb(t *testing.T) {
	plan := &ExecutionPlan{}
	got := candidateSubtestName(plan, 0, report.CandidateIdentity{AttemptIndex: 5})
	if !strings.HasPrefix(got, "generated#5_") {
		t.Fatalf("expected a bare generated#N_hash name for a plan without breadcrumbs, got %q", got)
	}
}

// TestCandidateSubtestNameOmitsSeedForCartesian pins #103's AC2 distinction between Cartesian and
// every other strategy: Cartesian's odometer order is a pure function of the declared dimensions and
// filters, with no RNG to pin down, so its candidate names never grow a "_seed..." segment even
// though CandidateIdentity always has a Seed field.
func TestCandidateSubtestNameOmitsSeedForCartesian(t *testing.T) {
	plan := &ExecutionPlan{Names: []string{"case"}, FullNames: []string{"suite/case"}}
	got := candidateSubtestName(plan, 0, report.CandidateIdentity{Strategy: "cartesian", AttemptIndex: 1, Fingerprint: 0xdeadbeef})
	if strings.Contains(got, "_seed") {
		t.Fatalf("expected no seed segment for cartesian, got %q", got)
	}
}

// TestCandidateSubtestNameIncludesSeedWhenPresent pins #103's AC2/AC3: every strategy but Cartesian
// draws its candidates from one RNG seeded once from CandidateIdentity.Seed, so that seed must be
// visible directly in the Go subtest name — the one place a bare `go test -v` transcript (no
// reporter attached) carries any information at all — for the candidate to be reproducible from -v
// output alone.
func TestCandidateSubtestNameIncludesSeedWhenPresent(t *testing.T) {
	plan := &ExecutionPlan{Names: []string{"case"}, FullNames: []string{"suite/case"}}
	got := candidateSubtestName(plan, 0, report.CandidateIdentity{
		Strategy: "sample", Seed: 42, HasSeed: true, AttemptIndex: 3, Fingerprint: 0xabc,
	})
	if !strings.Contains(got, "_seed42_") {
		t.Fatalf("expected a seed42 segment, got %q", got)
	}
}

// TestCandidateSubtestNameIncludesAcceptedIndexForAdaptiveStrategies pins #103's AC3: the three
// ExplorationGuided strategies (Explore/ExploreCoverage/ExploreSmart) name their AcceptedIndex
// explicitly, since Accept is public API a future strategy could use to make it diverge from
// AttemptIndex. Cartesian (never rejects) and Sample (whose internal filter retries never surface as
// a separate accepted/rejected proposal) omit it — it would just repeat AttemptIndex today.
func TestCandidateSubtestNameIncludesAcceptedIndexForAdaptiveStrategies(t *testing.T) {
	plan := &ExecutionPlan{Names: []string{"case"}, FullNames: []string{"suite/case"}}
	for _, strategy := range []string{"explore", "explore-coverage", "explore-smart"} {
		id := report.CandidateIdentity{Strategy: strategy, Seed: 9, HasSeed: true, AttemptIndex: 4, AcceptedIndex: 3, Fingerprint: 0x1}
		got := candidateSubtestName(plan, 0, id)
		if !strings.Contains(got, "_accepted3_") {
			t.Fatalf("%s: expected an accepted3 segment, got %q", strategy, got)
		}
	}
	for _, strategy := range []string{"cartesian", "sample"} {
		id := report.CandidateIdentity{Strategy: strategy, Seed: 9, HasSeed: strategy == "sample", AttemptIndex: 4, AcceptedIndex: 3, Fingerprint: 0x1}
		got := candidateSubtestName(plan, 0, id)
		if strings.Contains(got, "_accepted") {
			t.Fatalf("%s: expected no accepted segment, got %q", strategy, got)
		}
	}
}

// TestCandidatePathIdentityStrategyAndSeed pins candidatePathIdentity's mapping from a PathGenerator's
// own mode/strategy/explorationSeed to the CandidateIdentity every strategy reports (#103): Cartesian
// carries no seed at all (HasSeed false), and every other strategy carries the exact seed the
// generator resolved (explicit via .Seed, since every case below sets hasSeed true).
func TestCandidatePathIdentityStrategyAndSeed(t *testing.T) {
	oneVar := []PathVar{{Name: "n", Values: []any{1, 2}}}
	cases := []struct {
		name         string
		gen          *PathGenerator
		wantStrategy string
		wantHasSeed  bool
		wantSeed     int64
	}{
		{"cartesian", newPathGenerator(oneVar, nil, 0, 0, false, 0, 0, 0), "cartesian", false, 0},
		{"sample", newPathGenerator(oneVar, nil, 3, 42, true, 0, 0, 0), "sample", true, 42},
		{"explore", newPathGenerator(oneVar, nil, 0, 7, true, 5, 0, 0), "explore", true, 7},
		{"explore-coverage", newPathGenerator(oneVar, nil, 0, 11, true, 0, 5, 0), "explore-coverage", true, 11},
		{"explore-smart", newPathGenerator(oneVar, nil, 0, 13, true, 0, 0, 5), "explore-smart", true, 13},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := candidatePathIdentity(tc.gen, proposalCandidate{AttemptIndex: 3, AcceptedIndex: 3})
			if id.Strategy != tc.wantStrategy {
				t.Fatalf("expected strategy %q, got %q", tc.wantStrategy, id.Strategy)
			}
			if id.HasSeed != tc.wantHasSeed {
				t.Fatalf("expected HasSeed=%v, got %v", tc.wantHasSeed, id.HasSeed)
			}
			if tc.wantHasSeed && id.Seed != tc.wantSeed {
				t.Fatalf("expected seed %d, got %d", tc.wantSeed, id.Seed)
			}
		})
	}
}

// pathsIdentityHelperSuite declares a Paths-generated spec with four Cartesian combinations, so the
// real-process tests below can check every generated candidate's own subtest identity (#103) against
// a genuine `go test -v` transcript.
func pathsIdentityHelperSuite(s *Spec) {
	s.When("checkout", func(s *Spec) {
		s.Paths(func(pb *PathBuilder) {
			pb.Bool("vip")
			pb.Bool("giftWrap")
		}).It("computes a total", func(ctx *Context) {
			fmt.Printf("RAN vip=%v giftWrap=%v\n", ctx.Path().Bool("vip"), ctx.Path().Bool("giftWrap"))
		})
	})
}

// TestSpecRunGeneratedCandidateSubtestIdentityRealProcess proves acceptance criteria 1, 2 and 4 of
// #103 against a genuine `go test -v` run: every path-generated candidate's subtest now carries its
// owning It's full Describe/When/It breadcrumb — not the bare literal "generated" every candidate
// used to share regardless of which spec proposed it — followed by its own stable
// generated#<attempt>_<hash> element, so distinct candidates never need testing's own "#01"
// disambiguation suffix.
//
// A subprocess is required, not incidental: the assertions are about this process's own -v
// transcript, which only a child re-exec can produce and read back — the same reason
// TestSpecRunNestedSubtestIdentityRealProcess above needs one.
func TestSpecRunGeneratedCandidateSubtestIdentityRealProcess(t *testing.T) {
	if os.Getenv("GO_SPECS_GENERATED_IDENTITY_HELPER") == "1" {
		Describe(t, "suite", pathsIdentityHelperSuite)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.v", "-test.run=^TestSpecRunGeneratedCandidateSubtestIdentityRealProcess$")
	cmd.Env = append(os.Environ(), "GO_SPECS_GENERATED_IDENTITY_HELPER=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helper run failed: %v\n%s", err, output)
	}
	transcript := string(output)

	for _, want := range []string{
		"RAN vip=true giftWrap=true",
		"RAN vip=true giftWrap=false",
		"RAN vip=false giftWrap=true",
		"RAN vip=false giftWrap=false",
	} {
		if !strings.Contains(transcript, want) {
			t.Fatalf("expected every combination to run, missing %q in:\n%s", want, transcript)
		}
	}
	// Every candidate's subtest starts with the owning It's full breadcrumb, followed by
	// generated#<attempt>_<hash> — never the bare "generated" every candidate used to share.
	for i := 1; i <= 4; i++ {
		want := fmt.Sprintf("/suite/checkout/computes_a_total/generated#%d_", i)
		if !strings.Contains(transcript, want) {
			t.Fatalf("expected candidate %d's subtest to start with %q, got:\n%s", i, want, transcript)
		}
	}
	// Go's own "#01" duplicate-name suffix can only appear if two subtests were handed the exact
	// same literal name — the defect #103 fixes. It never should here, since every candidate's own
	// attempt index already makes its name unique.
	if strings.Contains(transcript, "generated#01") {
		t.Fatalf("did not expect testing's own duplicate-name suffix once candidates carry their own identity, got:\n%s", transcript)
	}
}

// pathsSampleIdentityHelperSuite declares a Paths-generated spec using the Sample strategy with an
// explicit seed, so TestSpecRunSampleCandidateSubtestIdentityRealProcess below can check that the
// seed itself — not just the attempt index — shows up in a genuine `go test -v` transcript (#103 AC2:
// enough stable information to reproduce the selected case from -v output alone, with no reporter
// attached).
func pathsSampleIdentityHelperSuite(s *Spec) {
	s.When("checkout", func(s *Spec) {
		s.Paths(func(pb *PathBuilder) {
			pb.Int("qty", []int{1, 2, 3, 4, 5})
		}).Sample(3).Seed(7).It("computes a total", func(ctx *Context) {
			fmt.Printf("RAN qty=%v\n", ctx.Path().Int("qty"))
		})
	})
}

// TestSpecRunSampleCandidateSubtestIdentityRealProcess proves #103's AC2/AC3 for the Sample strategy
// against a genuine `go test -v` run: since Sample draws its candidates from an RNG seeded once from
// PathSpec.Seed, every candidate's own subtest name must carry that seed — not just its attempt index
// — so the exact sequence of candidates can be reproduced by rerunning with the same .Seed(7) alone,
// without any replay/resumption machinery (explicitly out of scope for #103).
func TestSpecRunSampleCandidateSubtestIdentityRealProcess(t *testing.T) {
	if os.Getenv("GO_SPECS_SAMPLE_IDENTITY_HELPER") == "1" {
		Describe(t, "suite", pathsSampleIdentityHelperSuite)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.v", "-test.run=^TestSpecRunSampleCandidateSubtestIdentityRealProcess$")
	cmd.Env = append(os.Environ(), "GO_SPECS_SAMPLE_IDENTITY_HELPER=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helper run failed: %v\n%s", err, output)
	}
	transcript := string(output)

	for i := 1; i <= 3; i++ {
		want := fmt.Sprintf("/suite/checkout/computes_a_total/generated#%d_seed7_", i)
		if !strings.Contains(transcript, want) {
			t.Fatalf("expected candidate %d's subtest to contain %q, got:\n%s", i, want, transcript)
		}
	}
}

// pathsExploreIdentityHelperSuite declares a Paths-generated spec using the Explore (adaptive,
// ExplorationGuided) strategy with an explicit seed, so the real-process test below can check that
// both the seed and the accepted index show up in a genuine `go test -v` transcript (#103 AC3:
// adaptive strategies must expose seed + attempt/accepted identity).
func pathsExploreIdentityHelperSuite(s *Spec) {
	s.When("checkout", func(s *Spec) {
		s.Paths(func(pb *PathBuilder) {
			pb.Int("qty", []int{1, 2, 3, 4, 5})
		}).Explore(3).Seed(11).It("computes a total", func(ctx *Context) {
			fmt.Printf("RAN qty=%v\n", ctx.Path().Int("qty"))
		})
	})
}

// TestSpecRunExploreCandidateSubtestIdentityRealProcess proves #103's AC3 for the Explore
// (ExplorationGuided) strategy against a genuine `go test -v` run: every candidate's own subtest name
// must carry both its seed and its accepted index, since every candidate reaching execution here was
// accepted by the guided controller, not just enumerated like Cartesian.
func TestSpecRunExploreCandidateSubtestIdentityRealProcess(t *testing.T) {
	if os.Getenv("GO_SPECS_EXPLORE_IDENTITY_HELPER") == "1" {
		Describe(t, "suite", pathsExploreIdentityHelperSuite)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.v", "-test.run=^TestSpecRunExploreCandidateSubtestIdentityRealProcess$")
	cmd.Env = append(os.Environ(), "GO_SPECS_EXPLORE_IDENTITY_HELPER=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helper run failed: %v\n%s", err, output)
	}
	transcript := string(output)

	for i := 1; i <= 3; i++ {
		want := fmt.Sprintf("/suite/checkout/computes_a_total/generated#%d_seed11_accepted%d_", i, i)
		if !strings.Contains(transcript, want) {
			t.Fatalf("expected candidate %d's subtest to carry both seed and accepted index, wanted %q in:\n%s", i, want, transcript)
		}
	}
}

// TestSpecRunGeneratedCandidateReporterIdentityCarriesCandidate extends
// TestSpecRunHierarchicalSubtestsKeepReporterIdentityVerbatim to path-generated candidates. Giving
// each candidate its own breadcrumb-qualified Go subtest name (#103) must not change what the
// reporter is told a candidate's Name is — every accepted candidate still reports the plain declared
// leaf name, never the generated#<attempt>_<hash> suffix that exists purely for testing.T.Run.
//
// But #103's AC6 requires more than that: go test -v, test2json, and reporter events must all be able
// to identify the same logical candidate. A reporter that only ever saw the constant leaf Name could
// not do that — every one of a spec's candidates would look identical to it. So SpecStartEvent.Candidate
// must carry the same AttemptIndex (and, for Cartesian, Strategy/HasSeed) that candidateSubtestName
// wove into the Go subtest name for that very candidate.
func TestSpecRunGeneratedCandidateReporterIdentityCarriesCandidate(t *testing.T) {
	var rep recordingReporter
	DescribeWithReporter(t, "suite", &rep, pathsIdentityHelperSuite)

	if len(rep.specStarted) != 4 {
		t.Fatalf("expected 4 SpecStarted events, one per Cartesian combination, got %d", len(rep.specStarted))
	}
	for i, started := range rep.specStarted {
		if started.Name != "computes a total" {
			t.Fatalf("event %d: expected the reported Name to stay the declared leaf name, got %q", i, started.Name)
		}
		if started.Candidate == nil {
			t.Fatalf("event %d: expected a non-nil Candidate for a Paths()-generated spec", i)
		}
		if started.Candidate.Strategy != "cartesian" {
			t.Fatalf("event %d: expected strategy %q, got %q", i, "cartesian", started.Candidate.Strategy)
		}
		if started.Candidate.HasSeed {
			t.Fatalf("event %d: expected HasSeed=false for cartesian, got true", i)
		}
		wantAttempt := i + 1
		if started.Candidate.AttemptIndex != wantAttempt {
			t.Fatalf("event %d: expected AttemptIndex %d, got %d", i, wantAttempt, started.Candidate.AttemptIndex)
		}
		if started.Candidate.AcceptedIndex != started.Candidate.AttemptIndex {
			t.Fatalf("event %d: expected AcceptedIndex to equal AttemptIndex (%d), got %d", i, started.Candidate.AttemptIndex, started.Candidate.AcceptedIndex)
		}
	}
}

// BenchmarkCandidateSubtestName measures #103's per-candidate cost end to end: the fmt.Fprintf and
// strings.Builder work candidateSubtestName does for every executed generated case, against a
// realistic breadcrumb and identity.
func BenchmarkCandidateSubtestName(b *testing.B) {
	plan := &ExecutionPlan{
		Names:     []string{"computes a total"},
		FullNames: []string{"suite/checkout/computes a total"},
	}
	values := PathValues{
		index:   map[string]int{"sku": 0, "region": 1, "qty": 2, "price": 3, "vip": 4},
		values:  []any{"SKU-0042-DELUXE", "us-east", 7, 199, true},
		present: []bool{true, true, true, true, true},
	}
	id := report.CandidateIdentity{
		Strategy: "explore", Seed: 99, HasSeed: true,
		AttemptIndex: 42, AcceptedIndex: 40, Fingerprint: uint32(values.Hash()),
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = candidateSubtestName(plan, 0, id)
	}
}
