package specs

import (
	"sync"
	"testing"
)

func TestBeforeAfterHooksNestedOrder(t *testing.T) {
	var mu sync.Mutex
	order := make([]string, 0)
	Describe(t, "HooksOrder", func(s *Spec) {
		s.BeforeEach(func(_ *Context) {
			mu.Lock()
			order = append(order, "before:outer")
			mu.Unlock()
		})
		s.AfterEach(func(_ *Context) {
			mu.Lock()
			order = append(order, "after:outer")
			mu.Unlock()
		})
		s.When("middle", func(mid *Spec) {
			mid.BeforeEach(func(_ *Context) {
				mu.Lock()
				order = append(order, "before:middle")
				mu.Unlock()
			})
			mid.AfterEach(func(_ *Context) {
				mu.Lock()
				order = append(order, "after:middle")
				mu.Unlock()
			})
			mid.When("inner", func(inner *Spec) {
				inner.BeforeEach(func(_ *Context) {
					mu.Lock()
					order = append(order, "before:inner")
					mu.Unlock()
				})
				inner.AfterEach(func(_ *Context) {
					mu.Lock()
					order = append(order, "after:inner")
					mu.Unlock()
				})
				inner.It("leaf", func(_ *Context) {
					mu.Lock()
					order = append(order, "test")
					mu.Unlock()
				})
			})
		})
	})
	expected := []string{
		"before:outer",
		"before:middle",
		"before:inner",
		"test",
		"after:inner",
		"after:middle",
		"after:outer",
	}
	if len(order) != len(expected) {
		t.Fatalf("order length mismatch: got %v", order)
	}
	for i, step := range expected {
		if order[i] != step {
			t.Fatalf("unexpected order at %d: got %s want %s", i, order[i], step)
		}
	}
}

func TestAfterEachRunsOnPanic(t *testing.T) {
	ranAfter := false
	ctx := &Context{backend: asTestBackend(t), T: t}
	func() {
		defer func() { _ = recover() }()
		defer runAfterHooks(ctx, []Fixture{func(*Context) { ranAfter = true }})
		panic("boom")
	}()
	if !ranAfter {
		t.Fatal("expected AfterEach hooks to run even when panic occurs")
	}
}

func TestMultipleHooksPerLevel(t *testing.T) {
	order := make([]string, 0)
	Describe(t, "MultipleHooks", func(s *Spec) {
		s.BeforeEach(func(_ *Context) { order = append(order, "before1") })
		s.BeforeEach(func(_ *Context) { order = append(order, "before2") })
		s.AfterEach(func(_ *Context) { order = append(order, "after1") })
		s.AfterEach(func(_ *Context) { order = append(order, "after2") })
		s.It("runs", func(_ *Context) {
			order = append(order, "test")
		})
	})
	expected := []string{"before1", "before2", "test", "after2", "after1"}
	if len(order) != len(expected) {
		t.Fatalf("unexpected order length %v", order)
	}
	for i, step := range expected {
		if order[i] != step {
			t.Fatalf("unexpected order at %d: got %s want %s", i, order[i], step)
		}
	}
}

func TestAfterEachLegacyScope(t *testing.T) {
	order := make([]string, 0)
	Describe(t, "LegacyHooks", func(s *Spec) {
		s.AfterEach(func(_ *Context) { order = append(order, "after:outer") })
		s.When("legacy", func() {
			s.AfterEach(func(_ *Context) { order = append(order, "after:inner") })
			s.It("leaf", func(_ *Context) {
				order = append(order, "test")
			})
		})
	})
	expected := []string{"test", "after:inner", "after:outer"}
	if len(order) != len(expected) {
		t.Fatalf("unexpected order %v", order)
	}
	for i, step := range expected {
		if order[i] != step {
			t.Fatalf("unexpected order at %d: got %s want %s", i, order[i], step)
		}
	}
}
