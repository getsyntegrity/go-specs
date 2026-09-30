package specs

import "github.com/getsyntegrity/go-specs/assert"

// Project maps the actual to a field or derived value with project and applies child to it; a
// failure starts with name, the path of the projection. It is assert.Project re-exported so the
// public DSL stays specs.*; see assert.Project for the nil, type mismatch and panic behaviour.
func Project[T, V any](name string, project func(T) V, child Matcher) Matcher {
	return assert.Project(name, project, child)
}
