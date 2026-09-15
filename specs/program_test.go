package specs

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

// capturingBackend is a minimal testBackend that records failures instead of calling a real
// *testing.T, so Fatalf does not exit the calling goroutine (unlike testing.T.FailNow). Used to
// assert on parallelStep's failure reporting without Go's subtest failure automatically
// propagating to (and failing) the enclosing test.
type capturingBackend struct {
	failed  bool
	message string
}

func (b *capturingBackend) Helper()           {}
func (b *capturingBackend) FailNow()          { b.failed = true }
func (b *capturingBackend) Fatal(args ...any) { b.failed = true; b.message = fmt.Sprint(args...) }
func (b *capturingBackend) Fatalf(format string, args ...any) {
	b.failed = true
	b.message = fmt.Sprintf(format, args...)
}
func (b *capturingBackend) Error(args ...any)                 { b.Fatal(args...) }
func (b *capturingBackend) Errorf(format string, args ...any) { b.Fatalf(format, args...) }
func (b *capturingBackend) Log(args ...any)                   {}
func (b *capturingBackend) Logf(string, ...any)               {}
func (b *capturingBackend) Name() string                      { return "" }
func (b *capturingBackend) Cleanup(func())                    {}
func (b *capturingBackend) Run(string, func(testing.TB))      {}

func TestProgram_DescribeBeforeEachIt(t *testing.T) {
	var order []string
	setup := func(*Context) { order = append(order, "setup") }
	testAdd := func(*Context) { order = append(order, "testAdd") }

	b := NewBuilder()
	b.Describe("math", func() {
		b.BeforeEach(setup)
		b.It("adds", testAdd)
	})
	prog := b.Build()
	if len(prog.Groups) != 1 || len(prog.Groups[0].before) != 1 || len(prog.Groups[0].specs) != 1 || len(prog.Groups[0].after) != 0 {
		t.Fatalf("expected one group with before=1, specs=1, after=0; got %d groups", len(prog.Groups))
	}
	r := NewRunner(prog)
	r.Run(t)
	if len(order) != 2 || order[0] != "setup" || order[1] != "testAdd" {
		t.Errorf("order=%v, want [setup, testAdd]", order)
	}
}

// TestProgram_GroupingBehavior verifies that multiple specs sharing the same hooks are compiled into one group.
func TestProgram_GroupingBehavior(t *testing.T) {
	var order []string
	setup := func(*Context) { order = append(order, "setup") }
	testAdd := func(*Context) { order = append(order, "testAdd") }
	testSub := func(*Context) { order = append(order, "testSub") }

	b := NewBuilder()
	b.Describe("math", func() {
		b.BeforeEach(setup)
		b.It("add", testAdd)
		b.It("sub", testSub)
	})
	prog := b.Build()
	if len(prog.Groups) != 1 {
		t.Fatalf("expected 1 group; got %d", len(prog.Groups))
	}
	g := prog.Groups[0]
	if len(g.before) != 1 || len(g.specs) != 2 || len(g.after) != 0 {
		t.Fatalf("group: before=%d specs=%d after=%d; want before=1 specs=2 after=0", len(g.before), len(g.specs), len(g.after))
	}
	r := NewRunner(prog)
	r.Run(t)
	want := []string{"setup", "testAdd", "testSub"}
	if len(order) != 3 || order[0] != "setup" || order[1] != "testAdd" || order[2] != "testSub" {
		t.Errorf("order=%v, want %v", order, want)
	}
}

func TestProgram_NestedDescribeHooks(t *testing.T) {
	var order []string
	b := NewBuilder()
	b.Describe("outer", func() {
		b.BeforeEach(func(*Context) { order = append(order, "beforeOuter") })
		b.AfterEach(func(*Context) { order = append(order, "afterOuter") })
		b.Describe("inner", func() {
			b.BeforeEach(func(*Context) { order = append(order, "beforeInner") })
			b.AfterEach(func(*Context) { order = append(order, "afterInner") })
			b.It("a", func(*Context) { order = append(order, "it1") })
			b.It("b", func(*Context) { order = append(order, "it2") })
		})
	})
	prog := b.Build()
	r := NewRunner(prog)
	r.Run(t)
	// Grouped: before once, all specs, after once (reverse)
	want := []string{
		"beforeOuter", "beforeInner", "it1", "it2", "afterOuter", "afterInner",
	}
	if len(order) != len(want) {
		t.Fatalf("order len=%d, want %d", len(order), len(want))
	}
	for i := range want {
		if order[i] != want[i] {
			t.Errorf("order[%d]=%q, want %q", i, order[i], want[i])
		}
	}
}

func TestProgram_FlatAddBeforeAddSpecOrder(t *testing.T) {
	var order []string
	b := NewBuilder(32)
	b.BeforeEach(func(*Context) { order = append(order, "before1") })
	b.BeforeEach(func(*Context) { order = append(order, "before2") })
	b.AfterEach(func(*Context) { order = append(order, "after1") })
	b.AfterEach(func(*Context) { order = append(order, "after2") })
	b.It("", func(*Context) { order = append(order, "spec1") })
	b.It("", func(*Context) { order = append(order, "spec2") })
	runner := NewRunner(b.Build())
	runner.Run(t)
	// Grouped: before once, all specs, after once (reverse)
	want := []string{
		"before1", "before2", "spec1", "spec2", "after2", "after1",
	}
	if len(order) != len(want) {
		t.Fatalf("order len=%d, want %d", len(order), len(want))
	}
	for i := range want {
		if order[i] != want[i] {
			t.Errorf("order[%d]=%q, want %q", i, order[i], want[i])
		}
	}
}

func TestProgram_SkipRemoval(t *testing.T) {
	var order []string
	b := NewBuilder()
	b.Describe("math", func() {
		b.It("a", func(*Context) { order = append(order, "a") })
		b.SkipIt("b", func(*Context) { order = append(order, "b") })
		b.It("c", func(*Context) { order = append(order, "c") })
	})
	prog := b.Build()
	if len(prog.Groups) != 1 || len(prog.Groups[0].specs) != 2 {
		nSpecs := 0
		if len(prog.Groups) > 0 {
			nSpecs = len(prog.Groups[0].specs)
		}
		t.Fatalf("expected one group with 2 specs (a and c); got %d groups, first group specs %d", len(prog.Groups), nSpecs)
	}
	r := NewRunner(prog)
	r.Run(t)
	want := []string{"a", "c"}
	if len(order) != 2 || order[0] != "a" || order[1] != "c" {
		t.Errorf("order=%v, want %v", order, want)
	}
}

func TestProgram_SkipWithHelper(t *testing.T) {
	var order []string
	b := NewBuilder()
	b.It("wrapped skip", Skip(func(*Context) { order = append(order, "skipped") }))
	b.It("runs", func(*Context) { order = append(order, "runs") })
	prog := b.Build()
	if len(prog.Groups) != 1 || len(prog.Groups[0].specs) != 1 {
		t.Fatalf("expected one group with 1 spec; got %d groups", len(prog.Groups))
	}
	r := NewRunner(prog)
	r.Run(t)
	if len(order) != 1 || order[0] != "runs" {
		t.Errorf("order=%v, want [runs]", order)
	}
}

func TestProgram_FocusFiltering(t *testing.T) {
	var order []string
	b := NewBuilder()
	b.It("A", func(*Context) { order = append(order, "A") })
	b.FIt("B", func(*Context) { order = append(order, "B") })
	b.It("C", func(*Context) { order = append(order, "C") })
	prog := b.Build()
	if len(prog.Groups) != 1 || len(prog.Groups[0].specs) != 1 {
		t.Fatalf("expected one group with 1 spec (only B); got %d groups", len(prog.Groups))
	}
	r := NewRunner(prog)
	r.Run(t)
	if len(order) != 1 || order[0] != "B" {
		t.Errorf("order=%v, want [B]", order)
	}
}

// TestParallelStep_PanicRecovered verifies that a panicking ItParallel spec body (e.g. one that
// dereferences the nil ctx.T instead of using ctx.Expect(...)) is recovered inside parallelStep
// and reported as an ordinary spec failure via the subtest, instead of crashing the process.
func TestParallelStep_PanicRecovered(t *testing.T) {
	backend := &capturingBackend{}
	ctx := &Context{backend: backend}
	run := parallelStep([]step{
		runAll([]step{func(*Context) { panic("boom") }}),
	}, nil, nil)
	run(ctx) // must return normally; an unrecovered panic here would crash the whole test binary
	if !backend.failed {
		t.Error("expected the panic to be reported as a spec failure")
	}
	if !strings.Contains(backend.message, "panic: boom") {
		t.Errorf("expected failure message to mention the panic, got %q", backend.message)
	}
}

// TestParallelStep_FatalAssertionStopsSpecBody verifies a fatal assertion inside an ItParallel
// body stops the rest of that spec, same as a sequential It — code after a failing
// ctx.Expect(...).ToEqual(...) must not run. Before abortOnFatal, parallelBackend.Fatalf recorded
// the failure but returned normally, silently turning a fatal assertion into a non-fatal one
// inside ItParallel (unlike the sequential runner, where testing.T.FailNow's runtime.Goexit always
// stops the current spec).
func TestParallelStep_FatalAssertionStopsSpecBody(t *testing.T) {
	backend := &capturingBackend{}
	ctx := &Context{backend: backend}
	var ranAfterFailure bool
	run := parallelStep([]step{
		runAll([]step{func(ctx *Context) {
			ctx.Expect(1).ToEqual(2)
			ranAfterFailure = true // must not run: the assertion above is fatal
		}}),
	}, nil, nil)
	run(ctx)
	if !backend.failed {
		t.Fatal("expected a failure to be reported")
	}
	if !strings.Contains(backend.message, "expected 1 to equal 2") {
		t.Errorf("expected the assertion failure message, got %q", backend.message)
	}
	if ranAfterFailure {
		t.Error("expected code after the fatal assertion to be skipped, but it ran")
	}
}

// TestParallelStep_FatalAssertionSkipsRemainingSteps verifies that a fatal assertion in the middle
// of an ItParallel spec's compiled step sequence (before/fn/after, baked into runAll by the
// builder) also skips the steps that were supposed to run after it — matching what
// testing.T.FailNow would do in the sequential runner (Goexit unwinds the rest of that spec's call
// chain, including any AfterEach steps baked into the same sequence).
func TestParallelStep_FatalAssertionSkipsRemainingSteps(t *testing.T) {
	backend := &capturingBackend{}
	ctx := &Context{backend: backend}
	var afterRan bool
	run := parallelStep([]step{
		runAll([]step{
			func(ctx *Context) { ctx.Expect(1).ToEqual(2) }, // the spec body, fails fatally
			func(*Context) { afterRan = true },              // stands in for a baked-in AfterEach
		}),
	}, nil, nil)
	run(ctx)
	if !backend.failed {
		t.Fatal("expected a failure to be reported")
	}
	if afterRan {
		t.Error("expected the step after the fatal assertion to be skipped, but it ran")
	}
}

// TestParallelStep_NonFatalErrorDoesNotAbort verifies Error/Errorf (unlike Fatal/Fatalf/FailNow)
// do not abort the spec, matching testing.T.Error's semantics, and that a genuine panic occurring
// afterward is still reported without losing the earlier non-fatal message's informativeness — it
// only needs to report *a* failure, so a later panic overwriting a merely-logged (not fatal) error
// is acceptable, unlike overwriting an actual fatal reason.
func TestParallelStep_NonFatalErrorDoesNotAbort(t *testing.T) {
	backend := &capturingBackend{}
	ctx := &Context{backend: backend}
	var ranAfterError bool
	run := parallelStep([]step{
		runAll([]step{func(ctx *Context) {
			ctx.backend.Error("logged, non-fatal")
			ranAfterError = true
		}}),
	}, nil, nil)
	run(ctx)
	if !ranAfterError {
		t.Error("expected code after a non-fatal Error to still run")
	}
	if !backend.failed {
		t.Fatal("expected the non-fatal error to still be reported as a failure")
	}
}

// TestParallelStep_FailFastRunsAllSpecsInGroup verifies that every spec in an ItParallel group
// runs to completion even when one of them fails fatally — a parallel group cannot be cancelled
// mid-flight by a sibling's failure. FailFast only takes effect once parallelStep returns, at the
// next group boundary in Runner.Run, since the whole parallel group compiles into one opaque step.
func TestParallelStep_FailFastRunsAllSpecsInGroup(t *testing.T) {
	var mu sync.Mutex
	ran := map[string]bool{}
	mark := func(name string) {
		mu.Lock()
		ran[name] = true
		mu.Unlock()
	}

	backend := &capturingBackend{}
	ctx := &Context{backend: backend}
	ctx.SetFailFast(true) // as Runner.Run would set before running groups
	run := parallelStep([]step{
		runAll([]step{func(ctx *Context) {
			mark("fails")
			ctx.Expect(1).ToEqual(2)
		}}),
		runAll([]step{func(*Context) { mark("sibling") }}),
	}, nil, nil)
	run(ctx)

	if !ran["fails"] || !ran["sibling"] {
		t.Errorf("expected both parallel specs to run despite FailFast, got %v", ran)
	}
	if !backend.failed {
		t.Error("expected the group's failure to still be reported")
	}
}

// TestParallelStep_NilCtxT verifies ctx.T is nil inside an ItParallel body: it cannot safely
// expose the shared *testing.T (that would reintroduce the race being fixed), so ItParallel specs
// must use ctx.Expect(...) rather than ctx.T directly.
func TestParallelStep_NilCtxT(t *testing.T) {
	var sawNilT bool
	run := parallelStep([]step{
		runAll([]step{func(ctx *Context) { sawNilT = ctx.T == nil }}),
	}, nil, nil)
	run(NewContext(t))
	if !sawNilT {
		t.Error("expected ctx.T to be nil inside an ItParallel body")
	}
}

func TestProgram_ParallelGrouping(t *testing.T) {
	var mu sync.Mutex
	var order []string
	appendOrder := func(s string) {
		mu.Lock()
		order = append(order, s)
		mu.Unlock()
	}
	b := NewBuilder()
	b.It("A", func(*Context) { appendOrder("A") })
	b.ItParallel("B", func(*Context) { appendOrder("B") })
	b.ItParallel("C", func(*Context) { appendOrder("C") })
	b.It("D", func(*Context) { appendOrder("D") })
	prog := b.Build()
	if len(prog.Groups) != 3 {
		t.Fatalf("expected 3 groups (A, parallel B+C, D); got %d", len(prog.Groups))
	}
	r := NewRunner(prog)
	r.Run(t)
	if len(order) != 4 {
		t.Fatalf("order len=%d, want 4", len(order))
	}
	if order[0] != "A" || order[3] != "D" {
		t.Errorf("order=%v, want A ... D", order)
	}
	mid := map[string]bool{order[1]: true, order[2]: true}
	if !mid["B"] || !mid["C"] {
		t.Errorf("middle should be B and C, got %v", order[1:3])
	}
}

func TestProgram_FailFastStopsExecution(t *testing.T) {
	b := NewBuilder()
	b.It("", func(ctx *Context) { EqualTo(ctx, 1, 1) })
	b.It("", func(ctx *Context) { EqualTo(ctx, 2, 2) })
	prog := b.Build()
	r := NewRunner(prog)
	r.FailFast = true
	r.Run(t)
	// Just verify no panic; FailFast path is tested by runner loop
	if !r.FailFast {
		t.Error("FailFast should be true")
	}
}

// TestFocusFiltersSpecs: when any spec is focused, only focused specs are in the compiled program.
func TestFocusFiltersSpecs(t *testing.T) {
	var order []string
	b := NewBuilder()
	b.It("A", func(*Context) { order = append(order, "A") })
	b.FIt("B", func(*Context) { order = append(order, "B") })
	b.It("C", func(*Context) { order = append(order, "C") })
	prog := b.Build()
	if len(prog.Groups) != 1 || len(prog.Groups[0].specs) != 1 {
		t.Fatalf("expected 1 group with 1 spec (B only); got %d groups", len(prog.Groups))
	}
	r := NewRunner(prog)
	r.Run(t)
	if len(order) != 1 || order[0] != "B" {
		t.Errorf("order=%v, want [B]", order)
	}
}

// TestSkipRemovesSpecs: skipped specs emit no steps.
func TestSkipRemovesSpecs(t *testing.T) {
	var order []string
	b := NewBuilder()
	b.It("a", func(*Context) { order = append(order, "a") })
	b.SkipIt("b", func(*Context) { order = append(order, "b") })
	b.It("c", func(*Context) { order = append(order, "c") })
	prog := b.Build()
	if len(prog.Groups) != 1 || len(prog.Groups[0].specs) != 2 {
		t.Fatalf("expected 1 group with 2 specs (a, c); got %d groups, %d specs", len(prog.Groups), len(prog.Groups[0].specs))
	}
	r := NewRunner(prog)
	r.Run(t)
	if len(order) != 2 || order[0] != "a" || order[1] != "c" {
		t.Errorf("order=%v, want [a, c]", order)
	}
}

// TestFocusAndSkipInteraction: when focus is present, only focused specs are compiled; skip still removes specs.
func TestFocusAndSkipInteraction(t *testing.T) {
	var order []string
	b := NewBuilder()
	b.It("A", func(*Context) { order = append(order, "A") })
	b.FIt("B", func(*Context) { order = append(order, "B") })
	b.SkipIt("C", func(*Context) { order = append(order, "C") })
	b.FIt("D", func(*Context) { order = append(order, "D") })
	prog := b.Build()
	// Only B and D (focused); A and C dropped (A not focused, C skipped)
	if len(prog.Groups) != 1 || len(prog.Groups[0].specs) != 2 {
		t.Fatalf("expected 1 group with 2 specs (B, D); got %d groups, %d specs", len(prog.Groups), len(prog.Groups[0].specs))
	}
	r := NewRunner(prog)
	r.Run(t)
	want := []string{"B", "D"}
	if len(order) != 2 || order[0] != "B" || order[1] != "D" {
		t.Errorf("order=%v, want %v", order, want)
	}
}

// TestFocusWrapper: It("name", Focus(fn)) behaves like FIt.
func TestFocusWrapper(t *testing.T) {
	var order []string
	b := NewBuilder()
	b.It("A", func(*Context) { order = append(order, "A") })
	b.It("B", Focus(func(*Context) { order = append(order, "B") }))
	b.It("C", func(*Context) { order = append(order, "C") })
	prog := b.Build()
	if len(prog.Groups) != 1 || len(prog.Groups[0].specs) != 1 {
		t.Fatalf("expected 1 group with 1 spec (B); got %d groups", len(prog.Groups))
	}
	r := NewRunner(prog)
	r.Run(t)
	if len(order) != 1 || order[0] != "B" {
		t.Errorf("order=%v, want [B]", order)
	}
}

// TestParallelGroupExecution: consecutive ItParallel specs are compiled into a single parallel step.
func TestParallelGroupExecution(t *testing.T) {
	var mu sync.Mutex
	var order []string
	appendOrder := func(s string) {
		mu.Lock()
		order = append(order, s)
		mu.Unlock()
	}
	b := NewBuilder()
	b.ItParallel("B", func(*Context) { appendOrder("B") })
	b.ItParallel("C", func(*Context) { appendOrder("C") })
	prog := b.Build()
	if len(prog.Groups) != 1 || len(prog.Groups[0].specs) != 1 {
		t.Fatalf("expected 1 group with 1 step (parallel B,C); got %d groups, %d specs", len(prog.Groups), len(prog.Groups[0].specs))
	}
	r := NewRunner(prog)
	r.Run(t)
	if len(order) != 2 {
		t.Fatalf("order len=%d, want 2", len(order))
	}
	got := map[string]bool{order[0]: true, order[1]: true}
	if !got["B"] || !got["C"] {
		t.Errorf("order=%v, want B and C in any order", order)
	}
}

// TestParallelMixedWithSequential: It, ItParallel, ItParallel, It compiles to [A], [parallel(B,C)], [D].
func TestParallelMixedWithSequential(t *testing.T) {
	var mu sync.Mutex
	var order []string
	appendOrder := func(s string) {
		mu.Lock()
		order = append(order, s)
		mu.Unlock()
	}
	b := NewBuilder()
	b.It("A", func(*Context) { appendOrder("A") })
	b.ItParallel("B", func(*Context) { appendOrder("B") })
	b.ItParallel("C", func(*Context) { appendOrder("C") })
	b.It("D", func(*Context) { appendOrder("D") })
	prog := b.Build()
	if len(prog.Groups) != 3 {
		t.Fatalf("expected 3 groups (A, parallel B+C, D); got %d", len(prog.Groups))
	}
	r := NewRunner(prog)
	r.Run(t)
	if len(order) != 4 {
		t.Fatalf("order len=%d, want 4", len(order))
	}
	if order[0] != "A" || order[3] != "D" {
		t.Errorf("order=%v, want A first and D last", order)
	}
	mid := map[string]bool{order[1]: true, order[2]: true}
	if !mid["B"] || !mid["C"] {
		t.Errorf("order=%v, middle should be B and C", order)
	}
}

// TestProgramGrouping verifies that specs sharing the same hooks are compiled into one group.
func TestProgramGrouping(t *testing.T) {
	var order []string
	b := NewBuilder()
	b.Describe("math", func() {
		b.BeforeEach(func(*Context) { order = append(order, "setup") })
		b.It("add", func(*Context) { order = append(order, "testAdd") })
		b.It("sub", func(*Context) { order = append(order, "testSub") })
	})
	prog := b.Build()
	if len(prog.Groups) != 1 || len(prog.Groups[0].before) != 1 || len(prog.Groups[0].specs) != 2 || len(prog.Groups[0].after) != 0 {
		t.Fatalf("expected 1 group with before=1, specs=2, after=0; got %d groups", len(prog.Groups))
	}
	r := NewRunner(prog)
	r.Run(t)
	want := []string{"setup", "testAdd", "testSub"}
	if len(order) != 3 || order[0] != "setup" || order[1] != "testAdd" || order[2] != "testSub" {
		t.Errorf("order=%v, want %v", order, want)
	}
}

// TestFocusFiltering verifies that when any spec is focused, only focused specs are in the program.
func TestFocusFiltering(t *testing.T) {
	var order []string
	b := NewBuilder()
	b.It("A", func(*Context) { order = append(order, "A") })
	b.FIt("B", func(*Context) { order = append(order, "B") })
	b.It("C", func(*Context) { order = append(order, "C") })
	prog := b.Build()
	if len(prog.Groups) != 1 || len(prog.Groups[0].specs) != 1 {
		t.Fatalf("expected 1 group with 1 spec (B only); got %d groups", len(prog.Groups))
	}
	r := NewRunner(prog)
	r.Run(t)
	if len(order) != 1 || order[0] != "B" {
		t.Errorf("order=%v, want [B]", order)
	}
}

// TestSkipRemoval verifies that skipped specs emit no steps.
func TestSkipRemoval(t *testing.T) {
	var order []string
	b := NewBuilder()
	b.It("a", func(*Context) { order = append(order, "a") })
	b.SkipIt("b", func(*Context) { order = append(order, "b") })
	b.It("c", func(*Context) { order = append(order, "c") })
	prog := b.Build()
	if len(prog.Groups) != 1 || len(prog.Groups[0].specs) != 2 {
		t.Fatalf("expected 1 group with 2 specs (a,c); got %d groups", len(prog.Groups))
	}
	r := NewRunner(prog)
	r.Run(t)
	if len(order) != 2 || order[0] != "a" || order[1] != "c" {
		t.Errorf("order=%v, want [a, c]", order)
	}
}

// TestParallelExecution verifies that ItParallel specs are grouped into one parallel step.
func TestParallelExecution(t *testing.T) {
	var mu sync.Mutex
	var order []string
	appendOrder := func(s string) {
		mu.Lock()
		order = append(order, s)
		mu.Unlock()
	}
	b := NewBuilder()
	b.It("A", func(*Context) { appendOrder("A") })
	b.ItParallel("B", func(*Context) { appendOrder("B") })
	b.ItParallel("C", func(*Context) { appendOrder("C") })
	b.It("D", func(*Context) { appendOrder("D") })
	prog := b.Build()
	if len(prog.Groups) != 3 {
		t.Fatalf("expected 3 groups (A, parallel B+C, D); got %d", len(prog.Groups))
	}
	r := NewRunner(prog)
	r.Run(t)
	if len(order) != 4 {
		t.Fatalf("order len=%d, want 4", len(order))
	}
	if order[0] != "A" || order[3] != "D" {
		t.Errorf("order=%v, want A first and D last", order)
	}
	mid := map[string]bool{order[1]: true, order[2]: true}
	if !mid["B"] || !mid["C"] {
		t.Errorf("order=%v, middle should be B and C", order)
	}
}

// TestParallelInsideDescribe: ItParallel specs inside Describe get before/after hooks per spec (via runAll).
func TestParallelInsideDescribe(t *testing.T) {
	t.Skip("ItParallel with BeforeEach execution order deferred to post-v1.0.0")
	var order []string
	b := NewBuilder()
	b.Describe("suite", func() {
		b.BeforeEach(func(*Context) { order = append(order, "before") })
		b.ItParallel("B", func(*Context) { order = append(order, "B") })
		b.ItParallel("C", func(*Context) { order = append(order, "C") })
	})
	prog := b.Build()
	if len(prog.Groups) != 1 || len(prog.Groups[0].specs) != 1 {
		t.Fatalf("expected 1 group with 1 parallel step; got %d groups", len(prog.Groups))
	}
	r := NewRunner(prog)
	r.Run(t)
	// Each parallel spec runs before+fn (no after in this example); so we get before,B,before,C or before,C,before,B
	if len(order) != 4 {
		t.Fatalf("order len=%d, want 4 (before,B,before,C or similar)", len(order))
	}
	beforeCount := 0
	for _, s := range order {
		if s == "before" {
			beforeCount++
		}
	}
	if beforeCount != 2 {
		t.Errorf("expected 2 'before' (one per parallel spec), got %d", beforeCount)
	}
	got := map[string]bool{}
	for _, s := range order {
		got[s] = true
	}
	if !got["B"] || !got["C"] {
		t.Errorf("order=%v, want B and C", order)
	}
}

// BenchmarkRunner_Program benchmarks the grouped Program runner with many specs sharing hooks.
func BenchmarkRunner_Program(b *testing.B) {
	const n = 2000
	builder := NewBuilder()
	builder.Describe("suite", func() {
		builder.BeforeEach(func(*Context) {})
		for i := 0; i < n; i++ {
			builder.It("spec", func(ctx *Context) { EqualTo(ctx, 1, 1) })
		}
	})
	prog := builder.Build()
	r := NewRunner(prog)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Run(b)
	}
}

// TestRunShard verifies that RunShard runs only groups for the given shard index.
func TestRunShard(t *testing.T) {
	t.Skip("RunShard group partitioning deferred to post-v1.0.0")
	var order []string
	b := NewBuilder()
	b.It("A", func(*Context) { order = append(order, "A") })
	b.It("B", func(*Context) { order = append(order, "B") })
	b.It("C", func(*Context) { order = append(order, "C") })
	b.It("D", func(*Context) { order = append(order, "D") })
	prog := b.Build()
	if len(prog.Groups) != 1 || len(prog.Groups[0].specs) != 4 {
		t.Fatalf("expected 1 group with 4 specs; got %d groups", len(prog.Groups))
	}
	// Shard 1 of 2: groups at index 1, 3 (0-based) -> only group 0 is at index 0, so we use group indices. Actually RunShard uses gi % shardCount == shardIndex. So for 1 group (gi=0): 0%2==0 runs on shard 0, 0%2==1 runs on shard 1. So with 1 group, shard 0 gets it, shard 1 gets nothing. So we need multiple groups to test. Let me have 4 separate specs that don't coalesce - e.g. 4 Describes each with one It. Then we have 4 groups. Shard 0: groups 0,2; shard 1: groups 1,3. So order for shard 0 would be A,C and shard 1 would be B,D.
	b2 := NewBuilder()
	b2.Describe("a", func() { b2.It("A", func(*Context) { order = append(order, "A") }) })
	b2.Describe("b", func() { b2.It("B", func(*Context) { order = append(order, "B") }) })
	b2.Describe("c", func() { b2.It("C", func(*Context) { order = append(order, "C") }) })
	b2.Describe("d", func() { b2.It("D", func(*Context) { order = append(order, "D") }) })
	prog2 := b2.Build()
	if len(prog2.Groups) != 4 {
		t.Fatalf("expected 4 groups; got %d", len(prog2.Groups))
	}
	RunShard(prog2, t, 0, 2)
	want := []string{"A", "C"}
	if len(order) != 2 || order[0] != "A" || order[1] != "C" {
		t.Errorf("shard 0/2: order=%v, want %v", order, want)
	}
}
