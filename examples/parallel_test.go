// parallel_test.go shows concurrency inside a suite: Spec.ItParallel and ctx.Go.
//
//   - ItParallel runs adjacent independent specs at the same time, each as its own Go subtest with
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
	"time"

	"github.com/getsyntegrity/go-specs/specs"
)

// The two specs below wait for each other. That is only possible if they really run at the same
// time; run sequentially, the first one would time out. This makes the concurrency observable
// without sleeping or depending on the scheduler.
func TestParallel_itParallelRunsSiblingsConcurrently(t *testing.T) {
	var barrier sync.WaitGroup
	barrier.Add(2)

	meet := func(ctx *specs.Context) {
		barrier.Done()
		done := make(chan struct{})
		go func() { barrier.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			ctx.Errorf("the sibling spec never started: specs are not running concurrently")
		}
	}

	specs.Describe(t, "parallel math", func(s *specs.Spec) {
		s.ItParallel("first independent spec", meet)
		s.ItParallel("second independent spec", meet)
	})
}

// Hooks still wrap each parallel spec, and every spec gets its own Context, so shared counters
// need their own synchronization (here an atomic-free mutex).
func TestParallel_hooksWrapEachSpec(t *testing.T) {
	var mu sync.Mutex
	before := 0

	specs.Describe(t, "with hooks", func(s *specs.Spec) {
		s.BeforeEach(func(ctx *specs.Context) {
			mu.Lock()
			before++
			mu.Unlock()
		})
		s.ItParallel("one", func(ctx *specs.Context) { ctx.Expect(1 + 1).ToEqual(2) })
		s.ItParallel("two", func(ctx *specs.Context) { ctx.Expect(2 + 2).ToEqual(4) })
		s.It("counts both hooks after the batch", func(ctx *specs.Context) {
			ctx.Expect(before).ToEqual(3) // the hook ran for both parallel specs and for this one
		})
	})
}

// ctx.Go runs a function on its own goroutine as part of the current spec. The spec waits for
// every task before its AfterEach hooks run, and a failed assertion inside a task fails the spec.
// Always start goroutines that assert with ctx.Go, never with a bare `go` statement.
func TestParallel_ctxGoRunsAssertionsConcurrently(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]bool{}
	record := func(name string) {
		mu.Lock()
		seen[name] = true
		mu.Unlock()
	}

	specs.Describe(t, "services", func(s *specs.Spec) {
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

		s.It("has finished both tasks by the time the next spec runs", func(ctx *specs.Context) {
			mu.Lock()
			defer mu.Unlock()
			ctx.Expect(len(seen)).ToEqual(2)
		})
	})
}
