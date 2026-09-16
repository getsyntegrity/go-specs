package specs

import (
	"testing"
)

func TestCompileBlocks(t *testing.T) {
	specs := make([]RunSpec, 10)
	for i := range specs {
		i := i
		specs[i] = RunSpec{Name: "s", Fn: func(*Context) {}}
		_ = i
	}
	fns, blocks := CompileBlocks(specs, 8)
	if len(fns) != 10 {
		t.Fatalf("fns len: got %d, want 10", len(fns))
	}
	if len(blocks) != 2 {
		t.Fatalf("blocks len: got %d, want 2 (10/8 ceil)", len(blocks))
	}
	if blocks[0].start != 0 || blocks[0].count != 8 {
		t.Errorf("block 0: start=%d count=%d, want start=0 count=8", blocks[0].start, blocks[0].count)
	}
	if blocks[1].start != 8 || blocks[1].count != 2 {
		t.Errorf("block 1: start=%d count=%d, want start=8 count=2", blocks[1].start, blocks[1].count)
	}
}

func TestBlockRunner_Run(t *testing.T) {
	var ran []int
	specs := make([]RunSpec, 5)
	for i := range specs {
		i := i
		specs[i] = RunSpec{Name: "s", Fn: func(*Context) { ran = append(ran, i) }}
	}
	fns, blocks := CompileBlocks(specs, 2)
	r := NewBlockRunner(fns, blocks)
	r.Run(t)
	if len(ran) != 5 {
		t.Fatalf("ran %d specs, want 5", len(ran))
	}
	for i := 0; i < 5; i++ {
		if ran[i] != i {
			t.Errorf("order: ran[%d]=%d, want %d", i, ran[i], i)
		}
	}
}

func TestCompileBlocks_DefaultSize(t *testing.T) {
	specs := make([]RunSpec, 3)
	for i := range specs {
		specs[i] = RunSpec{Fn: func(*Context) {}}
	}
	_, blocks := CompileBlocks(specs, 0)
	if len(blocks) != 1 || blocks[0].count != 3 {
		t.Errorf("blockSize 0 should use default 8: got 1 block with count %d", blocks[0].count)
	}
}

func BenchmarkRunner_Minimal_Loop(b *testing.B) {
	const n = 2000
	r := NewMinimalRunner(n)
	for i := 0; i < n; i++ {
		r.Add("spec", func(ctx *Context) { EqualTo(ctx, 1, 1) })
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Run(b)
	}
}

func BenchmarkRunner_Block_8(b *testing.B) {
	const n = 2000
	specs := make([]RunSpec, n)
	for i := range specs {
		specs[i] = RunSpec{Name: "spec", Fn: func(ctx *Context) { EqualTo(ctx, 1, 1) }}
	}
	fns, blocks := CompileBlocks(specs, 8)
	runner := NewBlockRunner(fns, blocks)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runner.Run(b)
	}
}

func BenchmarkRunner_Block_16(b *testing.B) {
	const n = 2000
	specs := make([]RunSpec, n)
	for i := range specs {
		specs[i] = RunSpec{Name: "spec", Fn: func(ctx *Context) { EqualTo(ctx, 1, 1) }}
	}
	fns, blocks := CompileBlocks(specs, 16)
	runner := NewBlockRunner(fns, blocks)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runner.Run(b)
	}
}

func BenchmarkRunner_Block_32(b *testing.B) {
	const n = 2000
	specs := make([]RunSpec, n)
	for i := range specs {
		specs[i] = RunSpec{Name: "spec", Fn: func(ctx *Context) { EqualTo(ctx, 1, 1) }}
	}
	fns, blocks := CompileBlocks(specs, 32)
	runner := NewBlockRunner(fns, blocks)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runner.Run(b)
	}
}
