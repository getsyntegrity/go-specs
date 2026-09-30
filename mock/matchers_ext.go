package mock

import (
	"fmt"
	"reflect"
	"sync"
)

// Match returns a matcher that accepts the arguments for which pred returns true. desc appears in
// diagnostics, for example "argument 0: got 3, want an adult age", so write it as what is wanted.
func Match(desc string, pred func(any) bool) ArgMatcher {
	if pred == nil {
		panic("mock: Match requires a non-nil predicate")
	}
	return &predMatcher{desc: desc, pred: pred}
}

type predMatcher struct {
	desc string
	pred func(any) bool
}

func (m *predMatcher) Match(v any) bool { return m.pred(v) }
func (m *predMatcher) String() string   { return m.desc }

// MatchT is Match with a typed predicate. An argument that is not a T never matches and the
// diagnostics say so ("desc (of type T)"). A nil argument is passed to pred as the zero T when T
// can be nil (pointer, interface, map, slice, func or channel) and does not match otherwise.
func MatchT[T any](desc string, pred func(T) bool) ArgMatcher {
	if pred == nil {
		panic("mock: MatchT requires a non-nil predicate")
	}
	return &typedMatcher[T]{desc: desc, pred: pred}
}

type typedMatcher[T any] struct {
	desc string
	pred func(T) bool
}

func (m *typedMatcher[T]) Match(v any) bool {
	t, ok := asT[T](v)
	return ok && m.pred(t)
}

func (m *typedMatcher[T]) String() string {
	return fmt.Sprintf("%s (of type %s)", m.desc, typeName[T]())
}

// asT converts v to T, treating nil as the zero T when T is nilable.
func asT[T any](v any) (T, bool) {
	if t, ok := v.(T); ok {
		return t, true
	}
	var zero T
	if v == nil {
		switch reflect.TypeFor[T]().Kind() {
		case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
			return zero, true
		}
	}
	return zero, false
}

func typeName[T any]() string { return reflect.TypeFor[T]().String() }

// Captor records the arguments of the calls an expectation claims. Build one with NewCaptor, put
// Matcher() in Expect and read Values or Last after the calls.
//
// A value is recorded only when the whole call is claimed by the expectation holding the matcher:
// a call rejected because another argument did not match, because the expectation was at capacity,
// or because an earlier expectation took it leaves the captor untouched. Captured values are the
// caller's values, not deep copies: a pointer, slice or map can still be mutated by its owner.
// Under concurrent calls the recording order is the order in which claims finish, which may differ
// from the global call sequence. A Captor is safe for concurrent use.
type Captor[T any] struct {
	mu     sync.Mutex
	values []T
}

// NewCaptor returns an empty captor for values of type T.
func NewCaptor[T any]() *Captor[T] { return &Captor[T]{} }

// Matcher returns the matcher that accepts any argument assignable to T (nil for nilable T) and
// arranges for claimed values to be recorded.
func (c *Captor[T]) Matcher() ArgMatcher { return captorMatcher[T]{c} }

// Values returns a copy of the captured values in recording order.
func (c *Captor[T]) Values() []T {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]T(nil), c.values...)
}

// Last returns the most recently captured value, or the zero T when nothing was captured.
func (c *Captor[T]) Last() T {
	c.mu.Lock()
	defer c.mu.Unlock()
	var zero T
	if len(c.values) == 0 {
		return zero
	}
	return c.values[len(c.values)-1]
}

type captorMatcher[T any] struct{ c *Captor[T] }

func (m captorMatcher[T]) Match(v any) bool {
	_, ok := asT[T](v)
	return ok
}

func (m captorMatcher[T]) String() string { return "capture of type " + typeName[T]() }

func (m captorMatcher[T]) claimed(v any) {
	t, ok := asT[T](v)
	if !ok {
		return
	}
	m.c.mu.Lock()
	m.c.values = append(m.c.values, t)
	m.c.mu.Unlock()
}
