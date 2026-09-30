package mock_test

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/getsyntegrity/go-specs/mock"
)

// fakeTB records what the controller asks of its test.
type fakeTB struct {
	mu       sync.Mutex
	helpers  int
	cleanups []func()
	errors   []string
}

func (f *fakeTB) Helper() { f.mu.Lock(); f.helpers++; f.mu.Unlock() }
func (f *fakeTB) Cleanup(fn func()) {
	f.mu.Lock()
	f.cleanups = append(f.cleanups, fn)
	f.mu.Unlock()
}
func (f *fakeTB) Errorf(format string, args ...any) {
	f.mu.Lock()
	f.errors = append(f.errors, fmt.Sprintf(format, args...))
	f.mu.Unlock()
}
func (f *fakeTB) runCleanups() {
	for i := len(f.cleanups) - 1; i >= 0; i-- {
		f.cleanups[i]()
	}
}
func (f *fakeTB) errs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.errors...)
}

func wantErrContains(t *testing.T, tb *fakeTB, parts ...string) {
	t.Helper()
	errs := tb.errs()
	if len(errs) != 1 {
		t.Fatalf("want exactly 1 error, got %d: %q", len(errs), errs)
	}
	for _, p := range parts {
		if !strings.Contains(errs[0], p) {
			t.Fatalf("error %q does not contain %q", errs[0], p)
		}
	}
}

func TestControllerExpectationMet(t *testing.T) {
	tb := &fakeTB{}
	c := mock.NewController(tb)
	c.Method("Find").Expect("u1")
	c.Method("Find").Call("u1")
	c.Verify()
	if errs := tb.errs(); len(errs) != 0 {
		t.Fatalf("unexpected errors: %q", errs)
	}
}

func TestControllerMethodSameNameSameInstance(t *testing.T) {
	c := mock.NewController(&fakeTB{})
	a1, a2 := c.Method("A"), c.Method("A")
	if a1 != a2 || a1 == c.Method("B") {
		t.Fatal("Method must return one instance per name")
	}
}

func TestControllerUnmetExpectationDefaultTimes1(t *testing.T) {
	tb := &fakeTB{}
	c := mock.NewController(tb)
	c.Method("Find").Expect("u1", mock.Any())
	c.Verify()
	wantErrContains(t, tb, "unmet expectation", `Find(equal to "u1", any value)`, "controller_test.go:", "want 1, got 0")
}

func TestControllerTimesExactAndZero(t *testing.T) {
	tb := &fakeTB{}
	c := mock.NewController(tb)
	c.Method("Save").Expect(mock.Any()).Times(3)
	c.Method("Save").Call(1)
	c.Method("Save").Call(2)
	c.Method("Save").Call(3)
	c.Method("Drop").Expect().Times(0)
	c.Verify()
	if errs := tb.errs(); len(errs) != 0 {
		t.Fatalf("unexpected errors: %q", errs)
	}

	tb2 := &fakeTB{}
	c2 := mock.NewController(tb2)
	c2.Method("Save").Expect(mock.Any()).Times(3)
	c2.Method("Save").Call(1)
	c2.Verify()
	wantErrContains(t, tb2, "want 3, got 1")
}

func TestControllerTimesNegativePanics(t *testing.T) {
	c := mock.NewController(&fakeTB{})
	defer func() {
		if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), "Times") {
			t.Fatalf("want a clear panic, got %v", r)
		}
	}()
	c.Method("X").Expect().Times(-1)
}

func TestControllerUnexpectedCallDiagnostics(t *testing.T) {
	tb := &fakeTB{}
	c := mock.NewController(tb)
	c.Method("Find").Expect("u1")
	c.Method("Find").Expect("u2", mock.Any())
	r := c.Method("Find").Call("zz")
	if r.Get(0) != nil {
		t.Fatal("unexpected call must return a zero Result")
	}
	wantErrContains(t, tb,
		`unexpected call Find("zz")`,
		`expectation 1`, `argument 0: got "zz", want equal to "u1"`,
		`expectation 2`, `got 1 argument(s), want 2`,
	)
	if tb.helpers == 0 {
		t.Fatal("Helper must be called before reporting")
	}
}

func TestControllerUnexpectedCallNoExpectations(t *testing.T) {
	tb := &fakeTB{}
	c := mock.NewController(tb)
	c.Method("Find").Call("a", 2)
	wantErrContains(t, tb, `unexpected call Find("a", 2)`, "no expectations")
}

func TestControllerAtCapacityDiagnostics(t *testing.T) {
	tb := &fakeTB{}
	c := mock.NewController(tb)
	c.Method("Find").Expect("u1")
	c.Method("Find").Call("u1")
	c.Method("Find").Call("u1")
	wantErrContains(t, tb, `unexpected call Find("u1")`, "at capacity", "want 1, got 1")
}

func TestControllerFirstMatchWithCapacityWins(t *testing.T) {
	tb := &fakeTB{}
	c := mock.NewController(tb)
	c.Method("Find").Expect(mock.Any()).Times(1)
	c.Method("Find").Expect(mock.Any()).Times(1)
	c.Method("Find").Call("a")
	c.Method("Find").Call("b")
	c.Verify()
	if errs := tb.errs(); len(errs) != 0 {
		t.Fatalf("unexpected errors: %q", errs)
	}
}

func TestControllerVerifyIdempotent(t *testing.T) {
	tb := &fakeTB{}
	c := mock.NewController(tb)
	c.Method("Find").Expect("u1")
	c.Verify()
	c.Verify()
	wantErrContains(t, tb, "unmet expectation")
}

func TestControllerRegistersVerifyOnCleanup(t *testing.T) {
	tb := &fakeTB{}
	c := mock.NewController(tb)
	if len(tb.cleanups) != 1 {
		t.Fatalf("want 1 registered cleanup, got %d", len(tb.cleanups))
	}
	c.Method("Find").Expect("u1")
	tb.runCleanups()
	wantErrContains(t, tb, "unmet expectation")
	tb.runCleanups() // Verify already ran; must not duplicate
	if n := len(tb.errs()); n != 1 {
		t.Fatalf("cleanup duplicated messages: %d", n)
	}
}

func TestControllerReset(t *testing.T) {
	tb := &fakeTB{}
	c := mock.NewController(tb)
	c.Method("Find").Expect("u1")
	c.Method("Find").Call("u1")
	c.Spy("pub").Call("x")
	c.Reset()
	if len(c.Calls()) != 0 || len(c.Method("Find").Calls()) != 0 || c.Spy("pub").CallCount() != 0 {
		t.Fatal("Reset must drop recorded calls")
	}
	c.Verify()
	if errs := tb.errs(); len(errs) != 0 {
		t.Fatalf("Reset must drop expectations, got %q", errs)
	}
	// still usable and verifiable after Reset
	c.Reset()
	c.Method("Find").Expect("u2")
	c.Verify()
	wantErrContains(t, tb, "unmet expectation")
}

func TestControllerGlobalOrderAcrossMethodsAndSpy(t *testing.T) {
	c := mock.NewController(&fakeTB{})
	c.Method("A").Call(1)
	c.Spy("S").Call("s")
	c.Method("B").Call(2)
	calls := c.Calls()
	if len(calls) != 3 {
		t.Fatalf("want 3 calls, got %d", len(calls))
	}
	want := []string{"A", "S", "B"}
	for i, rc := range calls {
		if rc.Method != want[i] || rc.Seq != uint64(i+1) {
			t.Fatalf("call %d = %+v, want %s seq %d", i, rc, want[i], i+1)
		}
	}
	if got := c.Method("A").Calls(); len(got) != 1 || got[0].Seq != 1 || got[0].Args[0] != 1 {
		t.Fatalf("Method.Calls = %+v", got)
	}
	// the spy is a regular spy too
	s1, s2 := c.Spy("S"), c.Spy("S")
	if s1 != s2 || s1.CallCount() != 1 {
		t.Fatal("Spy must be stable and keep its own calls")
	}
	// returned slices are copies
	calls[0].Args[0] = 99
	if c.Calls()[0].Args[0] != 1 {
		t.Fatal("Calls must return copies")
	}
}

func TestControllerArgumentSliceCopied(t *testing.T) {
	c := mock.NewController(&fakeTB{})
	args := []any{"a", "b"}
	c.Method("M").Call(args...)
	args[0] = "changed"
	if c.Calls()[0].Args[0] != "a" {
		t.Fatal("argument slice must be copied")
	}
}

func TestControllerInOrderSatisfied(t *testing.T) {
	tb := &fakeTB{}
	c := mock.NewController(tb)
	e1 := c.Method("Load").Expect(mock.Any())
	e2 := c.Method("Save").Expect(mock.Any())
	c.InOrder(e1, e2)
	c.Method("Load").Call(1)
	c.Method("Save").Call(2)
	c.Verify()
	if errs := tb.errs(); len(errs) != 0 {
		t.Fatalf("unexpected errors: %q", errs)
	}
}

func TestControllerInOrderViolated(t *testing.T) {
	tb := &fakeTB{}
	c := mock.NewController(tb)
	e1 := c.Method("Load").Expect(mock.Any())
	e2 := c.Method("Save").Expect(mock.Any())
	c.InOrder(e1, e2)
	c.Method("Save").Call("s")
	c.Method("Load").Call("l")
	c.Verify()
	wantErrContains(t, tb, "order violation", "Load(any value)", "before", "Save(any value)",
		`observed sequence: #1 Save("s"), #2 Load("l")`)
}

func TestResultAccessors(t *testing.T) {
	var r mock.Result
	if r.Get(0) != nil || r.Get(-1) != nil || r.Err(0) != nil {
		t.Fatal("unconfigured result must yield nil")
	}
	if mock.Value[int](r, 0) != 0 || mock.Value[*string](r, 3) != nil || mock.Value[error](r, 0) != nil {
		t.Fatal("unconfigured Value must be the zero value")
	}
}

func TestNilSafety(t *testing.T) {
	var c *mock.Controller
	if c.Method("x") != nil || c.Spy("x") != nil || c.Calls() != nil {
		t.Fatal("nil controller must return nil")
	}
	c.Verify()
	c.Reset()
	c.InOrder()
	var m *mock.Method
	if m.Expect("a") != nil || m.Calls() != nil {
		t.Fatal("nil method must be inert")
	}
	_ = m.Call("a")
	var e *mock.Expectation
	if e.Times(2) != nil {
		t.Fatal("nil expectation must be inert")
	}
}

func TestControllerConcurrentCalls(t *testing.T) {
	tb := &fakeTB{}
	c := mock.NewController(tb)
	const goroutines, per = 32, 50
	c.Method("Inc").Expect(mock.Any(), mock.Any()).Times(goroutines * per / 2)
	c.Method("Inc").Expect(mock.Any(), mock.Any()).Times(goroutines * per / 2)
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < per; i++ {
				c.Method("Inc").Call(g, i)
				c.Spy("S").Call(g)
			}
		}(g)
	}
	wg.Wait()
	c.Verify()
	if errs := tb.errs(); len(errs) != 0 {
		t.Fatalf("unexpected errors: %q", errs[:1])
	}
	calls := c.Calls()
	if len(calls) != 2*goroutines*per {
		t.Fatalf("want %d calls, got %d", 2*goroutines*per, len(calls))
	}
	for i, rc := range calls {
		if rc.Seq != uint64(i+1) {
			t.Fatalf("sequence not dense/unique at %d: %d", i, rc.Seq)
		}
	}
	if n := len(c.Method("Inc").Calls()); n != goroutines*per {
		t.Fatalf("Inc calls = %d", n)
	}
}

type user struct{ ID string }

// findMock is a hand-written adapter for a repository interface.
type findMock struct{ c *mock.Controller }

func (m findMock) Find(id string) (*user, error) {
	r := m.c.Method("Find").Call(id)
	return mock.Value[*user](r, 0), r.Err(1)
}

func TestTypedAdapterZeroResult(t *testing.T) {
	c := mock.NewController(&fakeTB{})
	c.Method("Find").Expect("u1")
	u, err := findMock{c}.Find("u1")
	if u != nil || err != nil {
		t.Fatalf("got %v, %v", u, err)
	}
}

func ExampleController() {
	tb := &fakeTB{}
	c := mock.NewController(tb)
	c.Method("Find").Expect("u1")

	repo := findMock{c}
	u, err := repo.Find("u1") // no stubbing yet: zero results
	fmt.Println(u == nil, err == nil)

	_, _ = repo.Find("u2") // no expectation matches: reported immediately
	fmt.Println(len(tb.errs()))
	fmt.Println(len(c.Method("Find").Calls()))
	// Output:
	// true true
	// 1
	// 2
}
