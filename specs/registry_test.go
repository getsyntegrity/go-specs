package specs

import (
	"fmt"
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

// The mutating extension helpers have nowhere to write with no active registry. Discarding the
// argument would let a suite that registered nothing report green, so they fail closed (issue #151).
// The panic names the helper, because the call site is the only place that can fix the mistake.
func TestAppendHooksAndSetPathGenPanicWithoutRegistry(t *testing.T) {
	cases := []struct {
		helper string
		call   func()
	}{
		{"AppendBeforeHook", func() { AppendBeforeHook(func(ctx *Context) {}) }},
		{"AppendAfterHook", func() { AppendAfterHook(func(ctx *Context) {}) }},
		{"SetPathGen", func() { SetPathGen(&PathGenerator{}) }},
	}
	for _, tc := range cases {
		t.Run(tc.helper, func(t *testing.T) {
			msg := recoverMessage(t, tc.call)
			if !strings.Contains(msg, tc.helper) {
				t.Fatalf("panic must name the helper; got %q", msg)
			}
			if !strings.Contains(msg, "no active registry") {
				t.Fatalf("panic must explain the missing registry; got %q", msg)
			}
		})
	}
	if CurrentArena() != nil {
		t.Fatal("a failed helper call must not leave a registry behind")
	}
}

// The helpers are scoped to the goroutine Analyze pushed on. A goroutine started inside Analyze has
// its own (empty) registry stack, so writing from it would be discarded and must fail instead.
func TestAppendBeforeHookPanicsOnAnotherGoroutineInsideAnalyze(t *testing.T) {
	var msg string
	Analyze(func() {
		done := make(chan struct{})
		go func() {
			defer close(done)
			defer func() {
				if r := recover(); r != nil {
					msg = fmt.Sprint(r)
				}
			}()
			AppendBeforeHook(func(ctx *Context) {})
		}()
		<-done
	})
	if !strings.Contains(msg, "AppendBeforeHook") {
		t.Fatalf("expected a panic naming AppendBeforeHook on the child goroutine; got %q", msg)
	}
}

// recoverMessage runs fn and returns the message it panicked with, failing the test when it returns
// normally. A helper that silently accepts the call is exactly the defect under test.
func recoverMessage(t *testing.T, fn func()) (msg string) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected a panic, call returned normally")
		}
		msg = fmt.Sprint(r)
	}()
	fn()
	return ""
}

// The node stack is never empty in correct code: newRegistry seeds it with the suite root and the
// pop closure only shrinks it while len(stack) > 1. The guard exists so a future change that does
// break the invariant fails here instead of silently dropping the registration, so it is pinned
// against the one construction that can violate it — a registry that newRegistry never built.
func TestRegistryWritersPanicOnEmptyNodeStack(t *testing.T) {
	writers := map[string]func(r *registry){
		"enterNode":        func(r *registry) { r.enterNode(ItNode, "adds correctly", "", 0, nil) },
		"appendBeforeHook": func(r *registry) { r.appendBeforeHook(func(ctx *Context) {}) },
		"appendAfterHook":  func(r *registry) { r.appendAfterHook(func(ctx *Context) {}) },
		"setPathGen":       func(r *registry) { r.setPathGen(&PathGenerator{}) },
	}
	for name, write := range writers {
		t.Run(name, func(t *testing.T) {
			msg := recoverMessage(t, func() { write(&registry{}) })
			if !strings.Contains(msg, "registry invariant violated") {
				t.Fatalf("expected the stack-invariant panic; got %q", msg)
			}
		})
	}
}

// The invariant holds across nesting: every pop leaves the root in place, so the stack top is always
// a real arena node and repeated exits never drain it.
func TestRegistryStackKeepsRootAfterNestedExits(t *testing.T) {
	r := newRegistry()
	_, popOuter := r.enterNode(DescribeNode, "Calculator", "", 0, nil)
	_, popInner := r.enterNode(WhenNode, "adding numbers", "", 0, nil)
	popInner()
	popOuter()
	popOuter() // an extra pop must not drain the stack past the root
	r.mu.Lock()
	depth := len(r.stack)
	r.mu.Unlock()
	if depth != 1 {
		t.Fatalf("expected the stack to rest at the root, got depth %d", depth)
	}
	// The root is still writable, which is what the invariant buys.
	r.appendBeforeHook(func(ctx *Context) {})
	if got := len(r.arena.BeforeHooks[0]); got != 1 {
		t.Fatalf("expected the hook to land on the root node, got %d", got)
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
