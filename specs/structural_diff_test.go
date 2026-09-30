package specs

import (
	"strings"
	"testing"
)

type sdItem struct{ Price float64 }

type sdOrder struct {
	ID    int
	Items []sdItem
}

// The structural diff reaches the spec through both untyped equality entry points, and the
// failure stays a single recorded message whose first line is the familiar "expected ... to equal".
func TestExpectToEqualAndEqualMatcherReportStructuralDiffPaths(t *testing.T) {
	expected := sdOrder{ID: 1, Items: []sdItem{{1}, {2}, {9.99}}}
	actual := sdOrder{ID: 1, Items: []sdItem{{1}, {2}, {12.5}}}
	const want = "sdOrder.Items[2].Price: expected 9.99, actual 12.5"

	ctx, b := newCapturedContext()
	ctx.Expect(actual).ToEqual(expected)
	if !b.failed || !strings.HasPrefix(b.message, "expected {1 [") || !strings.Contains(b.message, "\ndifferences:\n  "+want) {
		t.Fatalf("ToEqual failure lacks the diff path %q:\n%s", want, b.message)
	}

	ctx, b = newCapturedContext()
	ctx.Expect(actual).To(Equal(expected))
	if !b.failed || !strings.Contains(b.message, "\ndifferences:\n  "+want) {
		t.Fatalf("Equal failure lacks the diff path %q:\n%s", want, b.message)
	}
}

func TestExpectToEqualPassingValuesReportNothing(t *testing.T) {
	ctx, b := newCapturedContext()
	ctx.Expect(sdOrder{ID: 1, Items: []sdItem{{1}}}).ToEqual(sdOrder{ID: 1, Items: []sdItem{{1}}})
	if b.failed {
		t.Fatalf("unexpected failure: %s", b.message)
	}
}
