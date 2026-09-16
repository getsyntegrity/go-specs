package specs

import (
	"sync"
	"testing"
)

// TestBuildSuiteConcurrentTopLevelSuitesDoNotCrossContaminate verifies #25's fix for the bytecode
// compiler path (top-level Describe/BuildSuite with no Analyze wrapping it): two goroutines building
// unrelated suites concurrently must never have one goroutine's It land in the other's plan.
//
// Deterministic reproduction, not a timing gamble: both goroutines are let run until each has pushed
// its own compiler and is blocked mid-body, so an implementation using a single shared "current
// compiler" (the pre-fix global stack) would deterministically resolve both It calls to whichever
// compiler was pushed last, corrupting one of the two plans regardless of scheduling order.
func TestBuildSuiteConcurrentTopLevelSuitesDoNotCrossContaminate(t *testing.T) {
	aReady, bReady := make(chan struct{}), make(chan struct{})
	aGo, bGo := make(chan struct{}), make(chan struct{})

	var suiteA, suiteB *CompiledSuite
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		suiteA = BuildSuite(nil, "SuiteA", func(s *Spec) {
			close(aReady)
			<-aGo
			s.It("a1", func(ctx *Context) {})
		})
	}()
	go func() {
		defer wg.Done()
		suiteB = BuildSuite(nil, "SuiteB", func(s *Spec) {
			close(bReady)
			<-bGo
			s.It("b1", func(ctx *Context) {})
		})
	}()

	<-aReady
	<-bReady
	close(aGo)
	close(bGo)
	wg.Wait()

	if suiteA == nil || suiteB == nil {
		t.Fatal("expected both suites to be built")
	}
	if got := suiteA.Plan.Names; len(got) != 1 || got[0] != "a1" {
		t.Fatalf("suite A plan corrupted: got names %v, want [a1]", got)
	}
	if got := suiteB.Plan.Names; len(got) != 1 || got[0] != "b1" {
		t.Fatalf("suite B plan corrupted: got names %v, want [b1]", got)
	}
}

// TestAnalyzeConcurrentSuitesDoNotCrossContaminate verifies #25's fix for the registry/arena path
// (Describe nested inside Analyze): two goroutines each running their own Analyze(func(){ Describe(...) })
// concurrently must not have one goroutine's It land in the other's arena. Same deterministic barrier
// technique as the compiler-path test above — both Describe calls push their registry and reach the
// blocked It call before either releases, so a shared top-of-stack lookup would deterministically
// resolve to whichever registry was pushed last for both goroutines.
func TestAnalyzeConcurrentSuitesDoNotCrossContaminate(t *testing.T) {
	aReady, bReady := make(chan struct{}), make(chan struct{})
	aGo, bGo := make(chan struct{}), make(chan struct{})

	var treeA, treeB *SuiteTree
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		treeA = Analyze(func() {
			Describe(nil, "RootA", func(s *Spec) {
				close(aReady)
				<-aGo
				s.It("a1", func(ctx *Context) {})
			})
		})
	}()
	go func() {
		defer wg.Done()
		treeB = Analyze(func() {
			Describe(nil, "RootB", func(s *Spec) {
				close(bReady)
				<-bGo
				s.It("b1", func(ctx *Context) {})
			})
		})
	}()

	<-aReady
	<-bReady
	close(aGo)
	close(bGo)
	wg.Wait()

	assertSingleItLeaf(t, treeA, "RootA", "a1")
	assertSingleItLeaf(t, treeB, "RootB", "b1")
}

// assertSingleItLeaf checks tree has exactly one Describe child named describeName, itself with
// exactly one It child named itName.
func assertSingleItLeaf(t *testing.T, tree *SuiteTree, describeName, itName string) {
	t.Helper()
	if tree == nil || tree.Arena == nil {
		t.Fatalf("expected a suite tree for %q", describeName)
	}
	rootID := tree.RootID
	children := tree.Arena.Children[rootID]
	if len(children) != 1 {
		t.Fatalf("%q: expected 1 root child, got %d", describeName, len(children))
	}
	descID := children[0]
	desc := &tree.Arena.Nodes[descID]
	if desc.Name != describeName || desc.Type != DescribeNode {
		t.Fatalf("%q: expected describe node %q, got %+v", describeName, describeName, desc)
	}
	leafChildren := tree.Arena.Children[descID]
	if len(leafChildren) != 1 {
		t.Fatalf("%q: expected 1 it child under %q, got %d: %v", describeName, describeName, len(leafChildren), leafChildren)
	}
	leafID := leafChildren[0]
	leaf := &tree.Arena.Nodes[leafID]
	if leaf.Name != itName || leaf.Type != ItNode {
		t.Fatalf("%q: expected it node %q, got %+v", describeName, itName, leaf)
	}
}

// TestNestedDescribeWhenStillWorksAfterExplicitThreading is a plain sanity check (no concurrency)
// that threading compiler/registry explicitly through Spec (instead of resolving via package-level
// global state) didn't change behavior for ordinary nested Describe/When/BeforeEach/AfterEach/It use,
// in both the bytecode-compiler path (top-level Describe, no Analyze) and the registry path (Describe
// nested inside Analyze).
func TestNestedDescribeWhenStillWorksAfterExplicitThreading(t *testing.T) {
	t.Run("compiler path", func(t *testing.T) {
		var order []string
		suite := BuildSuite(nil, "Root", func(s *Spec) {
			s.BeforeEach(func(ctx *Context) { order = append(order, "root-before") })
			s.Describe("Mid", func(s *Spec) {
				s.When("Sub", func(s *Spec) {
					s.BeforeEach(func(ctx *Context) { order = append(order, "sub-before") })
					s.It("leaf", func(ctx *Context) { order = append(order, "leaf") })
					s.AfterEach(func(ctx *Context) { order = append(order, "sub-after") })
				})
			})
		})
		if len(suite.Plan.Names) != 1 || suite.Plan.Names[0] != "leaf" {
			t.Fatalf("expected one spec named leaf, got %v", suite.Plan.Names)
		}
		if want := "Root/Mid/Sub/leaf"; suite.Plan.FullNames[0] != want {
			t.Fatalf("expected full name %q, got %q", want, suite.Plan.FullNames[0])
		}
	})

	t.Run("registry path", func(t *testing.T) {
		tree := Analyze(func() {
			Describe(nil, "Root", func(s *Spec) {
				s.Describe("Mid", func(s *Spec) {
					s.When("Sub", func(s *Spec) {
						s.It("leaf", func(ctx *Context) {})
					})
				})
			})
		})
		rootID := tree.RootID
		topID := tree.Arena.Children[rootID][0]
		top := &tree.Arena.Nodes[topID]
		if top.Name != "Root" || top.Type != DescribeNode {
			t.Fatalf("expected Root describe node, got %+v", top)
		}
		midID := tree.Arena.Children[topID][0]
		mid := &tree.Arena.Nodes[midID]
		if mid.Name != "Mid" || mid.Type != DescribeNode {
			t.Fatalf("expected Mid describe node, got %+v", mid)
		}
		subID := tree.Arena.Children[midID][0]
		sub := &tree.Arena.Nodes[subID]
		if sub.Name != "Sub" || sub.Type != WhenNode {
			t.Fatalf("expected Sub when node, got %+v", sub)
		}
		leafID := tree.Arena.Children[subID][0]
		leaf := &tree.Arena.Nodes[leafID]
		if leaf.Name != "leaf" || leaf.Type != ItNode {
			t.Fatalf("expected leaf it node, got %+v", leaf)
		}
	})
}
