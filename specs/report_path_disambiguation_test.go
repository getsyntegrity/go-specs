// report_path_disambiguation_test.go proves issue #275's contract: two sibling Describe/When groups
// sharing a literal name get distinct report Paths, computed once at compile time, across every build
// path (bytecode compiler, arena/registry, Builder/Program), while Go subtest identity and selection
// (#102) stay exactly as declared. See docs/DSL.md for the documented format this pins.
package specs

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/getsyntegrity/go-specs/report"
)

// specPaths collects every SpecStartEvent.Path recorded by rep, joined with "/", in report order —
// the shape most of this file's assertions compare against.
func specPaths(rep *recordingReporter) []string {
	rep.mu.Lock()
	defer rep.mu.Unlock()
	out := make([]string, len(rep.specStarted))
	for i, e := range rep.specStarted {
		out[i] = strings.Join(e.Path, "/")
	}
	return out
}

// TestSpecReportPathDisambiguatesDuplicateSiblings proves the bytecode-compiler (Describe/Spec/
// ExecutionPlan) build path gives two sibling Describe("D") groups with the same leaf name distinct
// report Paths, while their Go subtest identity is untouched (still only distinguished by testing's
// own "#01", per #102 — proven separately by the real-process test below).
func TestSpecReportPathDisambiguatesDuplicateSiblings(t *testing.T) {
	rep := &recordingReporter{}
	DescribeWithReporter(t, "suite", rep, func(s *Spec) {
		s.Describe("D", func(s *Spec) {
			s.It("z", func(*Context) {})
		})
		s.Describe("D", func(s *Spec) {
			s.It("z", func(*Context) {})
		})
	})
	got := specPaths(rep)
	want := []string{"suite/D/z", "suite/D#2/z"}
	if !stringSlicesEqual(got, want) {
		t.Fatalf("report Paths = %q, want %q", got, want)
	}
}

// TestSpecReportPathDisambiguatesThreeSiblings proves three same-named siblings each get a distinct
// suffix in declaration order.
func TestSpecReportPathDisambiguatesThreeSiblings(t *testing.T) {
	rep := &recordingReporter{}
	DescribeWithReporter(t, "suite", rep, func(s *Spec) {
		for range 3 {
			s.Describe("D", func(s *Spec) {
				s.It("z", func(*Context) {})
			})
		}
	})
	got := specPaths(rep)
	want := []string{"suite/D/z", "suite/D#2/z", "suite/D#3/z"}
	if !stringSlicesEqual(got, want) {
		t.Fatalf("report Paths = %q, want %q", got, want)
	}
}

// TestSpecReportPathDisambiguationIsPerParent proves duplicate names in different parent scopes never
// interact: a nested duplicate does not "borrow" ordinals from its parent's own duplicates, and the
// same name repeated in an unrelated sibling subtree stays unsuffixed there.
func TestSpecReportPathDisambiguationIsPerParent(t *testing.T) {
	rep := &recordingReporter{}
	DescribeWithReporter(t, "suite", rep, func(s *Spec) {
		s.Describe("D", func(s *Spec) {
			s.Describe("X", func(s *Spec) { s.It("z", func(*Context) {}) })
			s.Describe("X", func(s *Spec) { s.It("z", func(*Context) {}) })
		})
		s.Describe("D", func(s *Spec) {
			s.Describe("X", func(s *Spec) { s.It("z", func(*Context) {}) })
		})
	})
	got := specPaths(rep)
	want := []string{
		"suite/D/X/z",
		"suite/D/X#2/z",
		"suite/D#2/X/z",
	}
	if !stringSlicesEqual(got, want) {
		t.Fatalf("report Paths = %q, want %q", got, want)
	}
}

// TestSpecReportPathLiteralCollisionBothOrders proves docs/DSL.md's collision example both ways: a
// literal "D#2" sibling declared after the second "D" is still respected (the second "D" skips to
// "D#3"), and the same literal declared before the duplicate has the identical effect — the ordinal
// is computed once every sibling is known, not greedily as each is declared.
func TestSpecReportPathLiteralCollisionBothOrders(t *testing.T) {
	t.Run("literal declared after", func(t *testing.T) {
		rep := &recordingReporter{}
		DescribeWithReporter(t, "suite", rep, func(s *Spec) {
			s.Describe("D", func(s *Spec) { s.It("a", func(*Context) {}) })
			s.Describe("D", func(s *Spec) { s.It("b", func(*Context) {}) })
			s.Describe("D#2", func(s *Spec) { s.It("c", func(*Context) {}) })
		})
		got := specPaths(rep)
		want := []string{"suite/D/a", "suite/D#3/b", "suite/D#2/c"}
		if !stringSlicesEqual(got, want) {
			t.Fatalf("report Paths = %q, want %q", got, want)
		}
	})
	t.Run("literal declared before", func(t *testing.T) {
		rep := &recordingReporter{}
		DescribeWithReporter(t, "suite", rep, func(s *Spec) {
			s.Describe("D", func(s *Spec) { s.It("a", func(*Context) {}) })
			s.Describe("D#2", func(s *Spec) { s.It("c", func(*Context) {}) })
			s.Describe("D", func(s *Spec) { s.It("b", func(*Context) {}) })
		})
		got := specPaths(rep)
		want := []string{"suite/D/a", "suite/D#2/c", "suite/D#3/b"}
		if !stringSlicesEqual(got, want) {
			t.Fatalf("report Paths = %q, want %q", got, want)
		}
	})
}

// TestSpecReportPathRepeatedCompilationsAreDeterministic proves compiling the exact same suite
// declaration twice yields byte-identical report Paths both times: the ordinal is a pure function of
// declaration structure, never a global/process-wide counter.
func TestSpecReportPathRepeatedCompilationsAreDeterministic(t *testing.T) {
	build := func() []string {
		rep := &recordingReporter{}
		DescribeWithReporter(t, "suite", rep, func(s *Spec) {
			s.Describe("D", func(s *Spec) { s.It("z", func(*Context) {}) })
			s.Describe("D", func(s *Spec) { s.It("z", func(*Context) {}) })
			s.Describe("D", func(s *Spec) { s.It("z", func(*Context) {}) })
		})
		return specPaths(rep)
	}
	first := build()
	second := build()
	if !stringSlicesEqual(first, second) {
		t.Fatalf("repeated compilations disagree: %q vs %q", first, second)
	}
	want := []string{"suite/D/z", "suite/D#2/z", "suite/D#3/z"}
	if !stringSlicesEqual(first, want) {
		t.Fatalf("report Paths = %q, want %q", first, want)
	}
}

// TestSpecReportPathNoDuplicatesUnchanged proves a suite with no duplicate sibling group name reports
// exactly the Paths it always has — the feature must be a no-op for the common case.
func TestSpecReportPathNoDuplicatesUnchanged(t *testing.T) {
	rep := &recordingReporter{}
	DescribeWithReporter(t, "suite", rep, func(s *Spec) {
		s.When("when a", func(s *Spec) {
			s.It("does a", func(*Context) {})
		})
		s.When("when b", func(s *Spec) {
			s.It("does b", func(*Context) {})
		})
	})
	got := specPaths(rep)
	want := []string{"suite/when a/does a", "suite/when b/does b"}
	if !stringSlicesEqual(got, want) {
		t.Fatalf("report Paths = %q, want %q", got, want)
	}
}

// buildAnalyzeSuite builds and returns an arena/registry-path CompiledSuite for fn (issue #275's
// second build path): Analyze(fn) registers the tree without compiling or running it (tb is nil), so
// BuildSuite inside it just compiles — s.Compile() — without a real *testing.T subtest ever opening.
// Run separately with rep attached, after Analyze returns.
func buildAnalyzeSuite(t *testing.T, name string, fn func(s *Spec)) *CompiledSuite {
	t.Helper()
	var suite *CompiledSuite
	Analyze(func() {
		suite = BuildSuite(nil, name, fn)
	})
	if suite == nil {
		t.Fatalf("BuildSuite returned nil inside Analyze")
	}
	return suite
}

// TestArenaReportPathDisambiguatesDuplicateSiblings is
// TestSpecReportPathDisambiguatesDuplicateSiblings for the arena/registry build path (issue #275's
// second engine): Analyze/BuildSuite compiles from a NodeArena instead of the bytecode compiler.
func TestArenaReportPathDisambiguatesDuplicateSiblings(t *testing.T) {
	suite := buildAnalyzeSuite(t, "suite", func(s *Spec) {
		s.Describe("D", func(s *Spec) {
			s.It("z", func(*Context) {})
		})
		s.Describe("D", func(s *Spec) {
			s.It("z", func(*Context) {})
		})
	})
	rep := &recordingReporter{}
	suite.Reporter = rep
	suite.Run(t)
	got := specPaths(rep)
	want := []string{"suite/D/z", "suite/D#2/z"}
	if !stringSlicesEqual(got, want) {
		t.Fatalf("report Paths = %q, want %q", got, want)
	}
}

// TestArenaReportPathDisambiguatesThreeSiblings is TestSpecReportPathDisambiguatesThreeSiblings for
// the arena/registry build path.
func TestArenaReportPathDisambiguatesThreeSiblings(t *testing.T) {
	suite := buildAnalyzeSuite(t, "suite", func(s *Spec) {
		for range 3 {
			s.Describe("D", func(s *Spec) {
				s.It("z", func(*Context) {})
			})
		}
	})
	rep := &recordingReporter{}
	suite.Reporter = rep
	suite.Run(t)
	got := specPaths(rep)
	want := []string{"suite/D/z", "suite/D#2/z", "suite/D#3/z"}
	if !stringSlicesEqual(got, want) {
		t.Fatalf("report Paths = %q, want %q", got, want)
	}
}

// TestArenaReportPathDisambiguationIsPerParent is TestSpecReportPathDisambiguationIsPerParent for the
// arena/registry build path: nesting must not leak ordinals between unrelated parents there either.
func TestArenaReportPathDisambiguationIsPerParent(t *testing.T) {
	suite := buildAnalyzeSuite(t, "suite", func(s *Spec) {
		s.Describe("D", func(s *Spec) {
			s.Describe("X", func(s *Spec) { s.It("z", func(*Context) {}) })
			s.Describe("X", func(s *Spec) { s.It("z", func(*Context) {}) })
		})
		s.Describe("D", func(s *Spec) {
			s.Describe("X", func(s *Spec) { s.It("z", func(*Context) {}) })
		})
	})
	rep := &recordingReporter{}
	suite.Reporter = rep
	suite.Run(t)
	got := specPaths(rep)
	want := []string{
		"suite/D/X/z",
		"suite/D/X#2/z",
		"suite/D#2/X/z",
	}
	if !stringSlicesEqual(got, want) {
		t.Fatalf("report Paths = %q, want %q", got, want)
	}
}

// TestArenaReportPathLiteralCollisionBothOrders is TestSpecReportPathLiteralCollisionBothOrders for
// the arena/registry build path.
func TestArenaReportPathLiteralCollisionBothOrders(t *testing.T) {
	t.Run("literal declared after", func(t *testing.T) {
		suite := buildAnalyzeSuite(t, "suite", func(s *Spec) {
			s.Describe("D", func(s *Spec) { s.It("a", func(*Context) {}) })
			s.Describe("D", func(s *Spec) { s.It("b", func(*Context) {}) })
			s.Describe("D#2", func(s *Spec) { s.It("c", func(*Context) {}) })
		})
		rep := &recordingReporter{}
		suite.Reporter = rep
		suite.Run(t)
		got := specPaths(rep)
		want := []string{"suite/D/a", "suite/D#3/b", "suite/D#2/c"}
		if !stringSlicesEqual(got, want) {
			t.Fatalf("report Paths = %q, want %q", got, want)
		}
	})
	t.Run("literal declared before", func(t *testing.T) {
		suite := buildAnalyzeSuite(t, "suite", func(s *Spec) {
			s.Describe("D", func(s *Spec) { s.It("a", func(*Context) {}) })
			s.Describe("D#2", func(s *Spec) { s.It("c", func(*Context) {}) })
			s.Describe("D", func(s *Spec) { s.It("b", func(*Context) {}) })
		})
		rep := &recordingReporter{}
		suite.Reporter = rep
		suite.Run(t)
		got := specPaths(rep)
		want := []string{"suite/D/a", "suite/D#2/c", "suite/D#3/b"}
		if !stringSlicesEqual(got, want) {
			t.Fatalf("report Paths = %q, want %q", got, want)
		}
	})
}

// TestArenaReportPathRepeatedCompilationsAreDeterministic is
// TestSpecReportPathRepeatedCompilationsAreDeterministic for the arena/registry build path.
func TestArenaReportPathRepeatedCompilationsAreDeterministic(t *testing.T) {
	build := func() []string {
		suite := buildAnalyzeSuite(t, "suite", func(s *Spec) {
			s.Describe("D", func(s *Spec) { s.It("z", func(*Context) {}) })
			s.Describe("D", func(s *Spec) { s.It("z", func(*Context) {}) })
			s.Describe("D", func(s *Spec) { s.It("z", func(*Context) {}) })
		})
		rep := &recordingReporter{}
		suite.Reporter = rep
		suite.Run(t)
		return specPaths(rep)
	}
	first := build()
	second := build()
	if !stringSlicesEqual(first, second) {
		t.Fatalf("repeated compilations disagree: %q vs %q", first, second)
	}
	want := []string{"suite/D/z", "suite/D#2/z", "suite/D#3/z"}
	if !stringSlicesEqual(first, want) {
		t.Fatalf("report Paths = %q, want %q", first, want)
	}
}

// TestBuilderReportPathDisambiguatesDuplicateSiblings is
// TestSpecReportPathDisambiguatesDuplicateSiblings for the Builder/Program/Runner build path (issue
// #275's third engine).
func TestBuilderReportPathDisambiguatesDuplicateSiblings(t *testing.T) {
	rep := &recordingReporter{}
	b := NewBuilder()
	b.Describe("D", func() {
		b.It("z", func(*Context) {})
	})
	b.Describe("D", func() {
		b.It("z", func(*Context) {})
	})
	program := b.Build()
	got := runnerReportedPaths(t, program, rep)
	want := []string{"D/z", "D#2/z"}
	if !stringSlicesEqual(got, want) {
		t.Fatalf("report Paths = %q, want %q", got, want)
	}
}

// TestBuilderReportPathDisambiguatesThreeSiblings is TestSpecReportPathDisambiguatesThreeSiblings for
// the Builder/Program/Runner build path.
func TestBuilderReportPathDisambiguatesThreeSiblings(t *testing.T) {
	rep := &recordingReporter{}
	b := NewBuilder()
	for range 3 {
		b.Describe("D", func() {
			b.It("z", func(*Context) {})
		})
	}
	program := b.Build()
	got := runnerReportedPaths(t, program, rep)
	want := []string{"D/z", "D#2/z", "D#3/z"}
	if !stringSlicesEqual(got, want) {
		t.Fatalf("report Paths = %q, want %q", got, want)
	}
}

// TestBuilderReportPathDisambiguationIsPerParent is TestSpecReportPathDisambiguationIsPerParent for
// the Builder/Program/Runner build path.
func TestBuilderReportPathDisambiguationIsPerParent(t *testing.T) {
	rep := &recordingReporter{}
	b := NewBuilder()
	b.Describe("D", func() {
		b.Describe("X", func() { b.It("z", func(*Context) {}) })
		b.Describe("X", func() { b.It("z", func(*Context) {}) })
	})
	b.Describe("D", func() {
		b.Describe("X", func() { b.It("z", func(*Context) {}) })
	})
	program := b.Build()
	got := runnerReportedPaths(t, program, rep)
	want := []string{"D/X/z", "D/X#2/z", "D#2/X/z"}
	if !stringSlicesEqual(got, want) {
		t.Fatalf("report Paths = %q, want %q", got, want)
	}
}

// TestBuilderReportPathLiteralCollisionBothOrders is TestSpecReportPathLiteralCollisionBothOrders for
// the Builder/Program/Runner build path.
func TestBuilderReportPathLiteralCollisionBothOrders(t *testing.T) {
	t.Run("literal declared after", func(t *testing.T) {
		rep := &recordingReporter{}
		b := NewBuilder()
		b.Describe("D", func() { b.It("a", func(*Context) {}) })
		b.Describe("D", func() { b.It("b", func(*Context) {}) })
		b.Describe("D#2", func() { b.It("c", func(*Context) {}) })
		program := b.Build()
		got := runnerReportedPaths(t, program, rep)
		want := []string{"D/a", "D#3/b", "D#2/c"}
		if !stringSlicesEqual(got, want) {
			t.Fatalf("report Paths = %q, want %q", got, want)
		}
	})
	t.Run("literal declared before", func(t *testing.T) {
		rep := &recordingReporter{}
		b := NewBuilder()
		b.Describe("D", func() { b.It("a", func(*Context) {}) })
		b.Describe("D#2", func() { b.It("c", func(*Context) {}) })
		b.Describe("D", func() { b.It("b", func(*Context) {}) })
		program := b.Build()
		got := runnerReportedPaths(t, program, rep)
		want := []string{"D/a", "D#2/c", "D#3/b"}
		if !stringSlicesEqual(got, want) {
			t.Fatalf("report Paths = %q, want %q", got, want)
		}
	})
}

// TestBuilderReportPathRepeatedCompilationsAreDeterministic is
// TestSpecReportPathRepeatedCompilationsAreDeterministic for the Builder/Program/Runner build path.
func TestBuilderReportPathRepeatedCompilationsAreDeterministic(t *testing.T) {
	build := func() []string {
		rep := &recordingReporter{}
		b := NewBuilder()
		b.Describe("D", func() { b.It("z", func(*Context) {}) })
		b.Describe("D", func() { b.It("z", func(*Context) {}) })
		b.Describe("D", func() { b.It("z", func(*Context) {}) })
		program := b.Build()
		return runnerReportedPaths(t, program, rep)
	}
	first := build()
	second := build()
	if !stringSlicesEqual(first, second) {
		t.Fatalf("repeated compilations disagree: %q vs %q", first, second)
	}
	want := []string{"D/z", "D#2/z", "D#3/z"}
	if !stringSlicesEqual(first, want) {
		t.Fatalf("report Paths = %q, want %q", first, want)
	}
}

// TestBuilderReportPathNoDuplicatesUnchanged is TestSpecReportPathNoDuplicatesUnchanged for the
// Builder/Program/Runner build path.
func TestBuilderReportPathNoDuplicatesUnchanged(t *testing.T) {
	rep := &recordingReporter{}
	b := NewBuilder()
	b.Describe("when a", func() { b.It("does a", func(*Context) {}) })
	b.Describe("when b", func() { b.It("does b", func(*Context) {}) })
	program := b.Build()
	got := runnerReportedPaths(t, program, rep)
	want := []string{"when a/does a", "when b/does b"}
	if !stringSlicesEqual(got, want) {
		t.Fatalf("report Paths = %q, want %q", got, want)
	}
}

// duplicateSiblingSuite declares two sibling Describe("D") groups sharing the leaf It("z") — issue
// #275's reproduction shape — so the real-process test below can prove Go subtest identity and
// selection are byte-identical to #102's existing contract, unaffected by the report Path becoming
// disambiguated.
func duplicateSiblingSuite(s *Spec) {
	s.Describe("D", func(s *Spec) {
		s.It("z", func(*Context) { println("RAN first/z") })
	})
	s.Describe("D", func(s *Spec) {
		s.It("z", func(*Context) { println("RAN second/z") })
	})
}

// TestSpecRunDuplicateSiblingSubtestIdentityUnchangedRealProcess proves issue #275 changes only the
// reported Path: against a genuine `go test -v` run, two sibling Describe("D") groups sharing the
// leaf name "z" are still told apart solely by testing's own "#01" suffix — exactly #102's accepted,
// documented ambiguity (joinSubtestPath/subtest_identity.go) — never by any new segment this feature
// introduces. A subprocess is required: the assertion is about this process's own -v transcript.
func TestSpecRunDuplicateSiblingSubtestIdentityUnchangedRealProcess(t *testing.T) {
	if os.Getenv("GO_SPECS_DUP_SIBLING_HELPER") == "1" {
		Describe(t, "suite", duplicateSiblingSuite)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.v", "-test.run=^TestSpecRunDuplicateSiblingSubtestIdentityUnchangedRealProcess$")
	cmd.Env = append(os.Environ(), "GO_SPECS_DUP_SIBLING_HELPER=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helper run failed: %v\n%s", err, output)
	}
	out := string(output)
	for _, want := range []string{"/suite/D/z", "/suite/D/z#01"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected -v output to name the subtest %q (#102's unchanged contract), got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "D#2") {
		t.Fatalf("Go subtest identity must never carry the report-Path disambiguation suffix, got:\n%s", out)
	}
}

// TestSpecRunDuplicateSiblingSelectionByOrdinalRealProcess proves `go test -run` still selects one
// sibling of a duplicate-named pair only by testing's own "#01" ordinal — never by the new
// disambiguated report-Path label, which is not a valid -run pattern element and must not be treated
// as this feature's answer to #102's "select one sibling" limitation (#275 explicitly leaves it in
// place).
func TestSpecRunDuplicateSiblingSelectionByOrdinalRealProcess(t *testing.T) {
	if os.Getenv("GO_SPECS_DUP_SIBLING_SELECT_HELPER") == "1" {
		Describe(t, "suite", duplicateSiblingSuite)
		return
	}
	cmd := exec.Command(os.Args[0],
		"-test.v",
		"-test.run=^TestSpecRunDuplicateSiblingSelectionByOrdinalRealProcess$/^suite$/^D$/^z#01$",
	)
	cmd.Env = append(os.Environ(), "GO_SPECS_DUP_SIBLING_SELECT_HELPER=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helper run failed: %v\n%s", err, output)
	}
	out := string(output)
	if !strings.Contains(out, "RAN second/z") {
		t.Fatalf("expected the #01-selected second sibling's body to run, got:\n%s", out)
	}
	if strings.Contains(out, "RAN first/z") {
		t.Fatalf("expected the first, unselected sibling's body not to run, got:\n%s", out)
	}
}

// TestReportPathDisambiguationRendersAcrossFormats proves the disambiguated report Path reaches
// every rendered output go-specs produces, not just the in-memory SpecStartEvent: JSON's "path"
// array, JUnit XML's classname, and the plain-text/HTML failure line all show "D#2" for the second
// sibling Describe("D") group's hooked spec, exactly as they already show an ordinary group's Path.
func TestReportPathDisambiguationRendersAcrossFormats(t *testing.T) {
	c := report.NewCollector()
	c.SuiteStarted(report.SuiteStartEvent{Name: "Checkout"})
	suite := BuildSuite(nil, "Checkout", func(s *Spec) {
		s.Describe("D", func(s *Spec) {
			s.It("plain", func(*Context) {})
		})
		s.Describe("D", func(s *Spec) {
			s.BeforeAll(func(ctx *Context) { ctx.Expect(false).To(BeTrue()) })
			s.It("hooked", func(*Context) {})
		})
	})
	runCompiledSuiteWith(&controlledBackend{}, c, suite)
	c.SuiteFinished(report.SuiteEndEvent{Name: "Checkout"})
	normalized := c.Report()

	var jsonOut bytes.Buffer
	if err := report.RenderJSON(&jsonOut, normalized); err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}
	if !strings.Contains(jsonOut.String(), `"D#2"`) {
		t.Fatalf("expected JSON output's path array to contain %q, got:\n%s", "D#2", jsonOut.String())
	}

	var xmlOut bytes.Buffer
	if err := report.RenderXML(&xmlOut, normalized); err != nil {
		t.Fatalf("RenderXML: %v", err)
	}
	if !strings.Contains(xmlOut.String(), "D#2") {
		t.Fatalf("expected JUnit XML classname to contain %q, got:\n%s", "D#2", xmlOut.String())
	}

	var txtOut bytes.Buffer
	if err := report.RenderTXT(&txtOut, normalized); err != nil {
		t.Fatalf("RenderTXT: %v", err)
	}
	if !strings.Contains(txtOut.String(), "Checkout/D#2 [BeforeAll]") {
		t.Fatalf("expected TXT output to contain %q, got:\n%s", "Checkout/D#2 [BeforeAll]", txtOut.String())
	}

	var htmlOut bytes.Buffer
	if err := report.RenderHTML(&htmlOut, normalized); err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}
	if !strings.Contains(htmlOut.String(), "D#2") {
		t.Fatalf("expected HTML output to contain %q, got:\n%s", "D#2", htmlOut.String())
	}
}

// TestSkipMarkReportPathDisambiguatesDuplicateSiblings proves a compile-time SkipIt/PendingIt mark's
// reported Path is disambiguated exactly like a real spec's (issue #275), across both the
// bytecode-compiler and the arena/registry build paths.
func TestSkipMarkReportPathDisambiguatesDuplicateSiblings(t *testing.T) {
	build := func(s *Spec) {
		s.Describe("D", func(s *Spec) {
			s.SkipIt("skipped", func(*Context) {})
		})
		s.Describe("D", func(s *Spec) {
			s.PendingIt("pending", func(*Context) {})
		})
	}
	assertPaths := func(t *testing.T, suite *CompiledSuite) {
		t.Helper()
		rep := &recordingReporter{}
		runCompiledSuiteWith(&controlledBackend{}, rep, suite)
		got := specPaths(rep)
		want := []string{"Checkout/D/skipped", "Checkout/D#2/pending"}
		if !stringSlicesEqual(got, want) {
			t.Fatalf("report Paths = %q, want %q", got, want)
		}
	}
	t.Run("bytecode compiler", func(t *testing.T) {
		assertPaths(t, BuildSuite(nil, "Checkout", build))
	})
	t.Run("arena/registry", func(t *testing.T) {
		var suite *CompiledSuite
		Analyze(func() { suite = BuildSuite(nil, "Checkout", build) })
		assertPaths(t, suite)
	})
}

// runnerReportedPaths runs program with rep attached through NewRunner and returns every reported
// SpecStartEvent.Path, joined with "/", in report order.
func runnerReportedPaths(t *testing.T, program *Program, rep *recordingReporter) []string {
	t.Helper()
	r := NewRunner(program)
	r.Reporter = rep
	r.Run(t)
	return specPaths(rep)
}

// TestHookGroupReportPathDisambiguatesUnhookedSibling proves a hooked Describe group's synthetic
// [BeforeAll]/[AfterAll] case Path is disambiguated exactly like a real spec's Path (issue #275): the
// second sibling Describe("D") — the one that registers BeforeAll — gets "D#2", the same label an
// ordinary spec under it would get, while its Go subtest identity (validateHookGroups' collision
// check, based on the literal breadcrumb) is unaffected: only one of the two "D" siblings hooks, so
// there is no literal subtest-name collision for validateHookGroups to reject — proving the
// hooked/unhooked pairing this format must support.
func TestHookGroupReportPathDisambiguatesUnhookedSibling(t *testing.T) {
	rep := &recordingReporter{}
	suite := BuildSuite(nil, "Checkout", func(s *Spec) {
		s.Describe("D", func(s *Spec) {
			s.It("plain", func(*Context) {})
		})
		s.Describe("D", func(s *Spec) {
			s.BeforeAll(func(ctx *Context) { ctx.Expect(false).To(BeTrue()) })
			s.It("hooked", func(*Context) {})
		})
	})
	runCompiledSuiteWith(&controlledBackend{}, rep, suite)

	rep.mu.Lock()
	var gotSpecs []string
	for _, e := range rep.specStarted {
		if strings.HasPrefix(e.Name, "[") {
			continue // the synthetic [BeforeAll]/[AfterAll] hook case, asserted separately below
		}
		gotSpecs = append(gotSpecs, strings.Join(e.Path, "/"))
	}
	rep.mu.Unlock()
	wantSpecs := []string{"Checkout/D/plain", "Checkout/D#2/hooked"}
	if !stringSlicesEqual(gotSpecs, wantSpecs) {
		t.Fatalf("spec report Paths = %q, want %q", gotSpecs, wantSpecs)
	}

	var hookPath []string
	for _, e := range rep.specFinished {
		if e.Hook == report.HookBeforeAll {
			hookPath = e.Path
		}
	}
	wantHookPath := []string{"Checkout", "D#2"}
	if !stringSlicesEqual(hookPath, wantHookPath) {
		t.Fatalf("[BeforeAll] hook case Path = %q, want %q", hookPath, wantHookPath)
	}
}

// TestArenaHookGroupReportPathDisambiguatesUnhookedSibling is
// TestHookGroupReportPathDisambiguatesUnhookedSibling for the arena/registry build path.
func TestArenaHookGroupReportPathDisambiguatesUnhookedSibling(t *testing.T) {
	var suite *CompiledSuite
	Analyze(func() {
		suite = BuildSuite(nil, "Checkout", func(s *Spec) {
			s.Describe("D", func(s *Spec) {
				s.It("plain", func(*Context) {})
			})
			s.Describe("D", func(s *Spec) {
				s.BeforeAll(func(ctx *Context) { ctx.Expect(false).To(BeTrue()) })
				s.It("hooked", func(*Context) {})
			})
		})
	})
	rep := &recordingReporter{}
	runCompiledSuiteWith(&controlledBackend{}, rep, suite)

	rep.mu.Lock()
	var gotSpecs []string
	for _, e := range rep.specStarted {
		if strings.HasPrefix(e.Name, "[") {
			continue
		}
		gotSpecs = append(gotSpecs, strings.Join(e.Path, "/"))
	}
	rep.mu.Unlock()
	wantSpecs := []string{"Checkout/D/plain", "Checkout/D#2/hooked"}
	if !stringSlicesEqual(gotSpecs, wantSpecs) {
		t.Fatalf("spec report Paths = %q, want %q", gotSpecs, wantSpecs)
	}

	var hookPath []string
	for _, e := range rep.specFinished {
		if e.Hook == report.HookBeforeAll {
			hookPath = e.Path
		}
	}
	wantHookPath := []string{"Checkout", "D#2"}
	if !stringSlicesEqual(hookPath, wantHookPath) {
		t.Fatalf("[BeforeAll] hook case Path = %q, want %q", hookPath, wantHookPath)
	}
}
