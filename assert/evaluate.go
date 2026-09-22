package assert

// Evaluator is an optional interface a Matcher may implement to produce its verdict and its failure
// message in a single pass over its inputs. Evaluate (below) prefers it when present, which is what
// lets Not, All and Any (assert/composite_matchers.go) evaluate each of their own sub-matchers
// exactly once per assertion, however deeply they nest, instead of once to decide and again, on
// failure, to build the message. That distinction matters because a matcher may deliberately carry a
// side effect — MatchErrorAs's errors.As(actual, target) populates target as part of matching — and
// asking twice asks for that side effect twice. See the Matcher doc comment in matcher.go for why
// this exists, and the header comment of composite_matchers.go for the design this replaced.
type Evaluator interface {
	Evaluate(actual any) (matched bool, failure string)
}

// nilMatcherFailure is the message Evaluate reports for a nil matcher — untyped or typed — and the
// text every combinator's own Evaluate reuses verbatim (with its own "#N: " position prefix) for a
// nil entry, so a nil matcher reads identically whether it is asked about directly or found inside a
// composite.
const nilMatcherFailure = "nil matcher (never matches)"

// Evaluate returns m's verdict and, when it fails, its failure message, calling into m exactly once.
//
//   - A nil m, in either the untyped sense (m == nil) or the typed sense (a non-nil Matcher
//     interface wrapping a nil pointer/map/slice/etc — see isNilMatcher in composite_matchers.go),
//     never matches and is never called into; it reports nilMatcherFailure instead of panicking.
//   - When m implements Evaluator, Evaluate delegates to it, so a composite's own single-pass logic
//     runs uninterrupted rather than being driven from the outside through Match and FailureMessage.
//   - Otherwise this already is one evaluation for an ordinary matcher: Match decides the result, and
//     FailureMessage is built only when that call reports false. The doubling this package used to
//     risk only ever came from a composite driving its children through Match and then, again on its
//     own failure, through FailureMessage — never from this path.
func Evaluate(m Matcher, actual any) (matched bool, failure string) {
	if isNilMatcher(m) {
		return false, nilMatcherFailure
	}
	if e, ok := m.(Evaluator); ok {
		return e.Evaluate(actual)
	}
	if m.Match(actual) {
		return true, ""
	}
	return false, m.FailureMessage(actual)
}
