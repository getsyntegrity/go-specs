package mock_test

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/getsyntegrity/go-specs/mock"
)

// zeroMax lists the spellings of a prohibition: all must behave the same.
var zeroMax = map[string]func(*mock.Expectation) *mock.Expectation{
	"Never":           func(e *mock.Expectation) *mock.Expectation { return e.Never() },
	"Times0":          func(e *mock.Expectation) *mock.Expectation { return e.Times(0) },
	"AtMost0":         func(e *mock.Expectation) *mock.Expectation { return e.AtMost(0) },
	"AtLeast0AtMost0": func(e *mock.Expectation) *mock.Expectation { return e.AtLeast(0).AtMost(0) },
}

// declare adds the prohibition and a permissive fallback in the requested order.
func declare(m *mock.Method, prohibitFirst bool, prohibit func(*mock.Expectation) *mock.Expectation) *mock.Expectation {
	var fb *mock.Expectation
	if prohibitFirst {
		prohibit(m.Expect("protected"))
		fb = m.Expect(mock.Any()).AnyTimes()
	} else {
		fb = m.Expect(mock.Any()).AnyTimes()
		prohibit(m.Expect("protected"))
	}
	return fb
}

func TestProhibitionBeatsPermissiveExpectation(t *testing.T) {
	for name, prohibit := range zeroMax {
		for _, first := range []bool{true, false} {
			tb := &fakeTB{}
			c := mock.NewController(tb)
			m := c.Method("Delete")
			declare(m, first, prohibit)
			m.Call("protected")
			wantErrContains(t, tb, `forbidden call Delete("protected")`, "says never", "prohibition_test.go:")
			c.Verify()
			if len(tb.errs()) != 1 {
				t.Fatalf("%s first=%v: Verify must not report it again: %q", name, first, tb.errs())
			}
		}
	}
}

func TestForbiddenDiagnosticText(t *testing.T) {
	tb := &fakeTB{}
	c := mock.NewController(tb)
	m := c.Method("Delete")
	m.Expect("protected").Never()
	m.Expect(mock.Any()).AnyTimes()
	m.Call("protected")
	wantErrContains(t, tb, `mock: forbidden call Delete("protected"): expectation Delete(equal to "protected") declared at prohibition_test.go:`, "says never")
}

func TestNonProhibitedCallUsesFallback(t *testing.T) {
	for _, first := range []bool{true, false} {
		tb := &fakeTB{}
		c := mock.NewController(tb)
		m := c.Method("Delete")
		fb := declare(m, first, zeroMax["Never"])
		fb.Return("ok")
		if got := mock.Value[string](m.Call("other"), 0); got != "ok" {
			t.Fatalf("first=%v: fallback Return = %q", first, got)
		}
		wantNoErrors(t, tb)
	}
}

func TestProhibitionStaysAfterValidCalls(t *testing.T) {
	tb := &fakeTB{}
	c := mock.NewController(tb)
	m := c.Method("Delete")
	declare(m, true, zeroMax["Times0"])
	m.Call("a")
	m.Call("b")
	wantNoErrors(t, tb)
	m.Call("protected")
	wantErrContains(t, tb, "forbidden call")
	m.Call("c")
	if len(tb.errs()) != 1 {
		t.Fatalf("valid call after the forbidden one must pass: %q", tb.errs())
	}
}

func TestForbiddenCallRunsNothingAndReturnsZero(t *testing.T) {
	for _, first := range []bool{true, false} {
		tb := &fakeTB{}
		c := mock.NewController(tb)
		m := c.Method("Delete")
		var dos int
		cap := mock.NewCaptor[string]()
		var fb *mock.Expectation
		if first {
			m.Expect("protected").Never()
			fb = m.Expect(cap.Matcher()).AnyTimes()
		} else {
			fb = m.Expect(cap.Matcher()).AnyTimes()
			m.Expect("protected").Never()
		}
		fb.Return("fallback").Do(func([]any) []any { dos++; return []any{"do"} })
		res := m.Call("protected")
		if dos != 0 {
			t.Fatalf("first=%v: Do ran %d times", first, dos)
		}
		if v := cap.Values(); len(v) != 0 {
			t.Fatalf("first=%v: captor saw %q", first, v)
		}
		if res.Get(0) != nil {
			t.Fatalf("first=%v: result must be zero, got %v", first, res.Get(0))
		}
		if len(tb.errs()) != 1 {
			t.Fatalf("first=%v: want 1 error, got %q", first, tb.errs())
		}
	}
}

func TestForbiddenCallRecordedExactlyOnce(t *testing.T) {
	tb := &fakeTB{}
	c := mock.NewController(tb)
	m := c.Method("Delete")
	declare(m, false, zeroMax["Never"])
	m.Call("a")
	m.Call("protected")
	m.Call("b")
	calls := c.Calls()
	if len(calls) != 3 || len(m.Calls()) != 3 {
		t.Fatalf("want 3 recorded calls, got %d / %d", len(calls), len(m.Calls()))
	}
	for i, rc := range calls {
		if rc.Seq != uint64(i+1) {
			t.Fatalf("call %d has seq %d", i, rc.Seq)
		}
	}
	if calls[1].Args[0] != "protected" {
		t.Fatalf("second call = %v", calls[1].Args)
	}
}

func TestProhibitionConcurrentMix(t *testing.T) {
	tb := &fakeTB{}
	c := mock.NewController(tb)
	m := c.Method("Delete")
	declare(m, false, zeroMax["AtMost0"])
	const n = 200
	var wg sync.WaitGroup
	var forbidden atomic.Int32
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%3 == 0 {
				forbidden.Add(1)
				m.Call("protected")
			} else {
				m.Call("ok")
			}
		}(i)
	}
	wg.Wait()
	errs := tb.errs()
	if int32(len(errs)) != forbidden.Load() {
		t.Fatalf("want %d forbidden reports, got %d", forbidden.Load(), len(errs))
	}
	for _, e := range errs {
		if !strings.Contains(e, "forbidden call") {
			t.Fatalf("allowed call rejected: %q", e)
		}
	}
	if len(c.Calls()) != n {
		t.Fatalf("want %d recorded calls, got %d", n, len(c.Calls()))
	}
}
