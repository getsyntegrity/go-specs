// spec_itparallel_hooks_test.go pins issue #245's T3: a BeforeAll/AfterAll group (H9,
// docs/SUITE_HOOKS_CONTRACT.md) that wraps ItParallel specs runs its BeforeAll once, before any of
// them starts, and its AfterAll once, only after every one of them has finished — including a
// parallel one, exactly as H9 requires.
package specs

import (
	"sync"
	"sync/atomic"
	"testing"
)

// TestSpecItParallel_BeforeAllCompletesBeforeAnyParallelSpecStarts pins H9's first half: BeforeAll
// finishes before any spec of the group starts, including a parallel one. If a parallel spec could
// start before BeforeAll finished, at least one of the concurrently launched goroutines below would
// observe beforeAllDone still false.
func TestSpecItParallel_BeforeAllCompletesBeforeAnyParallelSpecStarts(t *testing.T) {
	var beforeAllDone atomic.Bool
	var sawIncomplete atomic.Bool
	var afterAllCount atomic.Int32

	BuildSuite(nil, "suite", func(s *Spec) {
		s.BeforeAll(func(*Context) { beforeAllDone.Store(true) })
		s.AfterAll(func(*Context) { afterAllCount.Add(1) })
		for i := 0; i < 4; i++ {
			s.ItParallel("spec", func(*Context) {
				if !beforeAllDone.Load() {
					sawIncomplete.Store(true)
				}
			})
		}
	}).Run(t)

	if sawIncomplete.Load() {
		t.Error("a parallel spec started before the group's BeforeAll finished")
	}
	if !beforeAllDone.Load() {
		t.Error("BeforeAll never ran")
	}
	if got := afterAllCount.Load(); got != 1 {
		t.Errorf("AfterAll ran %d times, want exactly 1", got)
	}
}

// TestSpecItParallel_AfterAllRunsOnlyAfterEveryParallelSpecFinishes pins H9's second half: AfterAll
// starts only after every spec of the group — including every parallel one — has finished (i.e.
// after the parallel batch has been waited on). Each parallel spec increments a counter right before
// returning; AfterAll must see the full count.
func TestSpecItParallel_AfterAllRunsOnlyAfterEveryParallelSpecFinishes(t *testing.T) {
	const n = 5
	var finished atomic.Int32
	var seenAtAfterAll int32
	var mu sync.Mutex
	var afterAllRan bool

	BuildSuite(nil, "suite", func(s *Spec) {
		s.AfterAll(func(*Context) {
			mu.Lock()
			seenAtAfterAll = finished.Load()
			afterAllRan = true
			mu.Unlock()
		})
		for i := 0; i < n; i++ {
			s.ItParallel("spec", func(*Context) {
				finished.Add(1)
			})
		}
	}).Run(t)

	if !afterAllRan {
		t.Fatal("AfterAll never ran")
	}
	if seenAtAfterAll != n {
		t.Errorf("AfterAll observed %d finished specs, want all %d", seenAtAfterAll, n)
	}
}

// TestSpecItParallel_NestedGroupWithHooksAndParallelSiblings exercises BeforeAll/AfterAll wrapping
// ItParallel inside a nested When, alongside a plain sequential sibling It outside the group, so the
// group boundary itself (not just the parallel range) is proven correct end to end.
func TestSpecItParallel_NestedGroupWithHooksAndParallelSiblings(t *testing.T) {
	var order []string
	var mu sync.Mutex
	record := func(s string) {
		mu.Lock()
		order = append(order, s)
		mu.Unlock()
	}

	BuildSuite(nil, "suite", func(s *Spec) {
		s.It("before", func(*Context) { record("before") })
		s.When("group", func(w *Spec) {
			w.BeforeAll(func(*Context) { record("BeforeAll") })
			w.AfterAll(func(*Context) { record("AfterAll") })
			w.ItParallel("a", func(*Context) { record("a") })
			w.ItParallel("b", func(*Context) { record("b") })
		})
		s.It("after", func(*Context) { record("after") })
	}).Run(t)

	if len(order) != 6 {
		t.Fatalf("order = %v, want 6 entries", order)
	}
	if order[0] != "before" {
		t.Errorf("order[0] = %q, want %q", order[0], "before")
	}
	if order[1] != "BeforeAll" {
		t.Errorf("order[1] = %q, want %q", order[1], "BeforeAll")
	}
	if order[5] != "after" {
		t.Errorf("order[5] = %q, want %q", order[5], "after")
	}
	// order[2] and order[3] are "AfterAll" then whichever of a/b finished last, or a/b in either
	// order followed by "AfterAll" — the only hard constraint is AfterAll strictly after both a and
	// b, which the count-based check below verifies regardless of interleaving.
	afterAllIdx, aIdx, bIdx := -1, -1, -1
	for i, e := range order {
		switch e {
		case "AfterAll":
			afterAllIdx = i
		case "a":
			aIdx = i
		case "b":
			bIdx = i
		}
	}
	if afterAllIdx < aIdx || afterAllIdx < bIdx {
		t.Errorf("AfterAll (index %d) did not run after both parallel specs (a=%d, b=%d): %v", afterAllIdx, aIdx, bIdx, order)
	}
}
