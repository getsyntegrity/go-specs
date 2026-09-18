package specs

import "github.com/getsyntegrity/go-specs/assert"

// Matcher is the interface for assertion matchers used with Expect(...).To(m).
// It is the same as assert.Matcher; re-exported for DSL stability.
type Matcher = assert.Matcher

// Re-export core matchers so the public DSL remains specs.Equal, specs.BeTrue, etc.
func Equal(expected any) Matcher   { return assert.Equal(expected) }
func NotEqual(expected any) Matcher { return assert.NotEqual(expected) }
func BeNil() Matcher               { return assert.BeNil() }
func BeTrue() Matcher              { return assert.BeTrue() }
func BeFalse() Matcher             { return assert.BeFalse() }
func Contain(expected any) Matcher { return assert.Contain(expected) }
