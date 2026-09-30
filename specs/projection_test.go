package specs

import "testing"

type projectionOrder struct {
	Status string
}

func TestProjectReExportPassesAndReportsTheFieldPath(t *testing.T) {
	status := Project("Status", func(o projectionOrder) string { return o.Status }, Equal("paid"))

	ctx, b := newCapturedContext()
	ctx.Expect(projectionOrder{Status: "paid"}).To(status)
	if b.failed {
		t.Fatalf("expected no failure, got %q", b.message)
	}

	ctx, b = newCapturedContext()
	ctx.Expect(projectionOrder{Status: "open"}).To(All(status))
	if !b.failed {
		t.Fatal("expected a failure")
	}
	want := `All: #1: "Status: expected open to equal paid"`
	if b.message != want {
		t.Fatalf("message = %q, want %q", b.message, want)
	}
}
