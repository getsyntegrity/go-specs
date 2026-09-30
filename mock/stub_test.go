package mock_test

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/mock"
)

type profile struct{ Name string }

type namer interface{ Label() string }

type label string

func (l label) Label() string { return string(l) }

var errNotFound = errors.New("not found")

func wantNoErrors(t *testing.T, tb *fakeTB) {
	t.Helper()
	if errs := tb.errs(); len(errs) != 0 {
		t.Fatalf("unexpected errors %q", errs)
	}
}

func recovered(fn func()) (msg string) {
	defer func() {
		if r := recover(); r != nil {
			msg = fmt.Sprint(r)
		}
	}()
	fn()
	return ""
}

func TestReturnSingle(t *testing.T) {
	tb := &fakeTB{}
	c := mock.NewController(tb)
	c.Method("Find").Expect("u1").Return("alice", 7)
	r := c.Method("Find").Call("u1")
	if mock.Value[string](r, 0) != "alice" || mock.Value[int](r, 1) != 7 || r.Get(2) != nil {
		t.Fatalf("unexpected result %v %v %v", r.Get(0), r.Get(1), r.Get(2))
	}
	c.Verify()
	wantNoErrors(t, tb)
}

func TestReturnEmptyIsExplicitEmptyResponse(t *testing.T) {
	c := mock.NewController(&fakeTB{})
	c.Method("Save").Expect(mock.Any()).Return().Times(2)
	r := c.Method("Save").Call(1)
	if r.Get(0) != nil || r.Err(0) != nil || mock.Value[int](r, 0) != 0 {
		t.Fatal("empty response must read as not configured")
	}
}

func TestReturnSequentialThenRepeatsLast(t *testing.T) {
	tb := &fakeTB{}
	c := mock.NewController(tb)
	c.Method("Next").Expect().Return("a").Return("b").Return("c").Times(5)
	var got []string
	for i := 0; i < 5; i++ {
		got = append(got, mock.Value[string](c.Method("Next").Call(), 0))
	}
	if strings.Join(got, "") != "abccc" {
		t.Fatalf("got %v, want a b c c c", got)
	}
	c.Verify()
	wantNoErrors(t, tb)
}

func TestReturnTimesSmallerThanSequence(t *testing.T) {
	c := mock.NewController(&fakeTB{})
	c.Method("Next").Expect().Return(1).Return(2).Times(3)
	var got []int
	for i := 0; i < 3; i++ {
		got = append(got, mock.Value[int](c.Method("Next").Call(), 0))
	}
	if fmt.Sprint(got) != "[1 2 2]" {
		t.Fatalf("got %v, want [1 2 2]", got)
	}
}

func TestReturnErrorWorksWithErrorsIs(t *testing.T) {
	c := mock.NewController(&fakeTB{})
	c.Method("Find").Expect("x").Return(nil, fmt.Errorf("wrap: %w", errNotFound))
	r := c.Method("Find").Call("x")
	if !errors.Is(r.Err(1), errNotFound) {
		t.Fatalf("Err(1) = %v, want errNotFound", r.Err(1))
	}
	if r.Err(0) != nil || mock.Value[*profile](r, 0) != nil {
		t.Fatal("nil first value must read as nil")
	}
}

func TestDoComputesFromArgs(t *testing.T) {
	c := mock.NewController(&fakeTB{})
	c.Method("Add").Expect(mock.Any(), mock.Any()).Times(2).Do(func(args []any) []any {
		return []any{args[0].(int) + args[1].(int)}
	})
	if v := mock.Value[int](c.Method("Add").Call(1, 2), 0); v != 3 {
		t.Fatalf("got %d, want 3", v)
	}
	if v := mock.Value[int](c.Method("Add").Call(10, 20), 0); v != 30 {
		t.Fatalf("got %d, want 30", v)
	}
}

func TestDoArgsAreACopy(t *testing.T) {
	c := mock.NewController(&fakeTB{})
	c.Method("M").Expect(mock.Any()).Do(func(args []any) []any {
		args[0] = "tampered"
		return nil
	})
	c.Method("M").Call("orig")
	if got := c.Method("M").Calls()[0].Args[0]; got != "orig" {
		t.Fatalf("recorded argument mutated: %v", got)
	}
}

func TestDoWinsOverReturn(t *testing.T) {
	c := mock.NewController(&fakeTB{})
	c.Method("M").Expect().Return("from-return").Do(func([]any) []any { return []any{"from-do"} })
	if v := mock.Value[string](c.Method("M").Call(), 0); v != "from-do" {
		t.Fatalf("got %q, want from-do", v)
	}
}

func TestDoRunsOutsideTheLock(t *testing.T) {
	tb := &fakeTB{}
	c := mock.NewController(tb)
	c.Method("Inner").Expect(mock.Any()).Return("inner")
	c.Method("Outer").Expect().Do(func([]any) []any {
		return []any{mock.Value[string](c.Method("Inner").Call(1), 0) + "+outer"}
	})
	done := make(chan string, 1)
	go func() { done <- mock.Value[string](c.Method("Outer").Call(), 0) }()
	select {
	case got := <-done:
		if got != "inner+outer" {
			t.Fatalf("got %q", got)
		}
	case <-timeoutC():
		t.Fatal("deadlock: Do must run outside the controller lock")
	}
	c.Verify()
	wantNoErrors(t, tb)
}

func TestDoPanicPropagatesToCaller(t *testing.T) {
	c := mock.NewController(&fakeTB{})
	c.Method("M").Expect().Do(func([]any) []any { panic("boom") })
	if msg := recovered(func() { c.Method("M").Call() }); msg != "boom" {
		t.Fatalf("panic = %q, want boom", msg)
	}
	// the controller is still usable afterwards
	c.Method("N").Expect().Return(1)
	if mock.Value[int](c.Method("N").Call(), 0) != 1 {
		t.Fatal("controller unusable after a Do panic")
	}
}

func TestValueKinds(t *testing.T) {
	u := &profile{Name: "bob"}
	c := mock.NewController(&fakeTB{})
	c.Method("M").Expect().Return(u, label("l"), profile{Name: "s"}, errNotFound, (*profile)(nil))
	r := c.Method("M").Call()
	if mock.Value[*profile](r, 0) != u {
		t.Fatal("pointer")
	}
	if mock.Value[namer](r, 1).Label() != "l" {
		t.Fatal("interface")
	}
	if mock.Value[profile](r, 2).Name != "s" {
		t.Fatal("struct")
	}
	if mock.Value[error](r, 3) != errNotFound {
		t.Fatal("error")
	}
	if p := mock.Value[*profile](r, 4); p != nil {
		t.Fatal("typed nil pointer")
	}
	if mock.Value[namer](r, 9) != nil || mock.Value[*profile](r, 9) != nil || mock.Value[profile](r, 9) != (profile{}) {
		t.Fatal("unset must be the zero value")
	}
}

func TestValueMismatchMessageNamesMethodAndIndex(t *testing.T) {
	c := mock.NewController(&fakeTB{})
	c.Method("Find").Expect().Return("text", 5)
	r := c.Method("Find").Call()
	for name, fn := range map[string]func(){
		"Value": func() { _ = mock.Value[int](r, 0) },
		"Err":   func() { _ = r.Err(1) },
	} {
		msg := recovered(fn)
		if !strings.Contains(msg, "Find") || !strings.Contains(msg, "result") {
			t.Fatalf("%s: unclear message %q", name, msg)
		}
	}
	if msg := recovered(func() { _ = mock.Value[int](r, 0) }); !strings.Contains(msg, "result 0") || !strings.Contains(msg, "string") || !strings.Contains(msg, "int") {
		t.Fatalf("unclear message %q", msg)
	}
}

func TestSequentialResponsesConcurrentlyDistinctAndComplete(t *testing.T) {
	const n = 200
	tb := &fakeTB{}
	c := mock.NewController(tb)
	e := c.Method("Next").Expect().Times(n)
	for i := 0; i < n; i++ {
		e.Return(i)
	}
	got := make([]int, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got[i] = mock.Value[int](c.Method("Next").Call(), 0)
		}()
	}
	wg.Wait()
	sort.Ints(got)
	for i, v := range got {
		if v != i {
			t.Fatalf("responses not distinct and complete at %d: %v", i, got[:i+1])
		}
	}
	c.Verify()
	wantNoErrors(t, tb)
}

func TestUnexpectedCallResultStaysZero(t *testing.T) {
	tb := &fakeTB{}
	c := mock.NewController(tb)
	c.Method("Find").Expect("known").Return("x")
	r := c.Method("Find").Call("other")
	if r.Get(0) != nil || mock.Value[string](r, 0) != "" {
		t.Fatal("result of an unexpected call must stay zero")
	}
	if len(tb.errs()) != 1 {
		t.Fatalf("want the unexpected call reported, got %q", tb.errs())
	}
}

func ExampleExpectation_Return() {
	tb := &fakeTB{}
	c := mock.NewController(tb)
	repo := c.Method("Find")
	repo.Expect("u1").Return(&profile{Name: "alice"}, nil)
	repo.Expect("u2").Return(nil, errNotFound)
	// sequential responses: the first call gets "a", the second "b", later calls repeat "b"
	next := c.Method("Next").Expect().Times(3).Return("a").Return("b")

	found := repo.Call("u1")
	fmt.Println(mock.Value[*profile](found, 0).Name, found.Err(1))
	missing := repo.Call("u2")
	fmt.Println(mock.Value[*profile](missing, 0) == nil, errors.Is(missing.Err(1), errNotFound))
	for i := 0; i < 3; i++ {
		fmt.Print(mock.Value[string](c.Method("Next").Call(), 0), " ")
	}
	fmt.Println()
	_ = next
	// Output:
	// alice <nil>
	// true true
	// a b b
}

func timeoutC() <-chan time.Time { return time.After(5 * time.Second) }
