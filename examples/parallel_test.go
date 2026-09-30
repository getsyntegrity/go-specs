// parallel_test.go shows concurrency inside a suite: Spec.ItParallel and ctx.Go.
//
//   - ItParallel lets adjacent independent specs run at the same time, each as its own Go subtest with
//     its own Context. Use it for slow, independent specs (network-free integration work, big
//     computations) so the suite takes as long as its slowest member.
//   - ctx.Go runs assertions concurrently inside ONE spec, and the spec waits for every task
//     before it finishes. Use it to check several things that block on each other or on a
//     shared resource.
//
// Semantics worth knowing: consecutive ItParallel specs form one batch that ends at the next
// It, FIt, SkipIt, PendingIt or group boundary; BeforeEach/AfterEach still wrap every spec; under
// focus (any FIt in the same Describe call) ItParallel specs are dropped, exactly like an
// unfocused It. Tasks started with ctx.Go are never cancelled, so a task that blocks forever
// blocks the spec. See docs/DSL.md for the complete contract.
package examples_test

import (
	"sync"
	"testing"

	"github.com/getsyntegrity/go-specs/assert"
	"github.com/getsyntegrity/go-specs/specs"
)

// Each ItParallel spec below is independent: it owns its input and result and never waits for a
// sibling, so it passes the same way when selected alone with -run or when the Go test runner is
// limited with -parallel=1. ItParallel makes adjacent siblings eligible to overlap; it does not
// require them to. That they really do overlap is proved by the library's internal tests
// (specs/spec_itparallel*_test.go), not by an example.
func TestParallel_itParallelRunsIndependentSpecs(t *testing.T) {
	specs.Describe(t, "parallel math", func(s *specs.Spec) {
		s.ItParallel("adds", func(ctx *specs.Context) { ctx.Expect(1 + 1).ToEqual(2) })
		s.ItParallel("multiplies", func(ctx *specs.Context) { ctx.Expect(3 * 4).ToEqual(12) })
		s.ItParallel("concatenates", func(ctx *specs.Context) { ctx.Expect("go" + "-specs").ToEqual("go-specs") })
	})
}

// Hooks still wrap each parallel spec, and every spec gets its own Context, so shared counters
// need their own synchronization (here a mutex). No spec asserts on what a sibling did: each one
// only checks that its own BeforeEach has already run, which holds whether it runs alone or with
// others. The exact counts are checked once, in t.Cleanup, over whichever specs actually ran.
func TestParallel_hooksWrapEachSpec(t *testing.T) {
	var mu sync.Mutex
	before, after, ran := 0, 0, 0

	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		if before != ran || after != ran {
			t.Errorf("hooks must wrap every spec that ran: ran=%d BeforeEach=%d AfterEach=%d", ran, before, after)
		}
	})

	count := func(n *int) {
		mu.Lock()
		*n++
		mu.Unlock()
	}
	// started reports whether this spec's own BeforeEach has already run.
	started := func() int {
		mu.Lock()
		defer mu.Unlock()
		ran++
		return before
	}

	specs.Describe(t, "with hooks", func(s *specs.Spec) {
		s.BeforeEach(func(ctx *specs.Context) { count(&before) })
		s.AfterEach(func(ctx *specs.Context) { count(&after) })
		s.ItParallel("one", func(ctx *specs.Context) {
			ctx.Expect(started()).To(assert.BeGreaterThan(0)) // its own BeforeEach ran first
		})
		s.ItParallel("two", func(ctx *specs.Context) {
			ctx.Expect(started()).To(assert.BeGreaterThan(0))
		})
		s.It("runs after the batch", func(ctx *specs.Context) {
			ctx.Expect(started()).To(assert.BeGreaterThan(0))
		})
	})
}

// ctx.Go runs a function on its own goroutine as part of the current spec. The spec waits for
// every task before its AfterEach hooks run, and a failed assertion inside a task fails the spec.
// Always start goroutines that assert with ctx.Go, never with a bare `go` statement. The AfterEach
// below belongs to the one spec of this suite, so it observes exactly that spec's tasks: if the
// spec did not wait for them, it would see fewer than two here.
func TestParallel_ctxGoRunsAssertionsConcurrently(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]bool{}
	record := func(name string) {
		mu.Lock()
		seen[name] = true
		mu.Unlock()
	}

	specs.Describe(t, "services", func(s *specs.Spec) {
		s.AfterEach(func(ctx *specs.Context) {
			mu.Lock()
			defer mu.Unlock()
			ctx.Expect(len(seen)).ToEqual(2) // both tasks finished before the hooks ran
		})

		s.It("pings both services at once", func(ctx *specs.Context) {
			ctx.Go(func(ctx *specs.Context) {
				record("billing")
				ctx.Expect("pong").ToEqual("pong")
			})
			ctx.Go(func(ctx *specs.Context) {
				record("inventory")
				ctx.Expect("pong").ToEqual("pong")
			})
		})
	})
}
