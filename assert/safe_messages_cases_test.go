package assert

import (
	"errors"
	"fmt"
	"time"
)

type compatPoint struct {
	X, Y int
}

type compatNamedErr struct{ code int }

func (e *compatNamedErr) Error() string { return fmt.Sprintf("named-%d", e.code) }

// compatCase is one representative matcher with an ordinary (acyclic, small) actual. The text it
// produces is pinned by TestOrdinaryMatcherMessagesKeepTheirText, so routing every diagnostic through
// the safe renderer cannot change how an ordinary failure reads.
type compatCase struct {
	name   string
	m      Matcher
	actual any
}

func compatCases() []compatCase {
	pt := &compatPoint{1, 2}
	return []compatCase{
		{"Equal/int", Equal(1), 2},
		{"Equal/struct", Equal(compatPoint{1, 2}), compatPoint{1, 3}},
		{"Equal/map", Equal(map[string]int{"a": 1}), map[string]int{"a": 2}},
		{"Equal/error", Equal(errors.New("a")), errors.New("b")},
		{"Equal/duration", Equal(time.Second), 2 * time.Second},
		{"NotEqual", NotEqual([]int{1, 2}), []int{1, 2}},
		{"BeNil", BeNil(), pt},
		{"BeTrue", BeTrue(), "no"},
		{"BeFalse", BeFalse(), 3},
		{"Contain/slice", Contain(9), []int{1, 2, 3}},
		{"Contain/string", Contain("zz"), "abc"},
		{"Contain/typeMismatch", Contain("x"), []int{1}},
		{"Contain/unsupported", Contain(1), map[string]int{"a": 1}},
		{"HaveLen", HaveLen(3), []string{"a"}},
		{"HaveLen/noLength", HaveLen(3), 5},
		{"BeEmpty", BeEmpty(), map[string]int{"a": 1}},
		{"HaveKey", HaveKey("b"), map[string]int{"a": 1}},
		{"HaveKey/wrongType", HaveKey(1), map[string]int{"a": 1}},
		{"HaveValue", HaveValue(9), map[string]int{"a": 1}},
		{"HavePair/missing", HavePair("b", 1), map[string]int{"a": 1}},
		{"HavePair/other", HavePair("a", 2), map[string]int{"a": 1}},
		{"ContainAllOf", ContainAllOf(1, 5), []int{1, 2}},
		{"ContainAnyOf", ContainAnyOf(7, 8), []int{1, 2}},
		{"ContainTheSameElementsAs", ContainTheSameElementsAs([]int{1, 2, 3}), []int{3, 4}},
		{"BeOneOf", BeOneOf("a", "b"), "c"},
		{"BeGreaterThan", BeGreaterThan(5), 3},
		{"BeBetween", BeBetween(1, 3), 9},
		{"BeCloseTo", BeCloseTo(10, 0.5), 12.25},
		{"BeZero", BeZero(), compatPoint{1, 0}},
		{"Satisfy", Satisfy("is even", func(any) bool { return false }), []int{1}},
		{"MatchError", MatchError(errors.New("boom")), errors.New("bang")},
		{"MatchError/nonError", MatchError(errors.New("boom")), 5},
		{"MatchErrorAs", MatchErrorAs(new(*compatNamedErr)), errors.New("plain")},
		{"StartWith", StartWith("x"), "abc"},
		{"MatchRegex", MatchRegex("^z"), "abc"},
		{"Not(Equal)", Not(Equal(1)), 1},
		{"Not(HaveLen)", Not(HaveLen(1)), []int{1}},
		{"All", All(Equal(1), HaveLen(2)), 1},
		{"Any", Any(Equal(1), Contain("x")), 2},
		{"Not(Any(Equal, BeOneOf))", Not(Any(Equal(3), BeOneOf(3, 4))), 3},
	}
}
