package mock_test

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/getsyntegrity/go-specs/mock"
)

// accepts reports whether an expectation configured by cfg is satisfied by exactly k matching
// calls: no unexpected-call error while calling and no unmet-expectation error at Verify.
func accepts(cfg func(e *mock.Expectation), k int) bool {
	tb := &fakeTB{}
	c := mock.NewController(tb)
	cfg(c.Method("Find").Expect(mock.Any()))
	for range k {
		c.Method("Find").Call("a")
	}
	c.Verify()
	return len(tb.errs()) == 0
}

func TestCountChainsResolveToExplicitBounds(t *testing.T) {
	cases := []struct {
		name   string
		cfg    func(e *mock.Expectation)
		lo, hi int // hi < 0 means unbounded
	}{
		{"AtLeast(2).AtMost(5).AtLeast(3)", func(e *mock.Expectation) { e.AtLeast(2).AtMost(5).AtLeast(3) }, 3, 5},
		{"AtMost(5).AtLeast(2).AtMost(3)", func(e *mock.Expectation) { e.AtMost(5).AtLeast(2).AtMost(3) }, 2, 3},
		{"AtMost(5).AtLeast(2).AtLeast(4)", func(e *mock.Expectation) { e.AtMost(5).AtLeast(2).AtLeast(4) }, 4, 5},
		{"AtLeast(2).AtMost(6).AtMost(4)", func(e *mock.Expectation) { e.AtLeast(2).AtMost(6).AtMost(4) }, 2, 4},
		{"AtLeast(1).AtMost(6).AtLeast(2).AtMost(5).AtLeast(3)", func(e *mock.Expectation) {
			e.AtLeast(1).AtMost(6).AtLeast(2).AtMost(5).AtLeast(3)
		}, 3, 5},
		{"AtLeast(0)", func(e *mock.Expectation) { e.AtLeast(0) }, 0, -1},
		{"AtMost(0)", func(e *mock.Expectation) { e.AtMost(0) }, 0, 0},
		{"AtLeast(0).AtMost(0)", func(e *mock.Expectation) { e.AtLeast(0).AtMost(0) }, 0, 0},
		{"AtMost(0).AtLeast(0)", func(e *mock.Expectation) { e.AtMost(0).AtLeast(0) }, 0, 0},
		{"AtMost(3).AtLeast(0)", func(e *mock.Expectation) { e.AtMost(3).AtLeast(0) }, 0, 3},
		{"Times(2).AtLeast(1) starts a new range", func(e *mock.Expectation) { e.Times(2).AtLeast(1) }, 1, -1},
		{"Times(2).AtMost(4) starts a new range", func(e *mock.Expectation) { e.Times(2).AtMost(4) }, 0, 4},
		{"Never().AtLeast(1) starts a new range", func(e *mock.Expectation) { e.Never().AtLeast(1) }, 1, -1},
		{"Never().AtMost(2) starts a new range", func(e *mock.Expectation) { e.Never().AtMost(2) }, 0, 2},
		{"AnyTimes().AtLeast(2) starts a new range", func(e *mock.Expectation) { e.AnyTimes().AtLeast(2) }, 2, -1},
		{"AnyTimes().AtMost(2) starts a new range", func(e *mock.Expectation) { e.AnyTimes().AtMost(2) }, 0, 2},
		{"AtLeast(2).AtMost(5).Times(1)", func(e *mock.Expectation) { e.AtLeast(2).AtMost(5).Times(1) }, 1, 1},
		{"AtLeast(2).AtMost(5).Never()", func(e *mock.Expectation) { e.AtLeast(2).AtMost(5).Never() }, 0, 0},
		{"AtLeast(2).AtMost(5).AnyTimes()", func(e *mock.Expectation) { e.AtLeast(2).AtMost(5).AnyTimes() }, 0, -1},
		{"Times(3).AtLeast(1).AtMost(2)", func(e *mock.Expectation) { e.Times(3).AtLeast(1).AtMost(2) }, 1, 2},
		{"default is exactly 1", func(*mock.Expectation) {}, 1, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for k := 0; k <= 8; k++ {
				want := k >= tc.lo && (tc.hi < 0 || k <= tc.hi)
				if got := accepts(tc.cfg, k); got != want {
					t.Fatalf("%d calls: accepted=%v, want %v (range %d..%d)", k, got, want, tc.lo, tc.hi)
				}
			}
		})
	}
}

func TestCountRangeDiagnosticsAfterChains(t *testing.T) {
	tb := &fakeTB{}
	c := mock.NewController(tb)
	c.Method("A").Expect().AtLeast(2).AtMost(5).AtLeast(3)
	c.Method("B").Expect().AtMost(5).AtLeast(2).AtMost(3)
	c.Method("C").Expect().AtLeast(0).AtMost(4).AtMost(0)
	c.Method("D").Expect().AtMost(0)
	c.Method("C").Call()
	c.Method("D").Call()
	c.Verify()
	errs := tb.errs()
	if len(errs) != 4 {
		t.Fatalf("want 4 errors, got %q", errs)
	}
	// Calls to C and D are reported at once; Verify then reports A and B in declaration order.
	want := []string{"says never", "says never", "want 3..5, got 0", "want 2..3, got 0"}
	for i, w := range want {
		if !strings.Contains(errs[i], w) {
			t.Fatalf("error %d %q lacks %q", i, errs[i], w)
		}
	}
}

func TestCountRangeAtMostZeroUnmetText(t *testing.T) {
	// A never expectation reached through a range has nothing to verify; its text is "never".
	tb := &fakeTB{}
	c := mock.NewController(tb)
	c.Method("Find").Expect().AtLeast(0).AtMost(0)
	c.Method("Find").Call()
	wantErrContains(t, tb, "says never")
}

func TestCountRangePanicsWithClearMessage(t *testing.T) {
	cases := []struct {
		name string
		fn   func(e *mock.Expectation)
		want string
	}{
		{"AtMost(0).AtLeast(1)", func(e *mock.Expectation) { e.AtMost(0).AtLeast(1) }, "AtLeast(1) exceeds the maximum of 0"},
		{"AtLeast(3).AtMost(2)", func(e *mock.Expectation) { e.AtLeast(3).AtMost(2) }, "AtMost(2) is below the minimum of 3"},
		{"AtLeast(1).AtMost(0)", func(e *mock.Expectation) { e.AtLeast(1).AtMost(0) }, "AtMost(0) is below the minimum of 1"},
		{"three-op chain", func(e *mock.Expectation) { e.AtLeast(2).AtMost(5).AtLeast(6) }, "AtLeast(6) exceeds the maximum of 5"},
		{"three-op chain max", func(e *mock.Expectation) { e.AtMost(5).AtLeast(2).AtMost(1) }, "AtMost(1) is below the minimum of 2"},
		{"AtLeast negative", func(e *mock.Expectation) { e.AtLeast(-1) }, "AtLeast requires n >= 0, got -1"},
		{"AtMost negative", func(e *mock.Expectation) { e.AtMost(-1) }, "AtMost requires n >= 0, got -1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := mock.NewController(&fakeTB{}).Method("Find").Expect()
			msg := func() (msg string) {
				defer func() { msg = fmt.Sprint(recover()) }()
				tc.fn(e)
				return "no panic"
			}()
			if !strings.Contains(msg, tc.want) {
				t.Fatalf("panic %q lacks %q", msg, tc.want)
			}
		})
	}
}

func TestCountRangeUnchangedByPanickingCall(t *testing.T) {
	cfg := func(e *mock.Expectation) {
		e.AtLeast(2).AtMost(5)
		func() {
			defer func() { _ = recover() }()
			e.AtLeast(6)
		}()
	}
	for k := 0; k <= 7; k++ {
		if want := k >= 2 && k <= 5; accepts(cfg, k) != want {
			t.Fatalf("%d calls: accepted=%v, want %v", k, !want, want)
		}
	}
}

func TestCountsConcurrentRange(t *testing.T) {
	tb := &fakeTB{}
	c := mock.NewController(tb)
	c.Method("Find").Expect(mock.Any()).AtLeast(2).AtMost(9).AtLeast(3).AtMost(10)
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() { defer wg.Done(); c.Method("Find").Call("a") }()
	}
	wg.Wait()
	if got := len(tb.errs()); got != 40 {
		t.Fatalf("want exactly 40 over-capacity errors, got %d", got)
	}
}
