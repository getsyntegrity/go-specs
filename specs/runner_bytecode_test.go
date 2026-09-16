package specs

import "testing"

func TestBytecodeRunner_OrderAndHooks(t *testing.T) {
	var order []string
	b := NewBCBuilder(32)
	b.AddBefore(func(*Context) { order = append(order, "before1") })
	b.AddBefore(func(*Context) { order = append(order, "before2") })
	b.AddAfter(func(*Context) { order = append(order, "after1") })
	b.AddAfter(func(*Context) { order = append(order, "after2") })
	b.AddSpec(func(ctx *Context) {
		order = append(order, "spec1")
		EqualTo(ctx, 1, 1)
	})
	b.AddSpec(func(ctx *Context) {
		order = append(order, "spec2")
		EqualTo(ctx, 2, 2)
	})
	prog := b.BuildBC()
	runner := NewBytecodeRunner(prog)
	runner.Run(t)

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

func TestBytecodeRunner_SpecStarts(t *testing.T) {
	b := NewBCBuilder(8)
	b.AddBefore(func(*Context) {})
	b.AddSpec(func(ctx *Context) { EqualTo(ctx, 1, 1) })
	b.AddSpec(func(ctx *Context) { EqualTo(ctx, 2, 2) })
	prog := b.BuildBC()
	if prog.NumSpecs() != 2 {
		t.Errorf("NumSpecs: got %d, want 2", prog.NumSpecs())
	}
	if len(prog.SpecStarts) != 3 {
		t.Errorf("SpecStarts len: got %d, want 3", len(prog.SpecStarts))
	}
	// One before, zero afters: 1 before + 1 runSpec per spec. 2 specs = 4 instructions.
	if prog.BCLen() != 4 {
		t.Errorf("BCLen: got %d, want 4", prog.BCLen())
	}
}

func TestBytecodeRunner_RunParallel(t *testing.T) {
	b := NewBCBuilder(16)
	for i := 0; i < 10; i++ {
		b.AddSpec(func(ctx *Context) { EqualTo(ctx, 1, 1) })
	}
	prog := b.BuildBC()
	runner := NewBytecodeRunner(prog)
	runner.RunParallel(t, 4)
}
