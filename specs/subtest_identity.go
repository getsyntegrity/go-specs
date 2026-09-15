// subtest_identity.go defines the single mapping from a spec's declared Describe/When/It breadcrumb
// to the name go-specs hands testing.T.Run, shared by both sequential execution models (#102).
package specs

import "strings"

// subtestSeparator joins Describe/When/It names into one Go subtest identity. It is testing's own
// subtest separator, so every declared scope becomes its own element of a `go test -run` pattern.
const subtestSeparator = "/"

// joinSubtestName returns the Go subtest name go-specs gives a sequential spec declared under path,
// with the outermost Describe first and the It name last. Both sequential execution models —
// Describe/Spec/ExecutionPlan and Builder/Program/Runner — use this mapping, so a spec declared as
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
// its usual "#01" suffix. That is accepted and inherited, not a defect awaiting a fix: it is exactly
// what testing.T.Run does on its own, where t.Run("a/b") and a nested t.Run("a")/t.Run("b") are
// likewise indistinguishable, and where t.Run("when a") and t.Run("when_a") likewise collide.
// Escaping the separator or the spaces here would buy uniqueness at the cost of the whole feature:
// `go test -run 'TestX/suite/when_a/does_it'` — a pattern a developer types by reading the declared
// names — would stop matching.
//
// Only Go subtest identity is derived this way. Reporter events keep the framework's own
// unsanitized Name and Path values, so nothing testing does here leaks back into a report.
func joinSubtestName(path ...string) string {
	return strings.Join(path, subtestSeparator)
}

// joinSubtestPath is joinSubtestName for a scope stack plus one leaf name, without the intermediate
// slice a variadic call would need. Both compilers build their breadcrumbs through it.
func joinSubtestPath(scopes []string, leaf string) string {
	if len(scopes) == 0 {
		return leaf
	}
	return strings.Join(scopes, subtestSeparator) + subtestSeparator + leaf
}
