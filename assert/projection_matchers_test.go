package assert

import (
	"strings"
	"testing"
)

// Project maps an actual to a field or derived value and applies an existing matcher to it (#362).
// These tests pin the pass/fail table, the exact message of every failure branch, the invalid
// inputs (wrong type, nil input, nil projection, nil child, panicking projection), composition
// with Not/All/Any and nested projections, and — by counting invocations — the single-pass
// guarantee: the projection and the child matcher run once per Evaluate.

type projItem struct {
	SKU      string
	Quantity int
}

type projOrder struct {
	Status string
	Items  []projItem
}

type projErr string

func (e projErr) Error() string { return string(e) }

// projSpy counts Match and FailureMessage calls and records what it was asked about.
type projSpy struct {
	matches  bool
	matched  int
	failed   int
	lastSeen any
}

func (s *projSpy) Match(actual any) bool {
	s.matched++
	s.lastSeen = actual
	return s.matches
}

func (s *projSpy) FailureMessage(actual any) string {
	s.failed++
	return "spy failed"
}

func (s *projSpy) Description() string { return "spy" }

func projStatus(o projOrder) string { return o.Status }

func TestProjectMatchTable(t *testing.T) {
	order := projOrder{Status: "paid"}
	cases := map[string]struct {
		m      Matcher
		actual any
		want   bool
	}{
		"child matches projected field":    {Project("Status", projStatus, Equal("paid")), order, true},
		"child rejects projected field":    {Project("Status", projStatus, Equal("open")), order, false},
		"wrong input type never matches":   {Project("Status", projStatus, Equal("paid")), "paid", false},
		"untyped nil never matches":        {Project("Status", projStatus, Equal("paid")), nil, false},
		"nil projection never matches":     {Project[projOrder, string]("Status", nil, Equal("paid")), order, false},
		"nil child never matches":          {Project("Status", projStatus, nil), order, false},
		"typed nil child never matches":    {Project("Status", projStatus, (*derefMatcher)(nil)), order, false},
		"panicking projection never match": {Project("Status", func(projOrder) string { panic("boom") }, Equal("paid")), order, false},
		"pointer input type":               {Project("Status", func(o *projOrder) string { return o.Status }, Equal("paid")), &order, true},
		"derived value":                    {Project("len(Items)", func(o projOrder) int { return len(o.Items) }, Equal(0)), order, true},
		"interface input type":             {Project("Error()", func(e error) string { return e.Error() }, Equal("x")), projErr("x"), true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := tc.m.Match(tc.actual); got != tc.want {
				t.Fatalf("Match = %v, want %v", got, tc.want)
			}
			gotEval, failure := Evaluate(tc.m, tc.actual)
			if gotEval != tc.want {
				t.Fatalf("Evaluate = %v, want %v", gotEval, tc.want)
			}
			if tc.want && failure != "" {
				t.Fatalf("a match reported failure %q", failure)
			}
			if !tc.want && failure == "" {
				t.Fatal("a mismatch reported an empty failure")
			}
		})
	}
}

func TestProjectFailureMessages(t *testing.T) {
	order := projOrder{Status: "open"}
	cases := map[string]struct {
		m      Matcher
		actual any
		want   string
	}{
		"child explanation under the field name": {
			Project("Status", projStatus, Equal("paid")), order,
			"Status: " + EqualFailureMessage("paid", "open"),
		},
		"wrong input type": {
			Project("Status", projStatus, Equal("paid")), "open",
			"Status: expected input of type assert.projOrder, got string",
		},
		"untyped nil input": {
			Project("Status", projStatus, Equal("paid")), nil,
			"Status: expected input of type assert.projOrder, got nil",
		},
		"nil projection": {
			Project[projOrder, string]("Status", nil, Equal("paid")), order,
			"Status: no projection function given",
		},
		"nil child": {
			Project("Status", projStatus, nil), order,
			"Status: " + nilMatcherFailure,
		},
		"typed nil child": {
			Project("Status", projStatus, (*derefMatcher)(nil)), order,
			"Status: " + nilMatcherFailure,
		},
		"panicking projection": {
			Project("Status", func(projOrder) string { panic("boom") }, Equal("paid")), order,
			"Status: projection panicked: boom",
		},
		"panic with an error value": {
			Project("Status", func(projOrder) string { panic(projErr("bad")) }, Equal("paid")), order,
			"Status: projection panicked: bad",
		},
		"empty name falls back": {
			Project("", projStatus, Equal("paid")), order,
			"projection: " + EqualFailureMessage("paid", "open"),
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, got := Evaluate(tc.m, tc.actual)
			if got != tc.want {
				t.Fatalf("Evaluate failure = %q, want %q", got, tc.want)
			}
			if fm := tc.m.FailureMessage(tc.actual); fm != tc.want {
				t.Fatalf("FailureMessage = %q, want %q", fm, tc.want)
			}
		})
	}
}

func TestProjectDescription(t *testing.T) {
	if got, want := Project("Status", projStatus, Equal("paid")).(Describer).Description(), "Status: equal to paid"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got, want := Project("Status", projStatus, nil).(Describer).Description(), "Status: <nil>"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestProjectEvaluateRunsProjectionAndChildOnce(t *testing.T) {
	for _, matches := range []bool{true, false} {
		spy := &projSpy{matches: matches}
		projected := 0
		m := Project("Status", func(o projOrder) string { projected++; return o.Status }, spy)

		gotMatched, _ := Evaluate(m, projOrder{Status: "open"})

		if gotMatched != matches {
			t.Fatalf("matches=%v: Evaluate = %v", matches, gotMatched)
		}
		if projected != 1 {
			t.Fatalf("matches=%v: projection ran %d times, want 1", matches, projected)
		}
		if spy.matched != 1 {
			t.Fatalf("matches=%v: child Match ran %d times, want 1", matches, spy.matched)
		}
		wantFailed := 0
		if !matches {
			wantFailed = 1
		}
		if spy.failed != wantFailed {
			t.Fatalf("matches=%v: child FailureMessage ran %d times, want %d", matches, spy.failed, wantFailed)
		}
		if spy.lastSeen != "open" {
			t.Fatalf("child saw %v, want the projected value", spy.lastSeen)
		}
	}
}

func TestProjectMatchRunsProjectionAndChildOnce(t *testing.T) {
	spy := &projSpy{matches: true}
	projected := 0
	m := Project("Status", func(o projOrder) string { projected++; return o.Status }, spy)
	if !m.Match(projOrder{Status: "open"}) {
		t.Fatal("expected a match")
	}
	if projected != 1 || spy.matched != 1 || spy.failed != 0 {
		t.Fatalf("projection=%d Match=%d FailureMessage=%d, want 1/1/0", projected, spy.matched, spy.failed)
	}
}

func TestProjectInvalidInputsNeverReachProjectionOrChild(t *testing.T) {
	spy := &projSpy{matches: true}
	projected := 0
	m := Project("Status", func(o projOrder) string { projected++; return o.Status }, spy)
	for _, actual := range []any{nil, "not an order", 42} {
		if matched, _ := Evaluate(m, actual); matched {
			t.Fatalf("%v matched", actual)
		}
	}
	if projected != 0 || spy.matched != 0 {
		t.Fatalf("projection ran %d times and child %d times, want 0/0", projected, spy.matched)
	}

	projected = 0
	nilChild := Project("Status", func(o projOrder) string { projected++; return o.Status }, nil)
	if matched, _ := Evaluate(nilChild, projOrder{}); matched || projected != 0 {
		t.Fatalf("a nil child matched=%v or ran the projection %d times", matched, projected)
	}
}

func TestProjectChildPanicPropagates(t *testing.T) {
	defer func() {
		if r := recover(); r != "child boom" {
			t.Fatalf("recovered %v, want the child's panic", r)
		}
	}()
	m := Project("Status", projStatus, Satisfy("explodes", func(any) bool { panic("child boom") }))
	Evaluate(m, projOrder{})
	t.Fatal("the child's panic was swallowed")
}

func TestProjectTypedNilInputIsProjected(t *testing.T) {
	m := Project("Status", func(o *projOrder) string { return o.Status }, Equal("x"))
	var nilOrder *projOrder
	_, failure := Evaluate(m, nilOrder)
	if !strings.HasPrefix(failure, "Status: projection panicked: ") {
		t.Fatalf("a typed nil reached the projection and its panic was not reported: %q", failure)
	}
}

func TestProjectComposesWithNotAllAny(t *testing.T) {
	order := projOrder{Status: "paid", Items: []projItem{{"a", 1}}}
	status := func(want string) Matcher { return Project("Status", projStatus, Equal(want)) }
	count := func(n int) Matcher {
		return Project("len(Items)", func(o projOrder) int { return len(o.Items) }, Equal(n))
	}

	if !Not(status("open")).Match(order) {
		t.Fatal("Not(Project) should match when the child does not")
	}
	if matched, failure := Evaluate(Not(status("paid")), order); matched || failure != "expected {paid [{a 1}]} not to be Status: equal to paid" {
		t.Fatalf("Not failure: matched=%v %q", matched, failure)
	}
	if !All(status("paid"), count(1)).Match(order) || All(status("paid"), count(2)).Match(order) {
		t.Fatal("All(Project...) truth table is wrong")
	}
	if !Any(status("open"), count(1)).Match(order) || Any(status("open"), count(2)).Match(order) {
		t.Fatal("Any(Project...) truth table is wrong")
	}

	_, failure := Evaluate(All(status("open"), count(2)), order)
	want := `All: #1: "Status: ` + EqualFailureMessage("open", "paid") + `"; #2: "len(Items): ` + EqualFailureMessage(2, 1) + `"`
	if failure != want {
		t.Fatalf("All failure = %q, want %q", failure, want)
	}
}

func TestProjectInsideCompositeRunsEachPartOnce(t *testing.T) {
	spy := &projSpy{matches: false}
	projected := 0
	m := All(Project("Status", func(o projOrder) string { projected++; return o.Status }, spy))
	Evaluate(m, projOrder{})
	if projected != 1 || spy.matched != 1 || spy.failed != 1 {
		t.Fatalf("projection=%d Match=%d FailureMessage=%d, want 1/1/1 (FailureMessage once, to build the explanation)", projected, spy.matched, spy.failed)
	}
}

func TestProjectNestedPathsJoin(t *testing.T) {
	first := func(o projOrder) projItem { return o.Items[0] }
	m := Project("Items[0]", first, Project("Quantity", func(i projItem) int { return i.Quantity }, Equal(2)))
	order := projOrder{Items: []projItem{{"a", 1}}}

	_, failure := Evaluate(m, order)
	if want := "Items[0].Quantity: " + EqualFailureMessage(2, 1); failure != want {
		t.Fatalf("got %q, want %q", failure, want)
	}
	if !m.Match(projOrder{Items: []projItem{{"a", 2}}}) {
		t.Fatal("nested projection should match")
	}

	index := Project("Items", func(o projOrder) []projItem { return o.Items },
		Project("[0]", func(s []projItem) projItem { return s[0] },
			Project("SKU", func(i projItem) string { return i.SKU }, Equal("z"))))
	_, failure = Evaluate(index, order)
	if want := "Items[0].SKU: " + EqualFailureMessage("z", "a"); failure != want {
		t.Fatalf("got %q, want %q", failure, want)
	}
}

func TestProjectNestedInvalidInnerReportsFullPath(t *testing.T) {
	m := Project("Items[0]", func(o projOrder) any { return "not an item" },
		Project("Quantity", func(i projItem) int { return i.Quantity }, Equal(1)))
	_, failure := Evaluate(m, projOrder{})
	if want := "Items[0].Quantity: expected input of type assert.projItem, got string"; failure != want {
		t.Fatalf("got %q, want %q", failure, want)
	}
}

func TestProjectInterfaceValuedProjectionPassesNilToChild(t *testing.T) {
	m := Project("Err", func(o projOrder) error { return nil }, BeNil())
	if !m.Match(projOrder{}) {
		t.Fatal("a nil interface projection should reach BeNil as nil")
	}
}

func TestProjectIsImmutableAcrossCalls(t *testing.T) {
	m := Project("Status", projStatus, Equal("paid"))
	for i := 0; i < 3; i++ {
		if !m.Match(projOrder{Status: "paid"}) || m.Match(projOrder{Status: "open"}) {
			t.Fatal("verdict changed between calls")
		}
	}
}
