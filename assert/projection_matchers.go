package assert

import (
	"fmt"
	"reflect"
	"strings"
)

// Project returns a matcher that maps the actual to a field or derived value with project and then
// applies child to that value. It is the typed counterpart of Satisfy for a domain object: Satisfy
// hides the field behind a boolean, so a failure can only say "expected X to satisfy ...", while
// Project keeps the child's own explanation and puts the name of the field in front of it:
//
//	Project("Status", func(o Order) string { return o.Status }, Equal("paid"))
//	// Status: expected open to equal paid
//
// name is the projection path shown in every failure ("Status", "Items[0].Quantity", "len(Items)");
// an empty name reads as "projection". Projections nest — a child that is itself a Project adds its
// name to the path, joined with "." (or directly when the inner name starts with "["). Because the
// result is an ordinary Matcher, it composes with Not, All and Any, and with any other matcher that
// takes a Matcher, such as a quantified collection matcher.
//
// Behaviour for every input that is not a plain match:
//
//   - The actual is not a T, or is an untyped nil: the matcher never matches, the projection and the
//     child are not called, and the failure says which type it expected and what it got. A typed nil
//     (a nil *Order for T = *Order) is a T, so it reaches the projection.
//   - project is nil, or child is nil (untyped or typed): never matches, nothing is called, and the
//     failure says what is missing.
//   - project panics: the panic is recovered and reported as a failure ("projection panicked: ...")
//     so a bad projection, for example a nil pointer dereference, fails the assertion instead of
//     taking the suite down. A panic inside child is not recovered, exactly as with Satisfy.
//
// Evaluate (assert/evaluate.go) runs the projection once and the child once per assertion; Match and
// FailureMessage stay separately callable like any Matcher, and a FailureMessage called on its own
// projects again. The projected value reaches the child as an any, so an interface-typed projection
// that returns nil reaches it as an untyped nil.
func Project[T, V any](name string, project func(T) V, child Matcher) Matcher {
	return &projectionMatcher[T, V]{name: name, project: project, child: child}
}

type projectionMatcher[T, V any] struct {
	name    string
	project func(T) V
	child   Matcher
}

// pathEvaluator is how a projection reports its own path separately from its failure text, so a
// projection nested inside another can join the two into one path instead of nesting two prefixes.
type pathEvaluator interface {
	evaluatePath(actual any) (matched bool, path, failure string)
}

func (m *projectionMatcher[T, V]) label() string {
	if m.name == "" {
		return "projection"
	}
	return m.name
}

// prepare validates the inputs and runs the projection once. ok is false when the matcher cannot
// go further, and then failure explains why.
func (m *projectionMatcher[T, V]) prepare(actual any) (v any, ok bool, failure string) {
	if m.project == nil {
		return nil, false, "no projection function given"
	}
	if isNilMatcher(m.child) {
		return nil, false, nilMatcherFailure
	}
	in, isT := actual.(T)
	if !isT {
		got := "nil"
		if actual != nil {
			got = fmt.Sprintf("%T", actual)
		}
		return nil, false, fmt.Sprintf("expected input of type %s, got %s", reflect.TypeFor[T](), got)
	}
	out, recovered, panicked := m.safeProject(in)
	if panicked {
		return nil, false, "projection panicked: " + userValue(recovered)
	}
	return out, true, ""
}

func (m *projectionMatcher[T, V]) safeProject(in T) (out V, recovered any, panicked bool) {
	finished := false
	defer func() {
		if !finished {
			recovered = recover()
			panicked = true
		}
	}()
	out = m.project(in)
	finished = true
	return out, nil, false
}

func (m *projectionMatcher[T, V]) Match(actual any) bool {
	v, ok, _ := m.prepare(actual)
	return ok && m.child.Match(v)
}

func (m *projectionMatcher[T, V]) FailureMessage(actual any) string {
	_, failure := m.Evaluate(actual)
	return failure
}

// Evaluate implements Evaluator: one projection and one child evaluation, with the child's failure
// prefixed by the projection path.
func (m *projectionMatcher[T, V]) Evaluate(actual any) (bool, string) {
	matched, path, failure := m.evaluatePath(actual)
	if matched {
		return true, ""
	}
	return false, path + ": " + failure
}

func (m *projectionMatcher[T, V]) evaluatePath(actual any) (bool, string, string) {
	path := m.label()
	v, ok, failure := m.prepare(actual)
	if !ok {
		return false, path, failure
	}
	if inner, nested := m.child.(pathEvaluator); nested {
		matched, innerPath, innerFailure := inner.evaluatePath(v)
		if matched {
			return true, path, ""
		}
		return false, joinProjectionPath(path, innerPath), innerFailure
	}
	matched, failure := Evaluate(m.child, v)
	return matched, path, failure
}

// Description implements Describer so Not, All and Any can name the projection in their messages.
func (m *projectionMatcher[T, V]) Description() string {
	return m.label() + ": " + describeMatcher(m.child)
}

func joinProjectionPath(outer, inner string) string {
	if strings.HasPrefix(inner, "[") {
		return outer + inner
	}
	return outer + "." + inner
}
