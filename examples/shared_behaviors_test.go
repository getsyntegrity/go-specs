// shared_behaviors_test.go shows how to reuse a group of specs across several implementations.
//
// A shared behavior needs no DSL primitive: it is an ordinary Go function that receives the
// *specs.Spec of the enclosing Describe/When and registers hooks and specs on it, exactly as
// inline code would. Use it for contract tests, where several types must satisfy the same
// interface. See the "Shared behaviors" section of docs/DSL.md.
package examples_test

import (
	"slices"
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
)

// sharedStore is the behavior under test: something that can set and get a value by key.
type sharedStore interface {
	Set(key, value string)
	Get(key string) (string, bool)
}

// sharedMapStore is a plain in-memory store.
type sharedMapStore struct{ data map[string]string }

func newSharedMapStore() *sharedMapStore { return &sharedMapStore{data: map[string]string{}} }

func (m *sharedMapStore) Set(key, value string) { m.data[key] = value }

func (m *sharedMapStore) Get(key string) (string, bool) {
	v, ok := m.data[key]
	return v, ok
}

// sharedPrefixedStore namespaces every key before delegating to another store. It is a second,
// independent implementation, which lets the behavior below be reused instead of rewritten.
type sharedPrefixedStore struct {
	prefix string
	inner  sharedStore
}

func (p *sharedPrefixedStore) Set(key, value string) { p.inner.Set(p.prefix+key, value) }

func (p *sharedPrefixedStore) Get(key string) (string, bool) { return p.inner.Get(p.prefix + key) }

// behavesLikeAStore is the shared behavior: a plain function, not a specs.* primitive. Its
// BeforeEach and Its become part of whichever Describe or When calls it.
func behavesLikeAStore(s *specs.Spec, mk func() sharedStore) {
	var store sharedStore
	s.BeforeEach(func(ctx *specs.Context) { store = mk() })

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

// The same behavior applied to two implementations. Only the constructor changes.
func TestSharedBehaviors_sameContractTwoImplementations(t *testing.T) {
	specs.Describe(t, "mapStore", func(s *specs.Spec) {
		behavesLikeAStore(s, func() sharedStore { return newSharedMapStore() })
	})
	specs.Describe(t, "prefixedStore", func(s *specs.Spec) {
		behavesLikeAStore(s, func() sharedStore {
			return &sharedPrefixedStore{prefix: "ns:", inner: newSharedMapStore()}
		})
	})
}

// behavesLikeAnObservedStore is the same idea with its own BeforeEach/AfterEach, so the test below
// can show where a helper's hooks land relative to the caller's.
func behavesLikeAnObservedStore(s *specs.Spec, order *[]string) {
	var store sharedStore
	s.BeforeEach(func(ctx *specs.Context) {
		*order = append(*order, "helper-before")
		store = newSharedMapStore()
	})
	s.AfterEach(func(ctx *specs.Context) { *order = append(*order, "helper-after") })

	s.It("reports a missing key", func(ctx *specs.Context) {
		*order = append(*order, "spec")
		_, ok := store.Get("missing")
		ctx.Expect(ok).To(specs.BeFalse())
	})
}

// Hooks registered inside a helper are ordinary hooks on the caller's Spec, so the usual order
// applies: BeforeEach runs in registration order, AfterEach in reverse. Here the caller registers
// its hooks first, so they wrap the helper's.
func TestSharedBehaviors_hookOrder(t *testing.T) {
	var order []string

	specs.Describe(t, "instrumented store", func(s *specs.Spec) {
		s.BeforeEach(func(ctx *specs.Context) { order = append(order, "outer-before") })
		s.AfterEach(func(ctx *specs.Context) { order = append(order, "outer-after") })

		s.When("it is empty", func(s *specs.Spec) {
			behavesLikeAnObservedStore(s, &order)
		})
	})

	want := []string{"outer-before", "helper-before", "spec", "helper-after", "outer-after"}
	if !slices.Equal(order, want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
}
