package specs

import (
	"strings"
	"testing"
)

type quantLine struct {
	SKU     string
	Shipped bool
}

func shipped() Matcher {
	return Satisfy("a shipped line", func(v any) bool { l, ok := v.(quantLine); return ok && l.Shipped })
}

// The quantified matchers are re-exported so the DSL reads specs.EveryElement(...); these tests pin
// the re-exports and the failure path through Expect(...).To(...), where the failing index must
// reach the reported message.
func TestQuantifiedMatchersAreReExported(t *testing.T) {
	lines := []quantLine{{"a", true}, {"b", false}, {"c", true}}

	ctx, b := newCapturedContext()
	ctx.Expect(lines).To(AnyElement(shipped()))
	ctx.Expect(lines).To(ExactlyNElements(2, shipped()))
	ctx.Expect(lines).To(AtLeastNElements(2, shipped()))
	ctx.Expect(lines).To(AtMostNElements(2, shipped()))
	ctx.Expect(lines).To(Not(EveryElement(shipped())))
	ctx.Expect([]quantLine{}).To(NoElement(shipped()))
	ctx.Expect([]int{1, 2, 3}).To(HaveElementsInOrder(Equal(1), Equal(2), Equal(3)))
	ctx.Expect([]int{1, 2, 3}).To(ContainElementsInOrder(Equal(1), Equal(3)))
	ctx.Expect(lines).To(All(AnyElement(shipped()), Not(NoElement(shipped()))))
	if b.failed {
		t.Fatalf("unexpected failure: %q", b.message)
	}

	ctx, b = newCapturedContext()
	ctx.Expect(lines).To(EveryElement(shipped()))
	if !b.failed || !strings.Contains(b.message, "1 of 3 elements failed") || !strings.Contains(b.message, "[1] {b false}") {
		t.Fatalf("message = %q, want the failing index 1", b.message)
	}
}
