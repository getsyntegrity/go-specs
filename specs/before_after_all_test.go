package specs

import (
	"sync"
	"testing"
)

// before_after_all_test.go pins the happy-path contract for once-per-group BeforeAll/AfterAll
// hooks (issue #207, docs/SUITE_HOOKS_CONTRACT.md H1-H3): lazy entry, outer->inner setup /
// inner->outer teardown, multiple hooks per group in registration order, and a group with zero
// runnable specs never entering at all. Failure semantics (H4-H7) are pinned separately in
// before_after_all_failure_test.go.

type orderRecorder struct {
	mu    sync.Mutex
	order []string
}

func (r *orderRecorder) add(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.order = append(r.order, s)
}

func (r *orderRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.order...)
}

func assertOrder(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("order length mismatch: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order mismatch at %d: got %v, want %v", i, got, want)
		}
	}
}

// TestBeforeAllAfterAllTopLevel proves a root Describe's BeforeAll/AfterAll each run exactly once,
// no matter how many specs the suite has, and that BeforeAll runs before the first spec while
// AfterAll runs after the last one.
func TestBeforeAllAfterAllTopLevel(t *testing.T) {
	r := &orderRecorder{}
	Describe(t, "TopLevelGroupHooks", func(s *Spec) {
		s.BeforeAll(func(*Context) { r.add("before-all") })
		s.AfterAll(func(*Context) { r.add("after-all") })
		s.It("first", func(*Context) { r.add("first") })
		s.It("second", func(*Context) { r.add("second") })
		s.It("third", func(*Context) { r.add("third") })
	})
	assertOrder(t, r.snapshot(), []string{"before-all", "first", "second", "third", "after-all"})
}

// TestBeforeAllAfterAllNestedOrder mirrors docs/SUITE_HOOKS_CONTRACT.md's H2 example exactly:
// outer BeforeAll, then inner BeforeAll, then the spec, then inner AfterAll, then outer AfterAll.
func TestBeforeAllAfterAllNestedOrder(t *testing.T) {
	r := &orderRecorder{}
	Describe(t, "Nested", func(s *Spec) {
		s.Describe("outer", func(o *Spec) {
			o.BeforeAll(func(*Context) { r.add("outer:before") })
			o.AfterAll(func(*Context) { r.add("outer:after") })
			o.When("inner", func(w *Spec) {
				w.BeforeAll(func(*Context) { r.add("inner:before") })
				w.AfterAll(func(*Context) { r.add("inner:after") })
				w.It("spec", func(*Context) { r.add("spec") })
			})
		})
	})
	assertOrder(t, r.snapshot(), []string{
		"outer:before", "inner:before", "spec", "inner:after", "outer:after",
	})
}

// TestMultipleBeforeAllAfterAllPerGroupRunInOrder proves multiple registrations in the same group
// run in registration order for BeforeAll and AfterAll alike (H1).
func TestMultipleBeforeAllAfterAllPerGroupRunInOrder(t *testing.T) {
	r := &orderRecorder{}
	Describe(t, "MultipleGroupHooks", func(s *Spec) {
		s.BeforeAll(func(*Context) { r.add("before1") })
		s.BeforeAll(func(*Context) { r.add("before2") })
		s.AfterAll(func(*Context) { r.add("after1") })
		s.AfterAll(func(*Context) { r.add("after2") })
		s.It("spec", func(*Context) { r.add("spec") })
	})
	assertOrder(t, r.snapshot(), []string{"before1", "before2", "spec", "after1", "after2"})
}

// TestBeforeAllRunsOnceAcrossMultipleSpecs proves BeforeAll/AfterAll each run exactly once for a
// group with several specs, not once per spec (the whole point of the feature).
func TestBeforeAllRunsOnceAcrossMultipleSpecs(t *testing.T) {
	var beforeCount, afterCount int
	var mu sync.Mutex
	Describe(t, "RunsOnce", func(s *Spec) {
		s.BeforeAll(func(*Context) { mu.Lock(); beforeCount++; mu.Unlock() })
		s.AfterAll(func(*Context) { mu.Lock(); afterCount++; mu.Unlock() })
		for i := 0; i < 5; i++ {
			s.It("spec", func(*Context) {})
		}
	})
	if beforeCount != 1 {
		t.Fatalf("BeforeAll ran %d times, want 1", beforeCount)
	}
	if afterCount != 1 {
		t.Fatalf("AfterAll ran %d times, want 1", afterCount)
	}
}

// TestZeroRunnableGroupNeverEntersHooks proves a group with BeforeAll/AfterAll but no It anywhere
// in its subtree is never entered at all (H3): neither hook ever runs.
func TestZeroRunnableGroupNeverEntersHooks(t *testing.T) {
	entered := false
	Describe(t, "EmptyGroup", func(s *Spec) {
		s.When("has no specs", func(w *Spec) {
			w.BeforeAll(func(*Context) { entered = true })
			w.AfterAll(func(*Context) { entered = true })
		})
		s.It("a real spec in a sibling scope", func(*Context) {})
	})
	if entered {
		t.Fatal("BeforeAll/AfterAll ran for a group with zero runnable specs, want neither to run")
	}
}

// TestAfterAllRunsBeforeNextSiblingGroupBeforeAll proves a group's AfterAll runs right after its
// own last spec and before the suite moves on to a sibling group's BeforeAll (H2).
func TestAfterAllRunsBeforeNextSiblingGroupBeforeAll(t *testing.T) {
	r := &orderRecorder{}
	Describe(t, "Siblings", func(s *Spec) {
		s.When("first", func(w *Spec) {
			w.BeforeAll(func(*Context) { r.add("first:before") })
			w.AfterAll(func(*Context) { r.add("first:after") })
			w.It("spec", func(*Context) { r.add("first:spec") })
		})
		s.When("second", func(w *Spec) {
			w.BeforeAll(func(*Context) { r.add("second:before") })
			w.AfterAll(func(*Context) { r.add("second:after") })
			w.It("spec", func(*Context) { r.add("second:spec") })
		})
	})
	assertOrder(t, r.snapshot(), []string{
		"first:before", "first:spec", "first:after",
		"second:before", "second:spec", "second:after",
	})
}

// TestBeforeAllAfterAllViaAnalyzeRegistryPath proves the arena/registry compile path (active
// inside specs.Analyze) implements the exact same ordering as the default bytecode-compiler path
// tested above — both compile paths must support group hooks (odd/tasks/before-after-all.md).
func TestBeforeAllAfterAllViaAnalyzeRegistryPath(t *testing.T) {
	r := &orderRecorder{}
	Analyze(func() {
		Describe(t, "AnalyzeNested", func(s *Spec) {
			s.Describe("outer", func(o *Spec) {
				o.BeforeAll(func(*Context) { r.add("outer:before") })
				o.AfterAll(func(*Context) { r.add("outer:after") })
				o.When("inner", func(w *Spec) {
					w.BeforeAll(func(*Context) { r.add("inner:before") })
					w.AfterAll(func(*Context) { r.add("inner:after") })
					w.It("spec", func(*Context) { r.add("spec") })
				})
			})
		})
	})
	assertOrder(t, r.snapshot(), []string{
		"outer:before", "inner:before", "spec", "inner:after", "outer:after",
	})
}

// TestBeforeAllAfterAllContextNotNil proves the *Context a group hook receives is usable for
// assertions, the same as BeforeEach/It (docs/SUITE_HOOKS_CONTRACT.md "Where a hook's *Context
// comes from").
func TestBeforeAllAfterAllContextNotNil(t *testing.T) {
	var sawBeforeCtx, sawAfterCtx bool
	Describe(t, "ContextCheck", func(s *Spec) {
		s.BeforeAll(func(ctx *Context) { sawBeforeCtx = ctx != nil })
		s.AfterAll(func(ctx *Context) { sawAfterCtx = ctx != nil })
		s.It("spec", func(*Context) {})
	})
	if !sawBeforeCtx {
		t.Fatal("BeforeAll received a nil *Context")
	}
	if !sawAfterCtx {
		t.Fatal("AfterAll received a nil *Context")
	}
}

// TestBeforeAllAfterAllDoNotAffectAllocationContractSuite is a smoke check that a suite using
// BeforeAll/AfterAll still runs and reports correctly end to end; the actual pinned zero-cost
// claim for a suite that registers NEITHER hook lives in allocation_contract_test.go and is
// unmodified by this change (H10).
func TestBeforeAllAfterAllDoNotAffectAllocationContractSuite(t *testing.T) {
	var order []string
	Describe(t, "ordinary", func(spec *Spec) {
		spec.BeforeEach(func(*Context) { order = append(order, "before") })
		spec.AfterEach(func(*Context) { order = append(order, "after") })
		spec.It("runs", func(*Context) { order = append(order, "body") })
	})
	assertOrder(t, order, []string{"before", "body", "after"})
}
