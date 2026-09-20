package specs

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// Issue #170: a handle returned by ctx.Expect(x) or ExpectT(ctx, x) is consumed by the first
// To/ToEqual call. Before the fix, that first call returned the object to a sync.Pool, so a second
// assertion through the same handle either silently returned (e.ctx was nil) or — once the pool had
// handed the object to another spec — reported against that other spec's backend.
//
// Both outcomes are false greens in a testing framework, which is the worst place for one. These
// regressions pin the two halves of the contract: a reused handle fails loudly, and a released
// handle is never handed to anyone else.

// TestExpectToEqualRejectsASecondAssertionThroughTheSameHandle is the reproducer from the issue.
func TestExpectToEqualRejectsASecondAssertionThroughTheSameHandle(t *testing.T) {
	ctx, b := newCapturedContext()

	e := ctx.Expect(1)
	e.ToEqual(1)

	assertPanicsWith(t, func() { e.ToEqual(999) }, "go-specs", "reused")

	if b.failed {
		t.Fatalf("the first, passing assertion must not have failed: %q", b.message)
	}
}

func TestExpectToRejectsASecondAssertionThroughTheSameHandle(t *testing.T) {
	ctx, _ := newCapturedContext()

	e := ctx.Expect(1)
	e.To(Equal(1))

	assertPanicsWith(t, func() { e.To(Equal(999)) }, "go-specs", "reused")
}

// Mixing the two assertion methods on one handle is the same misuse: the handle is spent by
// whichever call came first, not by a particular method.
func TestExpectRejectsASecondAssertionThroughADifferentMethod(t *testing.T) {
	ctx, _ := newCapturedContext()

	e := ctx.Expect(1)
	e.ToEqual(1)

	assertPanicsWith(t, func() { e.To(Equal(999)) }, "go-specs", "reused")
}

func TestExpectTToEqualRejectsASecondAssertionThroughTheSameHandle(t *testing.T) {
	ctx, b := newCapturedContext()

	x := ExpectT(ctx, 1)
	x.ToEqual(1)

	assertPanicsWith(t, func() { x.ToEqual(999) }, "go-specs", "reused")

	if b.failed {
		t.Fatalf("the first, passing assertion must not have failed: %q", b.message)
	}
}

func TestExpectTToRejectsASecondAssertionThroughTheSameHandle(t *testing.T) {
	ctx, _ := newCapturedContext()

	x := ExpectT(ctx, 1)
	x.To(Equal(1))

	assertPanicsWith(t, func() { x.To(Equal(999)) }, "go-specs", "reused")
}

// A failing first assertion still spends the handle. capturingBackend's Fatalf records rather than
// calling Goexit, so unlike a real *testing.T this reaches the second call at all — which is
// exactly why the check must not depend on the first assertion having passed.
func TestExpectRejectsASecondAssertionAfterTheFirstOneFailed(t *testing.T) {
	ctx, b := newCapturedContext()

	e := ctx.Expect(1)
	e.ToEqual(2)

	if !b.failed {
		t.Fatal("expected the first assertion to report a failure")
	}
	assertPanicsWith(t, func() { e.ToEqual(1) }, "go-specs", "reused")
}

// The other half of the contract, and the one a released flag alone would not give: a spent handle
// must never become another spec's handle. While expectations were pooled, the object behind a
// retained handle was handed to the next Expect call — so an assertion through the stale handle
// reported into whichever spec now owned it.
func TestASpentHandleIsNeverHandedToAnotherContext(t *testing.T) {
	ctxA, backendA := newCapturedContext()
	ctxB, backendB := newCapturedContext()

	stale := ctxA.Expect(1)
	stale.ToEqual(1)

	fresh := ctxB.Expect(2)
	if stale == fresh {
		t.Fatal("a spent expectation handle was handed out again to another context")
	}
	fresh.ToEqual(2)

	assertPanicsWith(t, func() { stale.ToEqual(999) }, "go-specs", "reused")

	if backendA.failed {
		t.Fatalf("reuse leaked a failure into the originating spec: %q", backendA.message)
	}
	if backendB.failed {
		t.Fatalf("reuse leaked a failure into an unrelated spec: %q", backendB.message)
	}
}

// Under concurrency the redirect was the dangerous failure mode: goroutine A retains a spent
// handle while goroutine B acquires an expectation, and A's second assertion lands on B's backend.
// Each goroutine here owns its own Context, backend and handles, so any cross-talk shows up as a
// failure recorded on a backend whose own assertions all passed.
//
// Note what this does NOT cover: no handle is shared between goroutines here, so it proves
// isolation between concurrent specs, not exclusive consumption of a single handle. That is
// TestASharedHandleIsConsumedByExactlyOneGoroutine below.
func TestConcurrentReuseCannotRedirectAnAssertionToAnotherSpec(t *testing.T) {
	const goroutines = 64
	const rounds = 200

	backends := make([]*capturingBackend, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		ctx, b := newCapturedContext()
		backends[g] = b
		go func() {
			defer wg.Done()
			for i := 0; i < rounds; i++ {
				stale := ctx.Expect(i)
				stale.ToEqual(i)
				// Reusing the spent handle must panic here, in this goroutine, and must never
				// travel to a sibling's backend.
				func() {
					defer func() {
						if recover() == nil {
							panic("expected reuse of a spent handle to panic")
						}
					}()
					stale.ToEqual(-1)
				}()
			}
		}()
	}
	wg.Wait()

	for g, b := range backends {
		if b.failed {
			t.Fatalf("goroutine %d's backend recorded a failure it never asserted: %q", g, b.message)
		}
	}
}

// Single-use has to be a real property, not merely a sequential one. A plain
// `if e.spent { panic }` followed by a deferred write is check-then-act: several goroutines racing
// on ONE fresh handle all read false before any of them sets it, so all of them assert. That is a
// second silently-passing assertion again, plus an unsynchronized read/write on spent, ctx and
// actual. Claiming the handle with CompareAndSwap is what makes exactly one caller win, whatever
// the interleaving, and the atomic also gives the losers a happens-before edge so -race has nothing
// to report.
//
// The starting gun maximises the overlap, and the rounds are what turn a probabilistic window into
// a reliable signal. Under -race a check-then-act implementation is reported directly; without it,
// the counts below still catch the double consumption.
func TestASharedHandleIsConsumedByExactlyOneGoroutine(t *testing.T) {
	const rounds = 300
	const racers = 4

	for round := 0; round < rounds; round++ {
		ctx, b := newCapturedContext()
		shared := ctx.Expect(1)

		var consumed, rejected atomic.Int32
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(racers)

		for r := 0; r < racers; r++ {
			go func() {
				defer wg.Done()
				defer func() {
					switch r := recover(); v := r.(type) {
					case nil:
						return
					case string:
						if !strings.Contains(v, "reused") {
							panic(r) // not our guard — let it surface rather than be counted
						}
						rejected.Add(1)
					default:
						panic(r)
					}
				}()
				<-start
				// Exactly one of these may run the assertion; the rest must be rejected.
				shared.ToEqual(1)
				consumed.Add(1)
			}()
		}
		close(start)
		wg.Wait()

		if got := consumed.Load(); got != 1 {
			t.Fatalf("round %d: %d goroutines asserted through one shared handle, want exactly 1", round, got)
		}
		if got := rejected.Load(); got != racers-1 {
			t.Fatalf("round %d: %d goroutines were rejected, want %d", round, got, racers-1)
		}
		// The one assertion that did run compares 1 to 1, so a recorded failure here means an
		// assertion reached the backend that never should have.
		if b.failed {
			t.Fatalf("round %d: nothing should have been reported to the backend, got %q", round, b.message)
		}
	}
}

// Normal single-use assertions must stay allocation-free on the passing path. This is the guard
// that keeps removing the expectation pool honest: the docs promise zero allocations on the
// assertion fast path, and a benchmark-only claim is not enforced by `go test ./...`. Without the
// pool the Expectation is stack-allocated instead — this test is what proves escape analysis
// actually delivers that, rather than the docs merely continuing to claim it.
//
// The matcher is built once, outside the measured function, for the same reason the matcher
// benchmark uses BeTrue(): constructing a matcher boxes it into the Matcher interface and that
// allocation belongs to the matcher, not to the assertion path under test here.
func TestPassingAssertionsAllocateNothingOnTheFastPath(t *testing.T) {
	ctx, _ := newCapturedContext()
	equals42 := Equal(42)

	cases := []struct {
		name string
		fn   func()
	}{
		{"ExpectT.ToEqual", func() { ExpectT(ctx, 42).ToEqual(42) }},
		{"ExpectT.To", func() { ExpectT(ctx, 42).To(equals42) }},
		{"Expect.ToEqual", func() { ctx.Expect(42).ToEqual(42) }},
		{"Expect.To", func() { ctx.Expect(42).To(equals42) }},
	}
	for _, c := range cases {
		if got := testing.AllocsPerRun(1000, c.fn); got != 0 {
			t.Errorf("%s allocated %v times per run on the passing path, want 0", c.name, got)
		}
	}
}

// assertPanicsWith checks that fn panics with a string message containing every wantSubstrings
// entry. Shared by the expectation-reuse regressions above and by matcher_assertion_test.go.
func assertPanicsWith(t *testing.T, fn func(), wantSubstrings ...string) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected a panic")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("expected a string panic message, got %T: %v", r, r)
		}
		for _, want := range wantSubstrings {
			if !strings.Contains(msg, want) {
				t.Fatalf("panic message %q missing expected substring %q", msg, want)
			}
		}
	}()
	fn()
}
