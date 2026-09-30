package mock_test

import (
	"sync"
	"testing"

	"github.com/getsyntegrity/go-specs/mock"
)

func TestAtLeastMetAndUnmet(t *testing.T) {
	tb := &fakeTB{}
	c := mock.NewController(tb)
	c.Method("Find").Expect(mock.Any()).AtLeast(2)
	c.Method("Find").Call("a")
	c.Verify()
	wantErrContains(t, tb, "unmet expectation", "want at least 2, got 1")

	tb2 := &fakeTB{}
	c2 := mock.NewController(tb2)
	c2.Method("Find").Expect(mock.Any()).AtLeast(2)
	for range 5 {
		c2.Method("Find").Call("a")
	}
	c2.Verify()
	if errs := tb2.errs(); len(errs) != 0 {
		t.Fatalf("unexpected errors: %q", errs)
	}
}

func TestAtMostExceededIsUnexpectedAtCapacity(t *testing.T) {
	tb := &fakeTB{}
	c := mock.NewController(tb)
	c.Method("Find").Expect(mock.Any()).AtMost(2)
	c.Method("Find").Call("a")
	c.Method("Find").Call("b")
	c.Method("Find").Call("c")
	wantErrContains(t, tb, `unexpected call Find("c")`, "at capacity", "want at most 2, got 2")
	c.Verify() // zero calls would also be fine for AtMost
	if len(tb.errs()) != 1 {
		t.Fatalf("Verify must not add errors: %q", tb.errs())
	}
}

func TestAtMostAllowsZeroCalls(t *testing.T) {
	tb := &fakeTB{}
	c := mock.NewController(tb)
	c.Method("Find").Expect(mock.Any()).AtMost(3)
	c.Verify()
	if errs := tb.errs(); len(errs) != 0 {
		t.Fatalf("unexpected errors: %q", errs)
	}
}

func TestAtLeastAtMostCombineIntoRange(t *testing.T) {
	for _, order := range []string{"min-first", "max-first"} {
		t.Run(order, func(t *testing.T) {
			tb := &fakeTB{}
			c := mock.NewController(tb)
			e := c.Method("Find").Expect(mock.Any())
			if order == "min-first" {
				e.AtLeast(2).AtMost(3)
			} else {
				e.AtMost(3).AtLeast(2)
			}
			c.Method("Find").Call("a")
			c.Verify()
			wantErrContains(t, tb, "want 2..3, got 1")

			tb2 := &fakeTB{}
			c2 := mock.NewController(tb2)
			e2 := c2.Method("Find").Expect(mock.Any())
			e2.AtLeast(2).AtMost(3)
			for range 4 {
				c2.Method("Find").Call("a")
			}
			wantErrContains(t, tb2, `unexpected call Find("a")`, "want 2..3, got 3")
		})
	}
}

func TestCountLastWinsAcrossKinds(t *testing.T) {
	tb := &fakeTB{}
	c := mock.NewController(tb)
	// Times resets any earlier bound; the later Times(1) wins over AtLeast(5).
	c.Method("Find").Expect(mock.Any()).AtLeast(5).Times(1)
	c.Method("Find").Call("a")
	c.Verify()
	if errs := tb.errs(); len(errs) != 0 {
		t.Fatalf("unexpected errors: %q", errs)
	}

	// AtLeast after Times replaces the exact count instead of combining.
	tb2 := &fakeTB{}
	c2 := mock.NewController(tb2)
	c2.Method("Find").Expect(mock.Any()).Times(1).AtLeast(2)
	c2.Method("Find").Call("a")
	c2.Verify()
	wantErrContains(t, tb2, "want at least 2, got 1")
}

func TestCountPanics(t *testing.T) {
	c := mock.NewController(&fakeTB{})
	e := c.Method("Find").Expect()
	cases := map[string]func(){
		"AtLeast negative": func() { e.AtLeast(-1) },
		"AtMost negative":  func() { e.AtMost(-1) },
		"min above max":    func() { e.AtMost(1).AtLeast(2) },
		"max below min":    func() { e.AtLeast(3).AtMost(2) },
	}
	for name, fn := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("expected panic")
				}
			}()
			fn()
		})
	}
}

func TestNeverViolatedImmediately(t *testing.T) {
	tb := &fakeTB{}
	c := mock.NewController(tb)
	c.Method("Delete").Expect("u1").Never()
	c.Method("Delete").Call("u1")
	wantErrContains(t, tb, `forbidden call Delete("u1")`, "declared at", "counts_test.go:", "says never")
	c.Verify()
	if len(tb.errs()) != 1 {
		t.Fatalf("Verify must not add errors: %q", tb.errs())
	}
}

func TestNeverSatisfiedWithoutCalls(t *testing.T) {
	tb := &fakeTB{}
	c := mock.NewController(tb)
	c.Method("Delete").Expect("u1").Never()
	c.Method("Delete").Call("u2") // does not match: reported as ordinary unexpected call
	if errs := tb.errs(); len(errs) != 1 {
		t.Fatalf("want 1 error for the non-matching call, got %q", errs)
	}
}

func TestAtMostZeroEqualsNever(t *testing.T) {
	tb := &fakeTB{}
	c := mock.NewController(tb)
	c.Method("Delete").Expect().AtMost(0)
	c.Method("Delete").Call()
	wantErrContains(t, tb, "says never")
}

func TestAnyTimes(t *testing.T) {
	tb := &fakeTB{}
	c := mock.NewController(tb)
	c.Method("Ping").Expect().AnyTimes()
	c.Method("Other").Expect().AnyTimes()
	for range 50 {
		c.Method("Ping").Call()
	}
	c.Verify()
	if errs := tb.errs(); len(errs) != 0 {
		t.Fatalf("unexpected errors: %q", errs)
	}
}

func TestCountTextInDiagnostics(t *testing.T) {
	tb := &fakeTB{}
	c := mock.NewController(tb)
	c.Method("A").Expect().AtLeast(2)
	c.Method("B").Expect().Times(2)
	c.Method("C").Expect().AtLeast(1).AtMost(4)
	c.Method("A").Call()
	c.Verify()
	errs := tb.errs()
	if len(errs) != 3 {
		t.Fatalf("want 3 errors, got %q", errs)
	}
	want := []string{"want at least 2, got 1", "want 2, got 0", "want 1..4, got 0"}
	for i, w := range want {
		if !containsStr(errs[i], w) {
			t.Fatalf("error %d %q lacks %q", i, errs[i], w)
		}
	}
}

func TestCountsConcurrentAtMost(t *testing.T) {
	tb := &fakeTB{}
	c := mock.NewController(tb)
	c.Method("Find").Expect(mock.Any()).AtMost(10)
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
