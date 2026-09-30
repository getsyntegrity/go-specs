package assert

import (
	"fmt"
	"reflect"
	"strings"
)

// The collection matchers compare elements exactly like Contain does: through ValuesEqual, so an
// int 1 never equals an int64 1, an error matches by identity and everything else is structural.
// A collection is a slice or an array; ContainAllOf and ContainAnyOf also accept a string, where
// each element is a substring (a non-string element is then simply never found), mirroring
// Contain. []int, []string, []float64 and []any take allocation-free fast paths, everything else
// goes through reflection. An actual that is not a collection never matches, and FailureMessage
// says so instead of reporting missing elements.

// ContainAllOf returns a matcher that expects actual (a slice, array or string) to contain every one
// of elems, in any order. Called with no elements it matches any supported collection, since
// nothing is required of it.
func ContainAllOf(elems ...any) Matcher {
	return &containAllOfMatcher{elems: append([]any(nil), elems...)}
}

type containAllOfMatcher struct {
	elems []any
}

func (m *containAllOfMatcher) Match(actual any) bool {
	for _, e := range m.elems {
		found, ok := containsElement(actual, e)
		if !ok || !found {
			return false
		}
	}
	// With no elements the loop never asks whether actual is a collection at all.
	return len(m.elems) > 0 || isCollectionOrString(actual)
}

func (m *containAllOfMatcher) FailureMessage(actual any) string {
	if !isCollectionOrString(actual) {
		return notCollectionMessage("ContainAllOf", actual, "a string, slice or array")
	}
	var missing []any
	for _, e := range m.elems {
		if found, _ := containsElement(actual, e); !found {
			missing = append(missing, e)
		}
	}
	return fmt.Sprintf("expected %s to contain all of %s, missing %s", userValue(actual), userValue(m.elems), userValue(missing))
}

// Description implements Describer; see equalMatcher.Description.
func (m *containAllOfMatcher) Description() string {
	return fmt.Sprintf("containing all of %s", userValue(m.elems))
}

// ContainAnyOf returns a matcher that expects actual (a slice, array or string) to contain at least
// one of elems. Called with no elements it never matches: there is nothing it could contain.
func ContainAnyOf(elems ...any) Matcher {
	return &containAnyOfMatcher{elems: append([]any(nil), elems...)}
}

type containAnyOfMatcher struct {
	elems []any
}

func (m *containAnyOfMatcher) Match(actual any) bool {
	for _, e := range m.elems {
		if found, _ := containsElement(actual, e); found {
			return true
		}
	}
	return false
}

func (m *containAnyOfMatcher) FailureMessage(actual any) string {
	if !isCollectionOrString(actual) {
		return notCollectionMessage("ContainAnyOf", actual, "a string, slice or array")
	}
	return fmt.Sprintf("expected %s to contain any of %s", userValue(actual), userValue(m.elems))
}

// Description implements Describer; see equalMatcher.Description.
func (m *containAnyOfMatcher) Description() string {
	return fmt.Sprintf("containing any of %s", userValue(m.elems))
}

// ContainTheSameElementsAs returns a matcher that expects actual (a slice or array) to hold the
// same elements as expected (a slice or array), in any order. It is multiset equality: duplicates
// count, so [1 1 2] and [1 2 2] differ. The two sides may have different types ([]int against
// []any is fine) as long as their elements are ValuesEqual. Elements are paired greedily, one to
// one, which is exact for equality that is an equivalence relation and best-effort for an unusual
// one such as errors matching by wrapping. A string actual is not supported here. An expected value
// that is not a slice or array never matches.
func ContainTheSameElementsAs(expected any) Matcher {
	return &sameElementsMatcher{expected: expected}
}

type sameElementsMatcher struct {
	expected any
}

func (m *sameElementsMatcher) Match(actual any) bool {
	switch a := actual.(type) {
	case []int:
		if e, ok := m.expected.([]int); ok {
			return sameElements(a, e, func(x, y int) bool { return x == y })
		}
	case []string:
		if e, ok := m.expected.([]string); ok {
			return sameElements(a, e, func(x, y string) bool { return x == y })
		}
	case []any:
		if e, ok := m.expected.([]any); ok {
			return sameElements(a, e, func(x, y any) bool { return ValuesEqual(y, x) })
		}
	}
	actualList, expectedList, ok := m.lists(actual)
	if !ok || actualList.Len() != expectedList.Len() {
		return false
	}
	unexpected, _ := elementDiff(actualList, expectedList)
	return len(unexpected) == 0
}

func (m *sameElementsMatcher) FailureMessage(actual any) string {
	actualList, expectedList, ok := m.lists(actual)
	if !ok {
		if !isList(reflect.ValueOf(m.expected)) {
			return fmt.Sprintf("ContainTheSameElementsAs: expected value must be a slice or array, got %T", m.expected)
		}
		return notCollectionMessage("ContainTheSameElementsAs", actual, "a slice or array")
	}
	unexpected, missing := elementDiff(actualList, expectedList)
	var parts []string
	if len(missing) > 0 {
		parts = append(parts, fmt.Sprintf("missing %s", userValue(missing)))
	}
	if len(unexpected) > 0 {
		parts = append(parts, fmt.Sprintf("unexpected %s", userValue(unexpected)))
	}
	message := fmt.Sprintf("expected %s to contain the same elements as %s", userValue(actual), userValue(m.expected))
	if len(parts) > 0 {
		message += " — " + strings.Join(parts, ", ")
	}
	return message
}

// Description implements Describer; see equalMatcher.Description.
func (m *sameElementsMatcher) Description() string {
	return fmt.Sprintf("containing the same elements as %s", userValue(m.expected))
}

func (m *sameElementsMatcher) lists(actual any) (actualList, expectedList reflect.Value, ok bool) {
	actualList, expectedList = reflect.ValueOf(actual), reflect.ValueOf(m.expected)
	return actualList, expectedList, isList(actualList) && isList(expectedList)
}

// BeOneOf returns a matcher that expects actual to equal (ValuesEqual) at least one of values.
// Called with no values it never matches.
func BeOneOf(values ...any) Matcher {
	return &beOneOfMatcher{values: append([]any(nil), values...)}
}

type beOneOfMatcher struct {
	values []any
}

func (m *beOneOfMatcher) Match(actual any) bool {
	for _, v := range m.values {
		if ValuesEqual(v, actual) {
			return true
		}
	}
	return false
}

func (m *beOneOfMatcher) FailureMessage(actual any) string {
	return fmt.Sprintf("expected %s to be one of %s", userValue(actual), userValue(m.values))
}

// Description implements Describer; see equalMatcher.Description.
func (m *beOneOfMatcher) Description() string {
	return fmt.Sprintf("one of %s", userValue(m.values))
}

func notCollectionMessage(matcher string, actual any, want string) string {
	return fmt.Sprintf("%s: %T is not %s", matcher, actual, want)
}

func isList(rv reflect.Value) bool {
	return rv.IsValid() && (rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array)
}

func isCollectionOrString(actual any) bool {
	switch actual.(type) {
	case string, []int, []string, []float64, []any:
		return true
	}
	rv := reflect.ValueOf(actual)
	return isList(rv) || (rv.IsValid() && rv.Kind() == reflect.String)
}

// containsElement reports whether the collection actual holds e, and whether actual is a collection
// this package supports at all (a string, slice or array).
func containsElement(actual, e any) (found, ok bool) {
	switch a := actual.(type) {
	case string:
		needle, isString := e.(string)
		return isString && strings.Contains(a, needle), true
	case []int:
		return containsComparable(a, e), true
	case []string:
		return containsComparable(a, e), true
	case []float64:
		return containsComparable(a, e), true
	case []any:
		for _, v := range a {
			if ValuesEqual(e, v) {
				return true, true
			}
		}
		return false, true
	}
	rv := reflect.ValueOf(actual)
	switch {
	case isList(rv):
		for i := 0; i < rv.Len(); i++ {
			if ValuesEqual(e, rv.Index(i).Interface()) {
				return true, true
			}
		}
		return false, true
	case rv.IsValid() && rv.Kind() == reflect.String:
		needle, isString := e.(string)
		return isString && strings.Contains(rv.String(), needle), true
	}
	return false, false
}

func containsComparable[T comparable](s []T, e any) bool {
	want, ok := e.(T)
	if !ok {
		return false
	}
	for _, v := range s {
		if v == want {
			return true
		}
	}
	return false
}

// sameElements is the typed fast path of multiset equality: each actual element claims one unused
// expected element. Up to 64 expected elements are tracked on the stack, so the common case does not
// allocate.
func sameElements[T any](actual, expected []T, eq func(a, e T) bool) bool {
	if len(actual) != len(expected) {
		return false
	}
	var small [64]bool
	var used []bool
	if len(expected) <= len(small) {
		used = small[:len(expected)]
	} else {
		used = make([]bool, len(expected))
	}
	for _, a := range actual {
		matched := false
		for j, e := range expected {
			if !used[j] && eq(a, e) {
				used[j], matched = true, true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// elementDiff pairs the elements of two lists greedily and returns the actual elements with no
// partner (unexpected) and the expected elements nobody claimed (missing), in list order.
func elementDiff(actual, expected reflect.Value) (unexpected, missing []any) {
	used := make([]bool, expected.Len())
	for i := 0; i < actual.Len(); i++ {
		a := actual.Index(i).Interface()
		matched := false
		for j := range used {
			if !used[j] && ValuesEqual(expected.Index(j).Interface(), a) {
				used[j], matched = true, true
				break
			}
		}
		if !matched {
			unexpected = append(unexpected, a)
		}
	}
	for j, u := range used {
		if !u {
			missing = append(missing, expected.Index(j).Interface())
		}
	}
	return unexpected, missing
}
