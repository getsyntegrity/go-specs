package assert

import (
	"fmt"
	"reflect"
	"strings"
)

// The quantified and ordered collection matchers judge the elements of a slice or array with child
// matchers instead of comparing them with values, which is what ContainAllOf and friends do. They
// answer questions such as "does every order have an item?", "are exactly two of the items
// shipped?" and "do the events arrive in this order?", and their failure messages name the actual
// elements (by index) that broke the expectation.
//
// Semantics, shared by every matcher in this file:
//
//   - Supported actual values are slices and arrays of any element type. A nil slice is an empty
//     collection. Anything else (untyped nil, a string, a map, a pointer to a slice, a number) never
//     matches and FailureMessage says so, exactly like the other collection matchers; as with
//     HaveLen, Not(EveryElement(m)) therefore succeeds on an unsupported value.
//   - Empty input follows logic: EveryElement and NoElement are vacuously true, AnyElement is
//     false, the counting matchers compare 0 with their count (ExactlyNElements(0, m) and
//     AtLeastNElements(0, m) hold, AtMostNElements(n, m) holds for every n >= 0), and
//     ContainElementsInOrder with no matchers holds for any collection.
//   - A nil child matcher (untyped or typed) never matches, even on an empty collection, and is
//     never called: hiding that mistake behind vacuous truth would let it ship green. A negative
//     count never matches either and the message says why.
//   - Duplicates are ordinary elements. Each element is judged on its own, so a value that appears
//     three times counts three times. The ordered matchers consume each element at most once.
//
// Single pass: every matcher implements Evaluator and asks the child matcher about each element at
// most once per evaluation, through the package-level Evaluate, so a child with a side effect (like
// MatchErrorAs) is not driven twice to build the message, and a child that is itself a composite
// stays single-pass too. A verdict that can already be decided as a success stops early (AnyElement at
// its first match, AtLeastNElements at its N-th); a failure always judges every element, because the
// message names every culprit. Match and FailureMessage, which the public Matcher interface
// requires, are implemented on top of Evaluate; the DSL calls Evaluate, see composite_matchers.go.

// quantifiedListLimit bounds how many elements a failure message lists; the rest is summarised as a
// count, so the message of a failing 100,000 element collection stays small.
const quantifiedListLimit = 10

type quantifier int

const (
	quantEvery quantifier = iota
	quantAny
	quantNone
	quantExactly
	quantAtLeast
	quantAtMost
)

// EveryElement returns a matcher that expects every element of actual (a slice or array) to match
// m. An empty or nil collection matches vacuously. The failure message lists each failing element
// by index with m's own failure message.
func EveryElement(m Matcher) Matcher {
	return &quantifiedMatcher{name: "EveryElement", kind: quantEvery, sub: m}
}

// AnyElement returns a matcher that expects at least one element of actual to match m. An empty or
// nil collection never matches.
func AnyElement(m Matcher) Matcher {
	return &quantifiedMatcher{name: "AnyElement", kind: quantAny, sub: m}
}

// NoElement returns a matcher that expects no element of actual to match m. An empty or nil
// collection matches vacuously. The failure message lists the matching elements by index.
func NoElement(m Matcher) Matcher {
	return &quantifiedMatcher{name: "NoElement", kind: quantNone, sub: m}
}

// ExactlyNElements returns a matcher that expects exactly n elements of actual to match m. Elements
// count once each, duplicates included. A negative n never matches.
func ExactlyNElements(n int, m Matcher) Matcher {
	return &quantifiedMatcher{name: "ExactlyNElements", kind: quantExactly, n: n, sub: m}
}

// AtLeastNElements returns a matcher that expects at least n elements of actual to match m.
// AtLeastNElements(0, m) holds for every supported collection. A negative n never matches.
func AtLeastNElements(n int, m Matcher) Matcher {
	return &quantifiedMatcher{name: "AtLeastNElements", kind: quantAtLeast, n: n, sub: m}
}

// AtMostNElements returns a matcher that expects at most n elements of actual to match m.
// AtMostNElements(0, m) is NoElement(m). A negative n never matches.
func AtMostNElements(n int, m Matcher) Matcher {
	return &quantifiedMatcher{name: "AtMostNElements", kind: quantAtMost, n: n, sub: m}
}

type quantifiedMatcher struct {
	name string
	kind quantifier
	n    int
	sub  Matcher
}

func (q *quantifiedMatcher) Match(actual any) bool {
	matched, _ := q.Evaluate(actual)
	return matched
}

// FailureMessage re-evaluates the collection, like every composite's FailureMessage does (see the
// header of composite_matchers.go); the DSL goes through Evaluate and never calls it.
func (q *quantifiedMatcher) FailureMessage(actual any) string {
	matched, failure := q.Evaluate(actual)
	if matched {
		return q.name + ": the collection matched, there is no failure to report"
	}
	return failure
}

func (q *quantifiedMatcher) Description() string {
	sub := describeMatcher(q.sub)
	switch q.kind {
	case quantEvery:
		return "every element is " + sub
	case quantAny:
		return "some element is " + sub
	case quantNone:
		return "no element is " + sub
	}
	qualifier := map[quantifier]string{quantExactly: "exactly", quantAtLeast: "at least", quantAtMost: "at most"}[q.kind]
	noun, verb := "elements", "are"
	if q.n == 1 {
		noun, verb = "element", "is"
	}
	return fmt.Sprintf("%s %d %s %s %s", qualifier, q.n, noun, verb, sub)
}

// Evaluate implements Evaluator; see the header of this file for the evaluation contract.
func (q *quantifiedMatcher) Evaluate(actual any) (bool, string) {
	if isNilMatcher(q.sub) {
		return false, q.name + ": " + nilMatcherFailure
	}
	counts := q.kind == quantExactly || q.kind == quantAtLeast || q.kind == quantAtMost
	if counts && q.n < 0 {
		return false, fmt.Sprintf("%s: count must not be negative, got %d", q.name, q.n)
	}
	list, ok := newElementList(actual)
	if !ok {
		return false, notListMessage(q.name, actual)
	}
	if q.kind == quantAtLeast && q.n == 0 {
		return true, ""
	}

	total := list.len()
	var small [16]int
	hits := small[:0] // indices of matching elements, only tracked where the message needs them
	trackHits := q.kind == quantNone || counts
	var misses elementListing // failing elements with the child's own message
	trackMisses := q.kind == quantEvery || q.kind == quantAny || q.kind == quantExactly || q.kind == quantAtLeast
	matchedCount := 0
	for i := 0; i < total; i++ {
		element := list.at(i)
		matched, failure := Evaluate(q.sub, element)
		if !matched {
			if trackMisses {
				misses.add(func() string { return fmt.Sprintf("[%d] %s: %q", i, userValue(element), failure) })
			}
			continue
		}
		matchedCount++
		if trackHits {
			hits = append(hits, i)
		}
		if q.kind == quantAny || (q.kind == quantAtLeast && matchedCount >= q.n) {
			return true, ""
		}
	}

	switch q.kind {
	case quantEvery:
		if misses.count() == 0 {
			return true, ""
		}
		return false, fmt.Sprintf("%s: %d of %d elements failed — %s", q.name, misses.count(), total, misses.String())
	case quantAny:
		if total == 0 {
			return false, q.name + ": the collection is empty, so no element could match"
		}
		return false, fmt.Sprintf("%s: none of %d elements matched — %s", q.name, total, misses.String())
	case quantNone:
		if matchedCount == 0 {
			return true, ""
		}
		var matching elementListing
		for _, i := range hits {
			matching.add(func() string { return fmt.Sprintf("[%d] %s", i, userValue(list.at(i))) })
		}
		return false, fmt.Sprintf("%s: %d of %d elements matched — %s", q.name, matchedCount, total, matching.String())
	}

	// The counting matchers. AtLeast only reaches this point when it fell short.
	var qualifier string
	switch q.kind {
	case quantExactly:
		if matchedCount == q.n {
			return true, ""
		}
		qualifier = "exactly"
	case quantAtLeast:
		qualifier = "at least"
	case quantAtMost:
		if matchedCount <= q.n {
			return true, ""
		}
		qualifier = "at most"
	}
	message := fmt.Sprintf("%s: expected %s %d of %d elements to match, %d did", q.name, qualifier, q.n, total, matchedCount)
	var details []string
	if matchedCount > 0 {
		details = append(details, "matching indices "+indicesString(hits))
	}
	if matchedCount < q.n && misses.count() > 0 {
		details = append(details, misses.String())
	}
	if len(details) > 0 {
		message += " — " + strings.Join(details, "; ")
	}
	return false, message
}

// HaveElementsInOrder returns a matcher that expects actual (a slice or array) to be exactly the
// sequence ms: the same length, and the element at position i matches ms[i]. It is an exact
// sequence, so an extra or missing element fails; use ContainElementsInOrder for a subsequence.
// With no matchers it matches only an empty or nil collection. A nil entry never matches and
// nothing is evaluated. On a length mismatch the paired prefix is still judged, once per position,
// so the message reports wrong elements and the unexpected or missing tail together.
func HaveElementsInOrder(ms ...Matcher) Matcher {
	return &orderedMatcher{name: "HaveElementsInOrder", exact: true, ms: append([]Matcher(nil), ms...)}
}

// ContainElementsInOrder returns a matcher that expects the elements of actual (a slice or array)
// to contain ms as a subsequence: there are positions p0 < p1 < ... such that the element at pk
// matches ms[k], with anything, or nothing, between them. Each element is consumed by at most one
// matcher, so ContainElementsInOrder(m, m) needs two matching elements, and matchers are consumed
// greedily from the left, which is exact because each matcher judges one element on its own. With
// no matchers it matches every supported collection. A nil entry never matches and nothing is
// evaluated.
func ContainElementsInOrder(ms ...Matcher) Matcher {
	return &orderedMatcher{name: "ContainElementsInOrder", ms: append([]Matcher(nil), ms...)}
}

type orderedMatcher struct {
	name  string
	exact bool
	ms    []Matcher
}

func (o *orderedMatcher) Match(actual any) bool {
	matched, _ := o.Evaluate(actual)
	return matched
}

// FailureMessage re-evaluates; see quantifiedMatcher.FailureMessage.
func (o *orderedMatcher) FailureMessage(actual any) string {
	matched, failure := o.Evaluate(actual)
	if matched {
		return o.name + ": the collection matched, there is no failure to report"
	}
	return failure
}

func (o *orderedMatcher) Description() string {
	prefix := "containing in order "
	if o.exact {
		prefix = "elements in order "
	}
	return prefix + "[" + strings.Join(describeEntries(o.ms), ", ") + "]"
}

// Evaluate implements Evaluator. Nil entries are reported first and stop the evaluation, like Any:
// a matcher that can never match makes the verdict, so asking the others would buy nothing.
func (o *orderedMatcher) Evaluate(actual any) (bool, string) {
	var nils []string
	for i, m := range o.ms {
		if isNilMatcher(m) {
			nils = append(nils, fmt.Sprintf("#%d: %s", i+1, nilMatcherFailure))
		}
	}
	if len(nils) > 0 {
		return false, o.name + ": " + strings.Join(nils, "; ")
	}
	list, ok := newElementList(actual)
	if !ok {
		return false, notListMessage(o.name, actual)
	}
	if o.exact {
		return o.evaluateExact(list)
	}
	return o.evaluateSubsequence(list)
}

func (o *orderedMatcher) evaluateExact(list elementList) (bool, string) {
	total, want := list.len(), len(o.ms)
	paired := min(total, want)
	var failures elementListing
	for i := 0; i < paired; i++ {
		element := list.at(i)
		matched, failure := Evaluate(o.ms[i], element)
		if !matched {
			failures.add(func() string { return fmt.Sprintf("[%d] %s: %q", i, userValue(element), failure) })
		}
	}
	if total == want && failures.count() == 0 {
		return true, ""
	}

	var parts []string
	switch {
	case total > want:
		var extra elementListing
		for i := want; i < total; i++ {
			extra.add(func() string { return fmt.Sprintf("[%d] %s", i, userValue(list.at(i))) })
		}
		parts = append(parts, fmt.Sprintf("expected %s, got %d — unexpected %s", countOf(want, "element"), total, extra.joined(", ")))
	case total < want:
		var missing elementListing
		for i := total; i < want; i++ {
			missing.add(func() string { return fmt.Sprintf("#%d (%s)", i+1, describeMatcher(o.ms[i])) })
		}
		label := "matcher"
		if want-total > 1 {
			label = "matchers"
		}
		parts = append(parts, fmt.Sprintf("expected %s, got %d — missing %s %s", countOf(want, "element"), total, label, missing.joined(", ")))
	}
	if failures.count() > 0 {
		parts = append(parts, fmt.Sprintf("%d of %d positions failed — %s", failures.count(), paired, failures.String()))
	}
	return false, o.name + ": " + strings.Join(parts, "; ")
}

func (o *orderedMatcher) evaluateSubsequence(list elementList) (bool, string) {
	total := list.len()
	next := 0                 // index in o.ms of the matcher currently looking for its element
	lastMatch := -1           // position of the element the previous matcher consumed
	var misses elementListing // elements the current matcher rejected, reset when it matches
	for i := 0; i < total && next < len(o.ms); i++ {
		element := list.at(i)
		matched, failure := Evaluate(o.ms[next], element)
		if matched {
			next++
			lastMatch = i
			misses = elementListing{}
			continue
		}
		misses.add(func() string { return fmt.Sprintf("[%d] %s: %q", i, userValue(element), failure) })
	}
	if next == len(o.ms) {
		return true, ""
	}
	where := "from index 0"
	if lastMatch >= 0 {
		where = fmt.Sprintf("after index %d", lastMatch)
	}
	message := fmt.Sprintf("%s: matched %d of %d matchers in order; matcher #%d (%s) matched no element %s",
		o.name, next, len(o.ms), next+1, describeMatcher(o.ms[next]), where)
	if misses.count() > 0 {
		message += " — " + misses.String()
	}
	return false, message
}

// elementList gives the quantified matchers indexed, read-only access to a slice or array without
// allocating for the common []any case; everything else goes through reflection, like the other
// collection matchers. A nil slice is an empty list.
type elementList struct {
	anys []any
	rv   reflect.Value
	n    int
}

func newElementList(actual any) (elementList, bool) {
	if a, ok := actual.([]any); ok {
		return elementList{anys: a, n: len(a)}, true
	}
	rv := reflect.ValueOf(actual)
	if !isList(rv) {
		return elementList{}, false
	}
	return elementList{rv: rv, n: rv.Len()}, true
}

func (l elementList) len() int { return l.n }

func (l elementList) at(i int) any {
	if l.anys != nil {
		return l.anys[i]
	}
	return l.rv.Index(i).Interface()
}

func notListMessage(matcher string, actual any) string {
	return notCollectionMessage(matcher, actual, "a slice or array")
}

// elementListing collects at most quantifiedListLimit rendered entries for a message and counts the
// rest, so a huge failing collection costs neither time nor message length beyond the limit. An
// entry is rendered by a closure that only runs while there is room.
type elementListing struct {
	parts []string
	extra int
}

func (l *elementListing) add(render func() string) {
	if len(l.parts) >= quantifiedListLimit {
		l.extra++
		return
	}
	l.parts = append(l.parts, render())
}

func (l *elementListing) count() int { return len(l.parts) + l.extra }

func (l *elementListing) joined(sep string) string {
	s := strings.Join(l.parts, sep)
	if l.extra > 0 {
		s += fmt.Sprintf("%s... %d more not shown", sep, l.extra)
	}
	return s
}

func (l *elementListing) String() string { return l.joined("; ") }

// indicesString renders element positions as "[0 2 5]", bounded like elementListing.
func indicesString(idx []int) string {
	shown := idx
	if len(shown) > quantifiedListLimit {
		shown = shown[:quantifiedListLimit]
	}
	s := fmt.Sprint(shown)
	if extra := len(idx) - len(shown); extra > 0 {
		s = s[:len(s)-1] + fmt.Sprintf(" ... %d more not shown]", extra)
	}
	return s
}

func countOf(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
