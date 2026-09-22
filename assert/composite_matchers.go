package assert

import (
	"fmt"
	"strings"
)

// Not, All and Any let a caller combine existing matchers logically instead of hand-writing a new
// matcher type for every combination (see NotEqual, which is exactly that: Equal negated by hand,
// in its own type, with its own message). #149/#200 taught this repo that a matcher's message is
// the product, so a composite that reports "combined matcher failed" is not an acceptable answer —
// each combinator below names the sub-matcher(s) actually responsible.
//
// FailureMessage may re-run sub-matchers: Match decides the boolean result once, and — only after a
// failure — FailureMessage is called separately to build the message, which for All/Any means
// evaluating the entries again to see which are still responsible (see the Matcher doc comment in
// matcher.go). Memoizing each entry's result inside the composite, so it only ever runs once, was
// rejected: it would make a composite value stateful, so a composite shared across parallel specs
// could build its message from another goroutine's actual, and it would add mutable state to a value
// that is otherwise immutable and safe to share. The cost instead falls on Matcher implementations,
// which the interface now documents as required to be deterministic and free of side effects. The one
// place this file avoids the re-run entirely is Any's nil-entry branch below: a nil sibling already
// dooms the composite, so evaluating the others would buy no diagnostic value.

// Describer is an optional interface a Matcher may implement to name itself in composite failures.
// Not needs this because, when it fails, its sub-matcher *succeeded* — calling sub.FailureMessage
// would print a message describing a comparison that did not fail ("expected 5 to equal 5"). A
// matcher that does not implement Describer falls back to %T rather than printing nothing, but that
// fallback leaks unexported type names (*assert.equalMatcher), which is why every built-in matcher
// implements this.
type Describer interface{ Description() string }

// isNilMatcher reports whether m is nil in either the untyped sense (an unassigned Matcher
// interface, m == nil) or the typed sense (a non-nil interface wrapping a nil pointer/map/slice/
// etc — e.g. a declared-but-unassigned *customMatcher passed as a Matcher). A composite must treat
// both exactly alike: calling Match or FailureMessage on the typed-nil case panics unless it is
// caught here first, so every nil comparison in this file goes through this helper instead of
// comparing to nil directly. IsNilValue (matcher.go) already implements the reflect-based typed-nil
// check that BeNil/IsNilValue use for their own semantics; this just adds the m == nil fast path for
// a genuinely unassigned interface, which reflect.ValueOf(nil) cannot distinguish on its own.
func isNilMatcher(m Matcher) bool {
	return m == nil || IsNilValue(m)
}

// describeMatcher renders m for a composite failure message: its own Description() when it has one,
// its %T otherwise. It is also used to describe nil-free sub-matchers inside a nested composite's
// own Description(), so nesting reads as English at every depth. A nil matcher (typed or untyped)
// renders as "<nil>" rather than risking a panic from calling Description() on a nil receiver.
func describeMatcher(m Matcher) string {
	if isNilMatcher(m) {
		return "<nil>"
	}
	if d, ok := m.(Describer); ok {
		return d.Description()
	}
	return fmt.Sprintf("%T", m)
}

// --- Not -----------------------------------------------------------------------------------------

// Not returns a matcher that succeeds exactly when m does not. A nil m is a caller mistake, not a
// logical value, so Not(nil) never matches — see the nil/empty table in
// odd/tasks/matcher-composition.md.
func Not(m Matcher) Matcher {
	return &notMatcher{sub: m}
}

type notMatcher struct {
	sub Matcher
}

func (n *notMatcher) Match(actual any) bool {
	if isNilMatcher(n.sub) {
		return false
	}
	return !n.sub.Match(actual)
}

func (n *notMatcher) FailureMessage(actual any) string {
	if isNilMatcher(n.sub) {
		return "Not: sub-matcher at position 1 is nil"
	}
	// Reached only when n.sub.Match(actual) was true, so quoting its FailureMessage would print a
	// message about a comparison that succeeded. Description() is the only safe thing to name here.
	return fmt.Sprintf("expected %v not to be %s", actual, describeMatcher(n.sub))
}

func (n *notMatcher) Description() string {
	if isNilMatcher(n.sub) {
		return "not (nil matcher)"
	}
	return "not " + describeMatcher(n.sub)
}

// --- All -------------------------------------------------------------------------------------------

// All returns a matcher that succeeds when every one of ms matches (logical AND). A nil entry never
// matches, so it forces the whole composite to fail exactly like any other failing entry. With no
// matchers at all, All is vacuously true — the identity of AND, the only composable answer to
// "all of nothing holds".
func All(ms ...Matcher) Matcher {
	return &allMatcher{ms: ms}
}

type allMatcher struct {
	ms []Matcher
}

func (a *allMatcher) Match(actual any) bool {
	for _, m := range a.ms {
		if isNilMatcher(m) || !m.Match(actual) {
			return false
		}
	}
	return true
}

func (a *allMatcher) FailureMessage(actual any) string {
	if len(a.ms) == 0 {
		// All() with no matchers is always true, so this path is not reachable through ordinary use
		// (FailureMessage is only ever called after Match returned false). It is still handled
		// explicitly rather than left to print an empty "All: " — a testing framework reports, it
		// does not crash or go silent.
		return "All: no sub-matcher explains the failure"
	}
	var parts []string
	for i, m := range a.ms {
		idx := i + 1
		switch {
		case isNilMatcher(m):
			parts = append(parts, fmt.Sprintf("#%d: nil matcher (never matches)", idx))
		case !m.Match(actual):
			parts = append(parts, fmt.Sprintf("#%d: %q", idx, m.FailureMessage(actual)))
		}
		// A matching entry contributed nothing to the failure, so it is left out of the message —
		// only the sub-matchers actually responsible are named.
	}
	if len(parts) == 0 {
		// a.ms is non-empty here, so every entry is either a nil (which always contributes its own
		// "nil matcher" part above) or a non-nil entry that just matched on this second pass. Reaching
		// an empty parts list therefore means Match's pass said at least one entry failed (hence
		// false), but this message-building pass finds every entry matching — a sub-matcher answered
		// differently on its second call. That is not "no sub-matcher explains the failure"; readers
		// deserve to be told exactly that instead of a placeholder that reads like a bug in this
		// framework.
		return "All: Match reported a failure but every sub-matcher now reports a match — a sub-matcher is not deterministic"
	}
	return "All: " + strings.Join(parts, "; ")
}

func (a *allMatcher) Description() string {
	return "all of [" + strings.Join(describeEntries(a.ms), ", ") + "]"
}

// --- Any -------------------------------------------------------------------------------------------

// Any returns a matcher that succeeds when at least one of ms matches (logical OR). With no matchers
// at all, Any is always false — the identity of OR, nothing offered, nothing satisfied. A nil entry
// forces the whole composite to fail even when a sibling matches: masking the mistake behind a
// passing sibling would let it ship green.
func Any(ms ...Matcher) Matcher {
	return &anyMatcher{ms: ms}
}

type anyMatcher struct {
	ms []Matcher
}

func (a *anyMatcher) Match(actual any) bool {
	if len(a.ms) == 0 {
		return false
	}
	// The nil scan comes first so the match loop below can still short-circuit on the first hit.
	// Folding the two together would force every entry to be evaluated on every call just to find
	// out whether a later one is nil — a cost paid by the overwhelmingly common nil-free case.
	for _, m := range a.ms {
		if isNilMatcher(m) {
			return false
		}
	}
	for _, m := range a.ms {
		if m.Match(actual) {
			return true
		}
	}
	return false
}

func (a *anyMatcher) FailureMessage(actual any) string {
	if len(a.ms) == 0 {
		return "Any: no matchers were given, so nothing could match"
	}
	nilAt := -1
	for i, m := range a.ms {
		if isNilMatcher(m) {
			nilAt = i
			break
		}
	}
	if nilAt >= 0 {
		// Match never reaches the match loop once any entry is nil (its own nil scan short-circuits
		// first), so a sibling here was never evaluated to decide the result. Evaluating it now, only
		// to build the message, would ask a question Match itself never asked — a composite that was
		// already going to fail regardless of the answer — and a Matcher with a side effect would pay
		// for that extra call. Describe every non-nil sibling instead of running it.
		parts := make([]string, 0, len(a.ms))
		for i, m := range a.ms {
			idx := i + 1
			if isNilMatcher(m) {
				parts = append(parts, fmt.Sprintf("#%d: nil matcher (never matches)", idx))
				continue
			}
			parts = append(parts, fmt.Sprintf("#%d: %s — not evaluated, the nil entry at #%d forces this Any to fail",
				idx, describeMatcher(m), nilAt+1))
		}
		return "Any: " + strings.Join(parts, "; ")
	}

	parts := make([]string, 0, len(a.ms))
	for i, m := range a.ms {
		if m.Match(actual) {
			// A matching entry contributed nothing to the failure, so it is left out of the message —
			// only the sub-matchers actually responsible are named.
			continue
		}
		parts = append(parts, fmt.Sprintf("#%d: %q", i+1, m.FailureMessage(actual)))
	}
	if len(parts) == 0 {
		// Match's pass said every entry failed (hence false overall), but this second,
		// message-building pass finds every entry matching. Same non-determinism as All's identical
		// guard: a sub-matcher answered differently on its second call, so say that instead of
		// emitting a misleading empty "Any: " list.
		return "Any: Match reported a failure but every sub-matcher now reports a match — a sub-matcher is not deterministic"
	}
	return "Any: " + strings.Join(parts, "; ")
}

func (a *anyMatcher) Description() string {
	return "any of [" + strings.Join(describeEntries(a.ms), ", ") + "]"
}

// describeEntries renders each of ms for a composite's own Description(). describeMatcher already
// names a nil entry (typed or untyped) as "<nil>" rather than dereferencing it, so this just maps
// over ms.
func describeEntries(ms []Matcher) []string {
	parts := make([]string, len(ms))
	for i, m := range ms {
		parts[i] = describeMatcher(m)
	}
	return parts
}
