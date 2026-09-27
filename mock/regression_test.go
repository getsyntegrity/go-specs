package mock

import (
	"errors"
	"fmt"
	"testing"
)

var errNotFound = errors.New("not found")

// Issue #239: mock.Equal must apply the same oriented error semantics as assert.Equal.
func TestEqualMatchesErrorsByIdentityNotStructure(t *testing.T) {
	cases := []struct {
		name string
		arg  any
		want bool
	}{
		{"same sentinel", errNotFound, true},
		{"wrapped sentinel", fmt.Errorf("repo: %w", errNotFound), true},
		{"unrelated error with the same message", errors.New("not found"), false},
		{"non-error argument", "not found", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			spy := NewSpy()
			spy.Call(c.arg)
			if got := spy.CalledWith(Equal(errNotFound)); got != c.want {
				t.Fatalf("CalledWith(Equal(errNotFound)) with %v = %v, want %v", c.arg, got, c.want)
			}
		})
	}
}

// Non-error values keep the structural semantics mock.Equal always had.
func TestEqualKeepsStructuralSemanticsForNonErrors(t *testing.T) {
	spy := NewSpy()
	spy.Call([]int{1, 2}, map[string]int{"a": 1}, nil)
	if !spy.CalledWith(Equal([]int{1, 2}), Equal(map[string]int{"a": 1}), Equal(nil)) {
		t.Fatal("expected structurally equal slice, map and nil arguments to match")
	}
	if spy.CalledWith(Equal([]int{2, 1}), Any(), Any()) {
		t.Fatal("expected a different slice not to match")
	}
}

// Issue #240: the recorded call must not alias the caller's variadic slice.
func TestCallCopiesTheArgumentSlice(t *testing.T) {
	spy := NewSpy()
	args := []any{"a", 1}
	spy.Call(args...)
	args[0] = "mutated"
	if got := spy.Calls()[0].Args[0]; got != "a" {
		t.Fatalf("recorded first argument = %v after the caller mutated its slice, want %q", got, "a")
	}
	if !spy.CalledWith(Equal("a"), Equal(1)) {
		t.Fatal("CalledWith must see the arguments as they were at call time")
	}
}

// Issue #241: the zero value of Mock must be usable, like other Go types with a mutex.
func TestZeroValueMockIsUsable(t *testing.T) {
	var m Mock
	s := m.Spy("save")
	s.Call(1)
	if m.Spy("save") != s {
		t.Fatal("expected the same spy back for the same name")
	}
	if m.Spy("save").CallCount() != 1 {
		t.Fatal("expected the call recorded on the zero-value Mock's spy")
	}
}

// fakeTB records Fatal/Fatalf instead of stopping the goroutine, so a test can observe them.
type fakeTB struct {
	testing.TB
	fatals []string
}

func (f *fakeTB) Helper() {}

func (f *fakeTB) Fatal(args ...any) { f.fatals = append(f.fatals, fmt.Sprint(args...)) }

func (f *fakeTB) Fatalf(format string, args ...any) {
	f.fatals = append(f.fatals, fmt.Sprintf(format, args...))
}

// Issue #246: CalledTimes must accept any testing.TB, not only *testing.T.
func TestCalledTimesAcceptsTestingTB(t *testing.T) {
	spy := NewSpy()
	spy.Call()
	spy.Call()

	pass := &fakeTB{}
	spy.CalledTimes(pass, 2)
	if len(pass.fatals) != 0 {
		t.Fatalf("matching count must not fail, got %q", pass.fatals)
	}

	fail := &fakeTB{}
	spy.CalledTimes(fail, 3)
	if len(fail.fatals) != 1 || fail.fatals[0] != "expected 3 calls, got 2" {
		t.Fatalf("fatals = %q, want [\"expected 3 calls, got 2\"]", fail.fatals)
	}
}

// Issue #246: a nil spy fails once and stops, even when the TB's Fatal returns.
func TestCalledTimesNilSpyFailsOnce(t *testing.T) {
	var spy *Spy
	tb := &fakeTB{}
	spy.CalledTimes(tb, 1)
	if len(tb.fatals) != 1 || tb.fatals[0] != "spy is nil" {
		t.Fatalf("fatals = %q, want [\"spy is nil\"]", tb.fatals)
	}
}
