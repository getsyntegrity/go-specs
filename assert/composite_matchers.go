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

// Describer is an optional interface a Matcher may implement to name itself in composite failures.
// Not needs this because, when it fails, its sub-matcher *succeeded* — calling sub.FailureMessage
// would print a message describing a comparison that did not fail ("expected 5 to equal 5"). A
// matcher that does not implement Describer falls back to %T rather than printing nothing, but that
// fallback leaks unexported type names (*assert.equalMatcher), which is why every built-in matcher
// implements this.
type Describer interface{ Description() string }

// describeMatcher renders m for a composite failure message: its own Description() when it has one,
// its %T otherwise. It is also used to describe nil-free sub-matchers inside a nested composite's
// own Description(), so nesting reads as English at every depth.
func describeMatcher(m Matcher) string {
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
	if n.sub == nil {
		return false
	}
	return !n.sub.Match(actual)
}

func (n *notMatcher) FailureMessage(actual any) string {
	if n.sub == nil {
		return "Not: sub-matcher at position 1 is nil"
	}
	// Reached only when n.sub.Match(actual) was true, so quoting its FailureMessage would print a
	// message about a comparison that succeeded. Description() is the only safe thing to name here.
	return fmt.Sprintf("expected %v not to be %s", actual, describeMatcher(n.sub))
}

func (n *notMatcher) Description() string {
	if n.sub == nil {
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
		if m == nil || !m.Match(actual) {
			return false
		}
	}
	return true
}

func (a *allMatcher) FailureMessage(actual any) string {
	var parts []string
	for i, m := range a.ms {
		idx := i + 1
		switch {
		case m == nil:
			parts = append(parts, fmt.Sprintf("#%d: nil matcher (never matches)", idx))
		case !m.Match(actual):
			parts = append(parts, fmt.Sprintf("#%d: %q", idx, m.FailureMessage(actual)))
		}
		// A matching entry contributed nothing to the failure, so it is left out of the message —
		// only the sub-matchers actually responsible are named.
	}
	if len(parts) == 0 {
		// All() with no matchers is always true, so this path is not reachable through ordinary use
		// (FailureMessage is only ever called after Match returned false). It is still handled
		// explicitly rather than left to print an empty "All: " — a testing framework reports, it
		// does not crash or go silent.
		return "All: no sub-matcher explains the failure"
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
		if m == nil {
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
	hasNil := false
	for _, m := range a.ms {
		if m == nil {
			hasNil = true
			break
		}
	}
	parts := make([]string, 0, len(a.ms))
	for i, m := range a.ms {
		idx := i + 1
		switch {
		case m == nil:
			parts = append(parts, fmt.Sprintf("#%d: nil matcher (never matches)", idx))
		case hasNil && m.Match(actual):
			// This entry would have made an ordinary Any succeed. It is reported, not silently
			// dropped, so the reader sees why a matching sibling did not save the result — and not
			// via FailureMessage, which would describe a comparison that succeeded.
			parts = append(parts, fmt.Sprintf("#%d: would have matched (%s), but a nil entry forces this Any to fail",
				idx, describeMatcher(m)))
		default:
			parts = append(parts, fmt.Sprintf("#%d: %q", idx, m.FailureMessage(actual)))
		}
	}
	return "Any: " + strings.Join(parts, "; ")
}

func (a *anyMatcher) Description() string {
	return "any of [" + strings.Join(describeEntries(a.ms), ", ") + "]"
}

// describeEntries renders each of ms for a composite's own Description(), naming a nil entry
// explicitly rather than dereferencing it.
func describeEntries(ms []Matcher) []string {
	parts := make([]string, len(ms))
	for i, m := range ms {
		if m == nil {
			parts[i] = "<nil>"
			continue
		}
		parts[i] = describeMatcher(m)
	}
	return parts
}
