package specs

import (
	"strings"
	"testing"
)

// analyzeSample builds a small three-level suite used by the registry helper tests.
func analyzeSample() *SuiteTree {
	return Analyze(func() {
		Describe(nil, "Calculator", func(s *Spec) {
			s.When("adding numbers", func(child *Spec) {
				child.It("adds correctly", func(ctx *Context) {})
				child.It("handles negatives", func(ctx *Context) {})
			})
		})
	})
}

// PrintTreeArena prints the node at rootID itself and then its descendants, so rootID 0 includes the
// synthetic "suite" root line. That differs from SuiteTree.Tree, which skips the suite root; the two
// are not interchangeable and this test pins the difference.
func TestPrintTreeArenaPrintsRootAndDescendants(t *testing.T) {
	suite := analyzeSample()
	var b strings.Builder
	PrintTreeArena(suite.Arena, 0, 0, &b)
	got := b.String()
	want := "suite\n  Calculator\n    adding numbers\n      adds correctly\n      handles negatives\n"
	if got != want {
		t.Fatalf("unexpected tree:\ngot:\n%swant:\n%s", got, want)
	}
}

func TestPrintTreeArenaFromSubtree(t *testing.T) {
	suite := analyzeSample()
	// Node 1 is "Calculator": the single child of the suite root.
	var b strings.Builder
	PrintTreeArena(suite.Arena, suite.Arena.Children[0][0], 0, &b)
	want := "Calculator\n  adding numbers\n    adds correctly\n    handles negatives\n"
	if got := b.String(); got != want {
		t.Fatalf("unexpected subtree:\ngot:\n%swant:\n%s", got, want)
	}
}

// A nil arena, an out-of-range rootID and a nil writer are documented no-ops rather than panics, so
// callers can print a tree that may not have been built.
func TestPrintTreeArenaTolerantOfMissingInput(t *testing.T) {
	var b strings.Builder
	PrintTreeArena(nil, 0, 0, &b)
	PrintTreeArena(analyzeSample().Arena, -1, 0, &b)
	PrintTreeArena(analyzeSample().Arena, 999, 0, &b)
	if got := b.String(); got != "" {
		t.Fatalf("expected no output, got %q", got)
	}
	// nil writer must not panic.
	PrintTreeArena(analyzeSample().Arena, 0, 0, nil)
}

func TestCurrentArenaAndCurrentSuiteOutsideAnalyzeAreNil(t *testing.T) {
	if arena := CurrentArena(); arena != nil {
		t.Fatalf("expected nil arena with no active registry, got %v", arena)
	}
	if suite := CurrentSuite(); suite != nil {
		t.Fatalf("expected nil suite with no active registry, got %v", suite)
	}
}

func TestCurrentArenaAndCurrentSuiteInsideAnalyze(t *testing.T) {
	var innerArena *NodeArena
	var innerTree string
	suite := Analyze(func() {
		Describe(nil, "Calculator", func(s *Spec) {
			s.It("adds correctly", func(ctx *Context) {})
		})
		innerArena = CurrentArena()
		if inner := CurrentSuite(); inner != nil {
			innerTree = inner.Tree()
		}
	})
	if innerArena == nil {
		t.Fatal("expected CurrentArena to return the active arena inside Analyze")
	}
	if innerArena != suite.Arena {
		t.Fatal("CurrentArena returned a different arena than the one Analyze returned")
	}
	if want := "Calculator\n  adds correctly"; innerTree != want {
		t.Fatalf("CurrentSuite tree mismatch: got %q want %q", innerTree, want)
	}
}

// AppendBeforeHook and AppendAfterHook attach to the node on top of the registry stack, which is the
// suite root when they are called directly under Analyze. They are the exported counterpart of
// Spec.BeforeEach/AfterEach for code building into the registry without a *Spec.
func TestAppendHooksAttachToCurrentNode(t *testing.T) {
	hook := func(ctx *Context) {}
	suite := Analyze(func() {
		AppendBeforeHook(hook)
		AppendAfterHook(hook)
		AppendAfterHook(hook)
	})
	if got := len(suite.Arena.BeforeHooks[0]); got != 1 {
		t.Fatalf("expected 1 before hook on the root node, got %d", got)
	}
	if got := len(suite.Arena.AfterHooks[0]); got != 2 {
		t.Fatalf("expected 2 after hooks on the root node, got %d", got)
	}
}

func TestAppendHooksAndSetPathGenAreNoOpsWithoutRegistry(t *testing.T) {
	// No active registry: these must not panic and must not create one.
	AppendBeforeHook(func(ctx *Context) {})
	AppendAfterHook(func(ctx *Context) {})
	SetPathGen(&PathGenerator{})
	if CurrentArena() != nil {
		t.Fatal("helpers must not create a registry when none is active")
	}
}

func TestSetPathGenSetsGeneratorOnCurrentNode(t *testing.T) {
	gen := &PathGenerator{}
	suite := Analyze(func() {
		SetPathGen(gen)
	})
	if suite.Arena.Nodes[0].PathGen != gen {
		t.Fatal("SetPathGen did not set the generator on the current node")
	}
}
