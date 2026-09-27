// Package shared_behaviors_test is the runnable companion to the "Shared behaviors" section of
// docs/DSL.md (issue #211): it proves, by executing, that a reusable behavior needs no DSL
// primitive of its own. A shared behavior is just an ordinary Go function that takes the *specs.Spec
// handed to it by the enclosing Describe/When and registers specs on it, exactly like inline code
// would.
package shared_behaviors_test

import (
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
)

// Store is the behavior under test: something that can set and retrieve a value by key.
type Store interface {
	Set(key, value string)
	Get(key string) (string, bool)
}

// mapStore is a plain in-memory Store.
type mapStore struct{ data map[string]string }

func newMapStore() *mapStore { return &mapStore{data: make(map[string]string)} }

func (m *mapStore) Set(key, value string) { m.data[key] = value }

func (m *mapStore) Get(key string) (string, bool) {
	v, ok := m.data[key]
	return v, ok
}

// prefixedStore namespaces every key with a fixed prefix before delegating to an inner Store. It is
// a second, independent Store implementation, so the shared behavior below can be shown reused
// across at least two implementations rather than exercised only once.
type prefixedStore struct {
	prefix string
	inner  Store
}

func newPrefixedStore(prefix string) *prefixedStore {
	return &prefixedStore{prefix: prefix, inner: newMapStore()}
}

func (p *prefixedStore) Set(key, value string) { p.inner.Set(p.prefix+key, value) }

func (p *prefixedStore) Get(key string) (string, bool) { return p.inner.Get(p.prefix + key) }

// behavesLikeAStore is the shared behavior. It is not a specs.* function: it is a plain
// func(*specs.Spec, ...) that a caller invokes from inside its own Describe/When callback, passing
// the *specs.Spec it was handed. Because the helper registers directly on that same Spec, its
// BeforeEach and Its become part of whichever Describe/When called it — there is no separate
// registration surface to learn.
func behavesLikeAStore(s *specs.Spec, mk func() Store) {
	var store Store
	s.BeforeEach(func(ctx *specs.Context) {
		store = mk()
	})

	s.It("stores and retrieves a value", func(ctx *specs.Context) {
		store.Set("key", "value")
		got, ok := store.Get("key")
		ctx.Expect(ok).To(specs.BeTrue())
		ctx.Expect(got).ToEqual("value")
	})

	s.It("reports a missing key", func(ctx *specs.Context) {
		_, ok := store.Get("missing")
		ctx.Expect(ok).To(specs.BeFalse())
	})
}

// TestMapStoreBehavesLikeAStore applies the shared behavior to the first implementation.
func TestMapStoreBehavesLikeAStore(t *testing.T) {
	specs.Describe(t, "mapStore", func(s *specs.Spec) {
		behavesLikeAStore(s, func() Store { return newMapStore() })
	})
}

// TestPrefixedStoreBehavesLikeAStore applies the exact same shared behavior to a second,
// independent implementation. Nothing about behavesLikeAStore changes: it is called the same way,
// from a different Describe, with a different constructor.
func TestPrefixedStoreBehavesLikeAStore(t *testing.T) {
	specs.Describe(t, "prefixedStore", func(s *specs.Spec) {
		behavesLikeAStore(s, func() Store { return newPrefixedStore("ns:") })
	})
}

// behavesLikeAnObservedStore is behavesLikeAStore's twin, with its own BeforeEach/AfterEach added so
// TestSharedBehaviorHookOrder can show exactly where hooks registered inside a shared helper land
// relative to hooks registered inline in the surrounding Describe.
func behavesLikeAnObservedStore(s *specs.Spec, mk func() Store, order *[]string) {
	var store Store
	s.BeforeEach(func(ctx *specs.Context) {
		*order = append(*order, "helper-before")
		store = mk()
	})
	s.AfterEach(func(ctx *specs.Context) {
		*order = append(*order, "helper-after")
	})

	s.It("stores and retrieves a value", func(ctx *specs.Context) {
		*order = append(*order, "test:stores-and-retrieves")
		store.Set("key", "value")
		got, ok := store.Get("key")
		ctx.Expect(ok).To(specs.BeTrue())
		ctx.Expect(got).ToEqual("value")
	})

	s.It("reports a missing key", func(ctx *specs.Context) {
		*order = append(*order, "test:missing-key")
		_, ok := store.Get("missing")
		ctx.Expect(ok).To(specs.BeFalse())
	})
}

// TestSharedBehaviorHookOrder documents, by executing, how hooks captured in the surrounding
// Describe interact with hooks the shared helper registers for itself: both are ordinary
// BeforeEach/AfterEach calls on the very same *specs.Spec, so the usual rule applies — BeforeEach
// runs in registration order, AfterEach runs LIFO (docs/DSL.md's "Execution order of hooks") —
// whether the call came from inline code or from inside a helper function. Here the surrounding
// Describe's BeforeEach/AfterEach are registered before behavesLikeAnObservedStore is called, so
// they run outermost: outer-before, then the helper's own before, then the spec body, then the
// helper's own after (registered after the outer AfterEach, so it runs first, LIFO), then the
// outer after.
func TestSharedBehaviorHookOrder(t *testing.T) {
	var order []string

	specs.Describe(t, "instrumented store", func(s *specs.Spec) {
		s.BeforeEach(func(ctx *specs.Context) { order = append(order, "outer-before") })
		s.AfterEach(func(ctx *specs.Context) { order = append(order, "outer-after") })

		behavesLikeAnObservedStore(s, func() Store { return newMapStore() }, &order)
	})

	want := []string{
		"outer-before", "helper-before", "test:stores-and-retrieves", "helper-after", "outer-after",
		"outer-before", "helper-before", "test:missing-key", "helper-after", "outer-after",
	}
	if len(order) != len(want) {
		t.Fatalf("hook order length = %d, want %d; got %v", len(order), len(want), order)
	}
	for i, step := range want {
		if order[i] != step {
			t.Fatalf("hook order[%d] = %q, want %q; full order: %v", i, order[i], step, order)
		}
	}
}
