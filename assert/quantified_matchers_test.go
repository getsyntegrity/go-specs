package assert

import (
	"fmt"
	"strings"
	"testing"
)

// The quantified matchers judge each element of a collection with a child matcher. These tests pin
// the truth tables (vacuous truth on empty input, counts, duplicates), the nil and unsupported
// inputs, the wording of every diagnostic (failing indices, counts), composition with Not/All/Any
// and, above all, that a child matcher is asked about each element at most once per evaluation.

// qOrder and qItem are small domain objects, so the tests read like the examples in docs/DSL.md.
type qItem struct {
	SKU string
	Qty int
}

type qOrder struct {
	ID    string
	Items []qItem
}

// qCounting wraps a predicate and counts how often the matcher is called, through Match and
// through FailureMessage, so a test can prove that a child is never asked twice about one element.
type qCounting struct {
	pred  func(any) bool
	calls *int
	seen  *[]any
}

func newQCounting(pred func(any) bool) qCounting {
	return qCounting{pred: pred, calls: new(int), seen: new([]any)}
}

func (c qCounting) Match(actual any) bool {
	*c.calls++
	*c.seen = append(*c.seen, actual)
	return c.pred(actual)
}

func (c qCounting) FailureMessage(actual any) string {
	*c.calls++
	return fmt.Sprintf("counting: %v rejected", actual)
}

func (c qCounting) Description() string { return "counted" }

// Evaluate makes qCounting an Evaluator, so a call through the package-level Evaluate is exactly one.
func (c qCounting) Evaluate(actual any) (bool, string) {
	*c.calls++
	*c.seen = append(*c.seen, actual)
	if c.pred(actual) {
		return true, ""
	}
	return false, fmt.Sprintf("counting: %v rejected", actual)
}

func isPositive(v any) bool { n, ok := v.(int); return ok && n > 0 }

func TestQuantifiedTruthTable(t *testing.T) {
	positive := Satisfy("positive", isPositive)
	cases := []struct {
		name    string
		m       Matcher
		actual  any
		matches bool
	}{
		{"every all match", EveryElement(positive), []int{1, 2, 3}, true},
		{"every one fails", EveryElement(positive), []int{1, -2, 3}, false},
		{"every empty is vacuously true", EveryElement(positive), []int{}, true},
		{"every nil slice is vacuously true", EveryElement(positive), []int(nil), true},
		{"every array", EveryElement(positive), [3]int{1, 2, 3}, true},
		{"any one matches", AnyElement(positive), []int{-1, 2, -3}, true},
		{"any none match", AnyElement(positive), []int{-1, -2}, false},
		{"any empty is false", AnyElement(positive), []int{}, false},
		{"none no match", NoElement(positive), []int{-1, -2}, true},
		{"none one matches", NoElement(positive), []int{-1, 2}, false},
		{"none empty is vacuously true", NoElement(positive), []int{}, true},
		{"exactly 2 of 2", ExactlyNElements(2, positive), []int{1, -1, 2}, true},
		{"exactly 2 of 3", ExactlyNElements(2, positive), []int{1, 2, 3}, false},
		{"exactly 2 of 1", ExactlyNElements(2, positive), []int{1, -2}, false},
		{"exactly 0 on empty", ExactlyNElements(0, positive), []int{}, true},
		{"exactly 1 on empty", ExactlyNElements(1, positive), []int{}, false},
		{"at least 2 met", AtLeastNElements(2, positive), []int{1, 2, 3}, true},
		{"at least 2 exact", AtLeastNElements(2, positive), []int{1, -1, 3}, true},
		{"at least 2 short", AtLeastNElements(2, positive), []int{1, -1}, false},
		{"at least 0 on empty", AtLeastNElements(0, positive), []int{}, true},
		{"at most 2 met", AtMostNElements(2, positive), []int{1, -1, 3}, true},
		{"at most 2 exceeded", AtMostNElements(2, positive), []int{1, 2, 3}, false},
		{"at most 0 on empty", AtMostNElements(0, positive), []int{}, true},
		{"at most 0 with a match", AtMostNElements(0, positive), []int{1}, false},
		{"duplicates count each", ExactlyNElements(3, Equal(7)), []int{7, 7, 1, 7}, true},
		{"at most with duplicates", AtMostNElements(2, Equal(7)), []int{7, 7, 7}, false},
		{"any slice of any", AnyElement(Equal("x")), []any{1, "x", nil}, true},
		{"nil elements reach the child", ExactlyNElements(1, BeNil()), []any{1, nil, "a"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.m.Match(tc.actual); got != tc.matches {
				t.Errorf("Match = %v, want %v", got, tc.matches)
			}
			matched, failure := Evaluate(tc.m, tc.actual)
			if matched != tc.matches {
				t.Errorf("Evaluate matched = %v, want %v", matched, tc.matches)
			}
			if !matched && failure == "" {
				t.Error("Evaluate reported a failure with an empty message")
			}
			if matched && failure != "" {
				t.Errorf("Evaluate matched with a message: %q", failure)
			}
		})
	}
}

func TestQuantifiedUnsupportedActual(t *testing.T) {
	positive := Satisfy("positive", isPositive)
	matchers := map[string]Matcher{
		"EveryElement":     EveryElement(positive),
		"AnyElement":       AnyElement(positive),
		"NoElement":        NoElement(positive),
		"ExactlyNElements": ExactlyNElements(0, positive),
		"AtLeastNElements": AtLeastNElements(0, positive),
		"AtMostNElements":  AtMostNElements(5, positive),
	}
	for name, m := range matchers {
		for _, actual := range []any{nil, 42, "abc", map[string]int{"a": 1}, &[]int{1}} {
			t.Run(fmt.Sprintf("%s/%T", name, actual), func(t *testing.T) {
				if m.Match(actual) {
					t.Fatalf("%s matched unsupported %T", name, actual)
				}
				matched, failure := Evaluate(m, actual)
				if matched {
					t.Fatal("Evaluate matched an unsupported value")
				}
				want := fmt.Sprintf("%s: %T is not a slice or array", name, actual)
				if failure != want {
					t.Errorf("failure = %q, want %q", failure, want)
				}
			})
		}
	}
	// As with HaveLen, Not turns "unsupported" into success.
	if !Not(EveryElement(positive)).Match(42) {
		t.Error("Not(EveryElement) must succeed on an unsupported value")
	}
}

func TestQuantifiedNilChildNeverMatches(t *testing.T) {
	var typedNil *derefMatcher
	for name, m := range map[string]func(Matcher) Matcher{
		"EveryElement":     EveryElement,
		"AnyElement":       AnyElement,
		"NoElement":        NoElement,
		"ExactlyNElements": func(c Matcher) Matcher { return ExactlyNElements(0, c) },
		"AtLeastNElements": func(c Matcher) Matcher { return AtLeastNElements(0, c) },
		"AtMostNElements":  func(c Matcher) Matcher { return AtMostNElements(9, c) },
	} {
		for label, child := range map[string]Matcher{"untyped": nil, "typed": typedNil} {
			t.Run(name+"/"+label, func(t *testing.T) {
				// Even on an empty collection: a nil child is a caller mistake, never masked by vacuity.
				for _, actual := range []any{[]int{1, 2}, []int{}} {
					matched, failure := Evaluate(m(child), actual)
					if matched {
						t.Fatalf("nil child matched %v", actual)
					}
					if want := name + ": " + nilMatcherFailure; failure != want {
						t.Errorf("failure = %q, want %q", failure, want)
					}
				}
			})
		}
	}
}

func TestQuantifiedInvalidCount(t *testing.T) {
	for name, m := range map[string]Matcher{
		"ExactlyNElements": ExactlyNElements(-1, BeNil()),
		"AtLeastNElements": AtLeastNElements(-1, BeNil()),
		"AtMostNElements":  AtMostNElements(-1, BeNil()),
	} {
		matched, failure := Evaluate(m, []any{nil})
		if matched {
			t.Errorf("%s with a negative count matched", name)
		}
		if want := name + ": count must not be negative, got -1"; failure != want {
			t.Errorf("failure = %q, want %q", failure, want)
		}
	}
}

func TestQuantifiedDiagnosticsNameIndices(t *testing.T) {
	positive := Satisfy("positive", isPositive)
	cases := []struct {
		name   string
		m      Matcher
		actual any
		want   []string
	}{
		{
			"every lists failing indices",
			EveryElement(positive), []int{1, -2, 3, 0},
			[]string{"EveryElement: 2 of 4 elements failed", "[1] -2:", "[3] 0:", "positive"},
		},
		{
			"any lists every element as a miss",
			AnyElement(positive), []int{-1, -2},
			[]string{"AnyElement: none of 2 elements matched", "[0] -1:", "[1] -2:"},
		},
		{
			"any on empty",
			AnyElement(positive), []int{},
			[]string{"AnyElement: the collection is empty, so no element could match"},
		},
		{
			"none lists the matching indices",
			NoElement(positive), []int{-1, 2, -3, 4},
			[]string{"NoElement: 2 of 4 elements matched", "[1] 2", "[3] 4"},
		},
		{
			"exactly too many names the matches",
			ExactlyNElements(1, positive), []int{1, -1, 2},
			[]string{"ExactlyNElements: expected exactly 1 of 3 elements to match, 2 did", "matching indices [0 2]"},
		},
		{
			"exactly too few names the misses",
			ExactlyNElements(2, positive), []int{1, -1, -2},
			[]string{"expected exactly 2 of 3 elements to match, 1 did", "matching indices [0]", "[1] -1:", "[2] -2:"},
		},
		{
			"at least",
			AtLeastNElements(2, positive), []int{1, -1, -2},
			[]string{"AtLeastNElements: expected at least 2 of 3 elements to match, 1 did", "matching indices [0]", "[1] -1:"},
		},
		{
			"at most",
			AtMostNElements(1, positive), []int{1, -1, 2, 3},
			[]string{"AtMostNElements: expected at most 1 of 4 elements to match, 3 did", "matching indices [0 2 3]"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, failure := Evaluate(tc.m, tc.actual)
			for _, w := range tc.want {
				if !strings.Contains(failure, w) {
					t.Errorf("failure %q does not contain %q", failure, w)
				}
			}
		})
	}
}

func TestQuantifiedDiagnosticsAreBounded(t *testing.T) {
	big := make([]int, 100)
	_, failure := Evaluate(EveryElement(Equal(1)), big)
	if !strings.Contains(failure, "100 of 100 elements failed") {
		t.Errorf("count missing: %q", failure)
	}
	if !strings.Contains(failure, "[9] 0:") || strings.Contains(failure, "[10] 0:") {
		t.Errorf("expected exactly the first 10 failing elements listed: %q", failure)
	}
	if !strings.Contains(failure, "90 more not shown") {
		t.Errorf("expected a truncation marker: %q", failure)
	}
	_, failure = Evaluate(NoElement(Equal(0)), big)
	if !strings.Contains(failure, "90 more not shown") {
		t.Errorf("expected a truncation marker for matches: %q", failure)
	}
}

func TestQuantifiedEvaluatesEachElementAtMostOnce(t *testing.T) {
	data := []int{1, -2, 3, -4, 5}
	build := map[string]func(Matcher) Matcher{
		"EveryElement":     EveryElement,
		"AnyElement":       AnyElement,
		"NoElement":        NoElement,
		"ExactlyNElements": func(c Matcher) Matcher { return ExactlyNElements(2, c) },
		"AtLeastNElements": func(c Matcher) Matcher { return AtLeastNElements(2, c) },
		"AtMostNElements":  func(c Matcher) Matcher { return AtMostNElements(2, c) },
	}
	for name, mk := range build {
		t.Run(name, func(t *testing.T) {
			c := newQCounting(isPositive)
			// Through Evaluate, directly, and nested in Not/All/Any: one call per element at most.
			for label, m := range map[string]Matcher{
				"direct": mk(c),
				"not":    Not(mk(c)),
				"all":    All(mk(c), mk(c)),
				"any":    Any(mk(c), mk(c)),
			} {
				*c.calls = 0
				*c.seen = nil
				Evaluate(m, data)
				limit := len(data)
				if label == "all" || label == "any" {
					limit *= 2
				}
				if *c.calls > limit {
					t.Errorf("%s: %d child calls for %d elements", label, *c.calls, len(data))
				}
			}
			*c.calls = 0
			Evaluate(mk(c), data)
			if *c.calls > len(data) {
				t.Errorf("%d child calls for %d elements", *c.calls, len(data))
			}
		})
	}
}

// A child with a side effect (like MatchErrorAs) must see each element once even when the failure
// message is built: that is the point of the single-pass evaluator.
func TestQuantifiedNeverReevaluatesForDiagnostics(t *testing.T) {
	c := newQCounting(isPositive)
	matched, failure := Evaluate(EveryElement(c), []int{1, -1, -2})
	if matched || failure == "" {
		t.Fatalf("expected a failure, got %v %q", matched, failure)
	}
	if *c.calls != 3 {
		t.Errorf("child called %d times for 3 elements", *c.calls)
	}
}

func TestQuantifiedCompositionWithNotAllAny(t *testing.T) {
	positive := Satisfy("positive", isPositive)
	even := Satisfy("even", func(v any) bool { n, ok := v.(int); return ok && n%2 == 0 })
	data := []int{2, 4, 6}

	if !qMatch(All(EveryElement(positive), EveryElement(even)), data) {
		t.Error("All of two quantifiers should hold")
	}
	if Not(EveryElement(positive)).Match(data) {
		t.Error("Not(EveryElement) must fail when every element matches")
	}
	if !Any(NoElement(positive), AnyElement(even)).Match(data) {
		t.Error("Any should hold through its second entry")
	}
	_, failure := Evaluate(All(EveryElement(positive), AtLeastNElements(4, even)), data)
	if !strings.Contains(failure, "#2:") || strings.Contains(failure, "#1:") {
		t.Errorf("All should name only the failing entry: %q", failure)
	}
	if !strings.Contains(failure, "AtLeastNElements: expected at least 4 of 3") {
		t.Errorf("All should carry the quantifier's own message: %q", failure)
	}
	_, failure = Evaluate(Not(EveryElement(positive)), data)
	if !strings.Contains(failure, "not to be every element is satisfying \"positive\"") {
		t.Errorf("Not's message should use Description: %q", failure)
	}
}

func qMatch(m Matcher, actual any) bool { ok, _ := Evaluate(m, actual); return ok }

func TestQuantifiedNested(t *testing.T) {
	orders := []qOrder{
		{ID: "A", Items: []qItem{{"x", 1}, {"y", 2}}},
		{ID: "B", Items: []qItem{{"x", 1}, {"z", 0}}},
		{ID: "C", Items: nil},
	}
	hasQty := func(n int) Matcher {
		return Satisfy(fmt.Sprintf("qty>=%d", n), func(v any) bool { i, ok := v.(qItem); return ok && i.Qty >= n })
	}
	everyItemShips := Satisfy("ships every item", func(v any) bool {
		o, ok := v.(qOrder)
		return ok && qMatch(EveryElement(hasQty(1)), o.Items)
	})
	matched, failure := Evaluate(EveryElement(everyItemShips), orders)
	if matched {
		t.Fatal("order B has a zero quantity")
	}
	if !strings.Contains(failure, "1 of 3 elements failed") || !strings.Contains(failure, "[1]") {
		t.Errorf("expected order index 1 as the only failure: %q", failure)
	}
	// Composing the matchers directly: the inner failure is the child's own message.
	child := Satisfy("ordered via EveryElement", func(v any) bool { return qMatch(EveryElement(hasQty(1)), v.(qOrder).Items) })
	if got := ExactlyNElements(2, child); !got.Match(orders) {
		t.Error("orders A and C ship every item (C vacuously)")
	}
}

func TestQuantifiedDescriptions(t *testing.T) {
	p := Equal(1)
	for want, m := range map[string]Matcher{
		"every element is equal to 1":           EveryElement(p),
		"some element is equal to 1":            AnyElement(p),
		"no element is equal to 1":              NoElement(p),
		"exactly 2 elements are equal to 1":     ExactlyNElements(2, p),
		"at least 2 elements are equal to 1":    AtLeastNElements(2, p),
		"at most 2 elements are equal to 1":     AtMostNElements(2, p),
		"every element is <nil>":                EveryElement(nil),
		"exactly 1 element is equal to 1":       ExactlyNElements(1, p),
		"elements in order [equal to 1, <nil>]": HaveElementsInOrder(p, nil),
		"containing in order [equal to 1]":      ContainElementsInOrder(p),
	} {
		d, ok := m.(Describer)
		if !ok {
			t.Fatalf("%T is not a Describer", m)
		}
		if got := d.Description(); got != want {
			t.Errorf("Description = %q, want %q", got, want)
		}
	}
}

func TestQuantifiedFailureMessageMatchesEvaluate(t *testing.T) {
	m := EveryElement(Equal(1))
	_, evaluated := Evaluate(m, []int{1, 2})
	if got := m.FailureMessage([]int{1, 2}); got != evaluated {
		t.Errorf("FailureMessage = %q, Evaluate = %q", got, evaluated)
	}
	if got := m.FailureMessage([]int{1}); !strings.Contains(got, "matched") {
		t.Errorf("FailureMessage on a passing input should say so, got %q", got)
	}
}

func TestHaveElementsInOrderExactSequence(t *testing.T) {
	positive := Satisfy("positive", isPositive)
	cases := []struct {
		name    string
		ms      []Matcher
		actual  any
		matches bool
		want    []string
	}{
		{"exact", []Matcher{Equal(1), Equal(2)}, []int{1, 2}, true, nil},
		{"wrong order", []Matcher{Equal(1), Equal(2)}, []int{2, 1}, false, []string{"HaveElementsInOrder: 2 of 2 positions failed", "[0] 2:", "[1] 1:"}},
		{"extra element", []Matcher{Equal(1)}, []int{1, 2}, false, []string{"expected 1 element, got 2", "unexpected [1] 2"}},
		{"missing element", []Matcher{Equal(1), Equal(2)}, []int{1}, false, []string{"expected 2 elements, got 1", "missing matcher #2"}},
		{"empty both", nil, []int{}, true, nil},
		{"empty matchers non-empty actual", nil, []int{1}, false, []string{"expected 0 elements, got 1"}},
		{"empty actual", []Matcher{positive}, []int(nil), false, []string{"expected 1 element, got 0"}},
		{"one failing position", []Matcher{positive, positive, Equal(9)}, []int{1, 2, 3}, false, []string{"1 of 3 positions failed", "[2] 3:"}},
		{"duplicates positional", []Matcher{Equal(7), Equal(7)}, []int{7, 7}, true, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := HaveElementsInOrder(tc.ms...)
			matched, failure := Evaluate(m, tc.actual)
			if matched != tc.matches || m.Match(tc.actual) != tc.matches {
				t.Fatalf("matched = %v, want %v (%q)", matched, tc.matches, failure)
			}
			for _, w := range tc.want {
				if !strings.Contains(failure, w) {
					t.Errorf("failure %q does not contain %q", failure, w)
				}
			}
		})
	}
	if _, f := Evaluate(HaveElementsInOrder(Equal(1)), 42); f != "HaveElementsInOrder: int is not a slice or array" {
		t.Errorf("unsupported failure = %q", f)
	}
	if _, f := Evaluate(HaveElementsInOrder(Equal(1), nil), []int{1, 2}); !strings.Contains(f, "#2: "+nilMatcherFailure) {
		t.Errorf("nil entry failure = %q", f)
	}
}

func TestContainElementsInOrderSubsequence(t *testing.T) {
	cases := []struct {
		name    string
		ms      []Matcher
		actual  any
		matches bool
		want    []string
	}{
		{"contiguous", []Matcher{Equal(1), Equal(2)}, []int{1, 2, 3}, true, nil},
		{"gaps allowed", []Matcher{Equal(1), Equal(3)}, []int{1, 2, 3}, true, nil},
		{"order matters", []Matcher{Equal(3), Equal(1)}, []int{1, 2, 3}, false, []string{"matcher #2 (equal to 1) matched no element after index 2"}},
		{"first never found", []Matcher{Equal(9)}, []int{1, 2}, false, []string{"matcher #1 (equal to 9) matched no element from index 0", "[0] 1:", "[1] 2:"}},
		{"each element used once", []Matcher{Equal(1), Equal(1)}, []int{1}, false, []string{"matcher #2"}},
		{"duplicates in actual", []Matcher{Equal(1), Equal(1)}, []int{1, 0, 1}, true, nil},
		{"empty matchers matches anything", nil, []int{1}, true, nil},
		{"empty matchers empty actual", nil, []int{}, true, nil},
		{"empty actual", []Matcher{Equal(1)}, []int{}, false, []string{"matcher #1 (equal to 1) matched no element from index 0"}},
		{"greedy is exact for predicates", []Matcher{Satisfy("positive", isPositive), Equal(5)}, []int{2, 5}, true, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := ContainElementsInOrder(tc.ms...)
			matched, failure := Evaluate(m, tc.actual)
			if matched != tc.matches || m.Match(tc.actual) != tc.matches {
				t.Fatalf("matched = %v, want %v (%q)", matched, tc.matches, failure)
			}
			for _, w := range tc.want {
				if !strings.Contains(failure, w) {
					t.Errorf("failure %q does not contain %q", failure, w)
				}
			}
		})
	}
	if _, f := Evaluate(ContainElementsInOrder(Equal(1)), "abc"); f != "ContainElementsInOrder: string is not a slice or array" {
		t.Errorf("unsupported failure = %q", f)
	}
	c := newQCounting(func(v any) bool { return v == 2 })
	Evaluate(ContainElementsInOrder(c, c), []int{1, 2, 3, 4})
	if *c.calls > 4 {
		t.Errorf("%d child calls for 4 elements; each element may be asked once", *c.calls)
	}
}

func TestOrderedMatchersEvaluateEachPositionOnce(t *testing.T) {
	c := newQCounting(isPositive)
	Evaluate(HaveElementsInOrder(c, c, c), []int{1, -1, 2})
	if *c.calls != 3 {
		t.Errorf("HaveElementsInOrder: %d calls, want 3", *c.calls)
	}
	// Length mismatch still judges only the paired prefix, once each.
	*c.calls = 0
	Evaluate(HaveElementsInOrder(c, c), []int{1, 2, 3})
	if *c.calls != 2 {
		t.Errorf("HaveElementsInOrder on a longer actual: %d calls, want 2", *c.calls)
	}
}

func TestQuantifiedDoesNotMutateMatcherState(t *testing.T) {
	m := EveryElement(Equal(1))
	first, _ := Evaluate(m, []int{1, 2})
	second, _ := Evaluate(m, []int{1, 1})
	if first || !second {
		t.Errorf("a matcher must be reusable: got %v then %v", first, second)
	}
}
