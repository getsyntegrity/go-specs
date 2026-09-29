package specs

import "github.com/getsyntegrity/go-specs/assert"

// Matcher is the interface for assertion matchers used with Expect(...).To(m).
// It is the same as assert.Matcher; re-exported for DSL stability.
type Matcher = assert.Matcher

// Re-export core matchers so the public DSL remains specs.Equal, specs.BeTrue, etc.
func Equal(expected any) Matcher    { return assert.Equal(expected) }
func NotEqual(expected any) Matcher { return assert.NotEqual(expected) }
func BeNil() Matcher                { return assert.BeNil() }
func BeTrue() Matcher               { return assert.BeTrue() }
func BeFalse() Matcher              { return assert.BeFalse() }
func Contain(expected any) Matcher  { return assert.Contain(expected) }

// HaveLen expects actual (a string, slice, array, map or chan) to have length n.
func HaveLen(n int) Matcher { return assert.HaveLen(n) }

// BeEmpty expects actual (a string, slice, array, map or chan) to have length zero; a nil slice,
// map or chan is empty.
func BeEmpty() Matcher { return assert.BeEmpty() }

// StartWith expects actual (a string or []byte) to start with prefix.
func StartWith(prefix string) Matcher { return assert.StartWith(prefix) }

// EndWith expects actual (a string or []byte) to end with suffix.
func EndWith(suffix string) Matcher { return assert.EndWith(suffix) }

// MatchRegex expects actual (a string or []byte) to contain a match for the RE2 pattern, compiled
// once. An invalid pattern never matches and its failure message carries the compile error.
func MatchRegex(pattern string) Matcher { return assert.MatchRegex(pattern) }

// HaveKey expects actual (a map) to contain key; a key of the wrong type is a non-match.
func HaveKey(key any) Matcher { return assert.HaveKey(key) }

// HaveValue expects actual (a map) to contain a value equal to value.
func HaveValue(value any) Matcher { return assert.HaveValue(value) }

// HavePair expects actual (a map) to map key to a value equal to value.
func HavePair(key, value any) Matcher { return assert.HavePair(key, value) }

// MatchError expects actual to be an error carrying target's identity, via errors.Is. Equal applies
// the same semantics to errors; MatchError states the intent explicitly at the call site.
func MatchError(target error) Matcher { return assert.MatchError(target) }

// MatchErrorAs expects actual to be an error assignable to target via errors.As, populating target
// on a match. target must be a non-nil pointer to a type implementing error, or to an interface.
func MatchErrorAs(target any) Matcher { return assert.MatchErrorAs(target) }

// Not, All and Any combine existing matchers logically instead of requiring a new matcher type for
// every combination (see assert/composite_matchers.go for the nil/empty semantics and how the
// failure message names the sub-matcher(s) actually responsible).
func Not(m Matcher) Matcher     { return assert.Not(m) }
func All(ms ...Matcher) Matcher { return assert.All(ms...) }
func Any(ms ...Matcher) Matcher { return assert.Any(ms...) }
