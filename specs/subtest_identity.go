// subtest_identity.go defines the single mapping from a spec's declared Describe/When/It breadcrumb
// to the name go-specs hands testing.T.Run, shared by both sequential execution models (#102).
package specs

import "strings"

// subtestSeparator joins Describe/When/It names into one Go subtest identity. It is testing's own
// subtest separator, so every declared scope becomes its own element of a `go test -run` pattern.
const subtestSeparator = "/"

// joinSubtestPath returns the Go subtest name go-specs gives a sequential spec declared under
// scopes (outermost Describe first) with the leaf It name last. Both sequential execution models —
// Describe/Spec/ExecutionPlan and Builder/Program/Runner — build their breadcrumbs through it, so a
// spec declared as
//
//	Describe(t, "Cart", func(s *specs.Spec) {
//		s.When("empty", func(s *specs.Spec) {
//			s.It("has no items", ...)
//		})
//	})
//
// runs as the subtest TestCart/Cart/empty/has_no_items and is selectable with
// `go test -run 'TestCart/Cart/empty/has_no_items'`.
//
// The mapping is deliberately not public API. It is an internal detail of how go-specs names
// subtests, and nothing outside this package consumes it.
//
// The mapping is a plain join: it never rewrites and never escapes a segment. Go's testing package
// applies its own presentation rules on top of the returned name — call the result of those rules
// the spec's *normalized* breadcrumb — and the normalized form is what a -run pattern must match:
//
//   - Spaces become underscores ("has no items" is matched as "has_no_items"), and non-printable
//     runes are escaped, by testing's own rewrite of the subtest name.
//   - A "/" inside a single Describe/When/It name is not escaped; it adds a pattern element, so
//     It("a/b") under Describe("D") normalizes exactly like Describe("D")/Describe("a")/It("b").
//   - An empty name contributes an empty element rather than collapsing, so It("") under
//     Describe("D") is "D/" — a trailing empty element, not just "D".
//
// The guarantee this mapping makes is therefore scoped to the normalized form: two specs are
// independently identifiable — selectable one at a time with `go test -run`, and never told apart by
// testing's incidental "#01" suffix — whenever their normalized breadcrumbs differ. Two specs
// sharing only a leaf It name under different scopes satisfy that, which is the point of the
// mapping.
//
// Breadcrumbs that normalize to the same string stay ambiguous, and testing disambiguates them with
// its usual "#01" suffix. That is a consequence of a deliberate design choice, not a limitation
// testing imposes: go-specs flattens the declared tree into a single t.Run per spec. The compiler
// emits one linear instruction stream, coalesces groups by hookKey, and flattens each scope's hooks
// into the spec's own instruction range, so a whole spec is one unit of execution with exactly one
// subtest to name — and the breadcrumb has to be encoded into that one name, where "/" and " " stop
// being separable from the segments around them.
//
// The alternative is not escaping, it is nesting: a real t.Run per Describe/When scope, so each
// segment is its own subtest and "a/b" can never be mistaken for "a" then "b". That would undo the
// execution model — a live *testing.T per scope, hooks re-resolved per level instead of flattened
// per spec, a goroutine per scope rather than per spec, and group coalescing losing its meaning. The
// flat stream is what keeps the runner allocation-free without a reporter; nesting trades that away
// to remove a collision only deliberately confusable names can reach.
//
// A suite that registers BeforeAll/AfterAll is the one exception (#207): each hooked group gets a
// real subtest so its hooks can share the group's *testing.T (see group_hooks.go). Those group
// subtests are named so that every spec's full name is still exactly the one this mapping gives
// it, and a group whose subtest could not keep that promise runs without one.
//
// Escaping was rejected for a smaller reason: `go test -run 'TestX/suite/when_a/does_it'` — a
// pattern a developer types by reading the declared names — would stop matching.
//
// Only Go subtest identity is derived this way. Reporter events are not re-derived from the subtest
// name: SpecStartEvent.Name stays the declared leaf name verbatim, so testing's rewrite of spaces
// and its "#01" suffixing never leak back into a report.
//
// SpecStartEvent.Path is a weaker guarantee, and it predates this mapping. specEventPath rebuilds it
// by splitting the same joined string on "/", so a "/" inside one declared name is indistinguishable
// from a scope boundary: It("slash/inside") under Describe("suite")/When("when b") reports the four
// segments ["suite" "when b" "slash" "inside"], and its last segment is not the spec's Name. Path is
// therefore the breadcrumb only for names that contain no separator. Tracked in #113; the fix is
// to carry the segments from the compiler's name stack instead of round-tripping them through a
// joined string.
//
// The result is built into one exactly-sized buffer rather than through `strings.Join(...) + sep +
// leaf`, which would allocate the joined prefix and then the concatenation. This runs once per
// declared It on both build paths, so the saved allocation is per spec, not per suite.
func joinSubtestPath(scopes []string, leaf string) string {
	if len(scopes) == 0 {
		return leaf
	}
	n := len(leaf) + len(scopes)*len(subtestSeparator)
	for _, s := range scopes {
		n += len(s)
	}
	var b strings.Builder
	b.Grow(n)
	for _, s := range scopes {
		b.WriteString(s)
		b.WriteString(subtestSeparator)
	}
	b.WriteString(leaf)
	return b.String()
}
