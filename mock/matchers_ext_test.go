package mock_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/getsyntegrity/go-specs/mock"
)

func containsStr(s, sub string) bool { return strings.Contains(s, sub) }

func TestMatchPredicateAndDescription(t *testing.T) {
	tb := &fakeTB{}
	c := mock.NewController(tb)
	c.Method("Find").Expect(mock.Match("a non-empty string", func(v any) bool {
		s, ok := v.(string)
		return ok && s != ""
	})).AnyTimes()
	c.Method("Find").Call("x")
	if len(tb.errs()) != 0 {
		t.Fatalf("unexpected errors: %q", tb.errs())
	}
	c.Method("Find").Call("")
	wantErrContains(t, tb, `argument 0: got "", want a non-empty string`)
}

func TestMatchTTypedPredicate(t *testing.T) {
	tb := &fakeTB{}
	c := mock.NewController(tb)
	c.Method("Age").Expect(mock.MatchT("an adult age", func(n int) bool { return n >= 18 })).AnyTimes()
	c.Method("Age").Call(30)
	if len(tb.errs()) != 0 {
		t.Fatalf("unexpected errors: %q", tb.errs())
	}
	c.Method("Age").Call(3)
	wantErrContains(t, tb, "argument 0: got 3, want an adult age")
}

func TestMatchTWrongTypeDoesNotMatchAndSaysSo(t *testing.T) {
	tb := &fakeTB{}
	c := mock.NewController(tb)
	c.Method("Age").Expect(mock.MatchT("an adult age", func(n int) bool { return n >= 18 }))
	c.Method("Age").Call("thirty")
	wantErrContains(t, tb, `got "thirty"`, "an adult age", "int")
}

func TestMatchTNilForNilableType(t *testing.T) {
	m := mock.MatchT("nil error", func(err error) bool { return err == nil })
	if !m.Match(nil) {
		t.Fatal("nil must match an interface T when the predicate accepts the zero value")
	}
	if mock.MatchT("x", func(n int) bool { return true }).Match(nil) {
		t.Fatal("nil must not match a non-nilable T")
	}
}

func TestCaptorCapturesClaimedCalls(t *testing.T) {
	tb := &fakeTB{}
	c := mock.NewController(tb)
	cap := mock.NewCaptor[string]()
	c.Method("Save").Expect(cap.Matcher()).Times(2)
	c.Method("Save").Call("a")
	c.Method("Save").Call("b")
	if got := cap.Values(); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("Values = %v", got)
	}
	if cap.Last() != "b" {
		t.Fatalf("Last = %q", cap.Last())
	}
	// Values returns a copy.
	cap.Values()[0] = "mutated"
	if cap.Values()[0] != "a" {
		t.Fatal("Values must return a copy")
	}
}

func TestCaptorIgnoresCallRejectedForCapacity(t *testing.T) {
	tb := &fakeTB{}
	c := mock.NewController(tb)
	cap := mock.NewCaptor[string]()
	c.Method("Save").Expect(cap.Matcher()) // Times(1)
	c.Method("Save").Call("first")
	c.Method("Save").Call("second") // matches the captor but the expectation is full
	if got := cap.Values(); len(got) != 1 || got[0] != "first" {
		t.Fatalf("Values = %v, want [first]", got)
	}
	if len(tb.errs()) != 1 {
		t.Fatalf("want the unexpected call reported: %q", tb.errs())
	}
}

func TestCaptorIgnoresCallRejectedByAnotherArgument(t *testing.T) {
	tb := &fakeTB{}
	c := mock.NewController(tb)
	cap := mock.NewCaptor[string]()
	c.Method("Save").Expect(cap.Matcher(), "wanted").AnyTimes()
	c.Method("Save").Call("nope", "other")
	c.Method("Save").Call("yes", "wanted")
	if got := cap.Values(); len(got) != 1 || got[0] != "yes" {
		t.Fatalf("Values = %v, want [yes]", got)
	}
}

func TestCaptorFallsThroughToLaterExpectationWithoutCapturing(t *testing.T) {
	tb := &fakeTB{}
	c := mock.NewController(tb)
	first, second := mock.NewCaptor[string](), mock.NewCaptor[string]()
	c.Method("Save").Expect(first.Matcher())
	c.Method("Save").Expect(second.Matcher())
	c.Method("Save").Call("a")
	c.Method("Save").Call("b") // first is full: only second claims it
	if got := first.Values(); len(got) != 1 || got[0] != "a" {
		t.Fatalf("first = %v", got)
	}
	if got := second.Values(); len(got) != 1 || got[0] != "b" {
		t.Fatalf("second = %v", got)
	}
}

func TestCaptorWrongTypeDoesNotMatch(t *testing.T) {
	tb := &fakeTB{}
	c := mock.NewController(tb)
	cap := mock.NewCaptor[int]()
	c.Method("Save").Expect(cap.Matcher())
	c.Method("Save").Call("text")
	wantErrContains(t, tb, `got "text"`, "int")
	if len(cap.Values()) != 0 {
		t.Fatal("nothing must be captured")
	}
}

func TestCaptorLastOnEmpty(t *testing.T) {
	cap := mock.NewCaptor[int]()
	if cap.Last() != 0 || len(cap.Values()) != 0 {
		t.Fatal("empty captor must return zero value and no values")
	}
}

func TestCaptorConcurrent(t *testing.T) {
	tb := &fakeTB{}
	c := mock.NewController(tb)
	cap := mock.NewCaptor[int]()
	c.Method("Save").Expect(cap.Matcher()).AnyTimes()
	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func() { defer wg.Done(); c.Method("Save").Call(i); _ = cap.Last(); _ = cap.Values() }()
	}
	wg.Wait()
	vals := cap.Values()
	if len(vals) != 100 {
		t.Fatalf("captured %d, want 100", len(vals))
	}
	seen := map[int]bool{}
	for _, v := range vals {
		seen[v] = true
	}
	if len(seen) != 100 {
		t.Fatalf("captured %d distinct values, want 100", len(seen))
	}
}
