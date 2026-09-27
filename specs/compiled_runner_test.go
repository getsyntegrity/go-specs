package specs

import "testing"

func TestCompiledRunner_OrderAndHooks(t *testing.T) {
	var order []string
	b := NewBuilder(32)
	b.BeforeEach(func(*Context) { order = append(order, "before1") })
	b.BeforeEach(func(*Context) { order = append(order, "before2") })
	b.AfterEach(func(*Context) { order = append(order, "after1") })
	b.AfterEach(func(*Context) { order = append(order, "after2") })
	b.It("", func(ctx *Context) {
		order = append(order, "spec1")
		EqualTo(ctx, 1, 1)
	})
	b.It("", func(ctx *Context) {
		order = append(order, "spec2")
		EqualTo(ctx, 2, 2)
	})
	prog := b.Build()
	runner := NewRunner(prog)
	runner.Run(t)

	// Each spec runs its own before/after (#109): coalescing into one group is a compile-time
	// optimization only, not a change in how often the hooks run.
	want := []string{
		"before1", "before2", "spec1", "after2", "after1",
		"before1", "before2", "spec2", "after2", "after1",
	}
	if len(order) != len(want) {
		t.Fatalf("order length: got %d, want %d", len(order), len(want))
	}
	for i := range want {
		if order[i] != want[i] {
			t.Errorf("order[%d]: got %q, want %q", i, order[i], want[i])
		}
	}
}
