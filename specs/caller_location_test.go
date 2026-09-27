package specs

import (
	"strings"
	"sync"
	"testing"
)

// withCaptureCallerLocation enables capture for the duration of the test and restores the previous
// setting afterwards, so a failing test cannot leak the toggle into the rest of the package.
func withCaptureCallerLocation(t *testing.T, enabled bool) {
	t.Helper()
	previous := CaptureCallerLocationEnabled()
	SetCaptureCallerLocation(enabled)
	t.Cleanup(func() { SetCaptureCallerLocation(previous) })
}

func TestCaptureCallerLocationIsDisabledByDefault(t *testing.T) {
	if CaptureCallerLocationEnabled() {
		t.Fatalf("expected caller-location capture to be disabled by default")
	}
	suite := Analyze(func() {
		Describe(nil, "Root", func(s *Spec) {
			s.It("leaf", func(ctx *Context) {})
		})
	})
	descID := suite.Arena.Children[suite.RootID][0]
	desc := &suite.Arena.Nodes[descID]
	if desc.File != "" || desc.Line != 0 {
		t.Fatalf("expected no location on describe node, got %q:%d", desc.File, desc.Line)
	}
	leafID := suite.Arena.Children[descID][0]
	leaf := &suite.Arena.Nodes[leafID]
	if leaf.File != "" || leaf.Line != 0 {
		t.Fatalf("expected no location on it node, got %q:%d", leaf.File, leaf.Line)
	}
}

func TestSetCaptureCallerLocationRoundTrips(t *testing.T) {
	withCaptureCallerLocation(t, true)
	if !CaptureCallerLocationEnabled() {
		t.Fatalf("expected capture to report enabled after SetCaptureCallerLocation(true)")
	}
	SetCaptureCallerLocation(false)
	if CaptureCallerLocationEnabled() {
		t.Fatalf("expected capture to report disabled after SetCaptureCallerLocation(false)")
	}
}

func TestEnabledCaptureRecordsDeclarationSiteOnEveryNode(t *testing.T) {
	withCaptureCallerLocation(t, true)

	suite := Analyze(func() {
		Describe(nil, "Root", func(s *Spec) {
			s.When("a condition holds", func(s *Spec) {
				s.It("leaf", func(ctx *Context) {})
			})
		})
	})

	descID := suite.Arena.Children[suite.RootID][0]
	whenID := suite.Arena.Children[descID][0]
	leafID := suite.Arena.Children[whenID][0]

	desc := &suite.Arena.Nodes[descID]
	when := &suite.Arena.Nodes[whenID]
	leaf := &suite.Arena.Nodes[leafID]

	for _, node := range []*ArenaNode{desc, when, leaf} {
		if !strings.HasSuffix(node.File, "caller_location_test.go") {
			t.Fatalf("expected %q to be declared in this test file, got %q", node.Name, node.File)
		}
		if node.Line <= 0 {
			t.Fatalf("expected a positive line for %q, got %d", node.Name, node.Line)
		}
	}

	// The three nodes are declared on consecutive source lines, so their recorded lines must
	// increase in declaration order.
	if desc.Line >= when.Line || when.Line >= leaf.Line {
		t.Fatalf("expected increasing declaration lines, got describe=%d when=%d it=%d",
			desc.Line, when.Line, leaf.Line)
	}
}

func TestConcurrentSuiteConstructionAndToggling(t *testing.T) {
	withCaptureCallerLocation(t, false)

	const (
		builders = 8
		togglers = 4
		rounds   = 50
	)

	var wg sync.WaitGroup
	start := make(chan struct{})

	for range builders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for range rounds {
				suite := Analyze(func() {
					Describe(nil, "Root", func(s *Spec) {
						s.When("a condition holds", func(s *Spec) {
							s.It("leaf", func(ctx *Context) {})
						})
					})
				})
				if suite == nil || suite.Arena == nil {
					panic("expected a suite tree from concurrent Analyze")
				}
			}
		}()
	}

	for i := range togglers {
		wg.Add(1)
		go func(enabled bool) {
			defer wg.Done()
			<-start
			for range rounds {
				SetCaptureCallerLocation(enabled)
				_ = CaptureCallerLocationEnabled()
				enabled = !enabled
			}
		}(i%2 == 0)
	}

	close(start)
	wg.Wait()
}
