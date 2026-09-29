package specs

import (
	"strings"
	"sync"
	"testing"
)

// TestAfterEachOrderContract pins the documented hook order (docs/DSL.md "AfterEach",
// docs/ARCHITECTURE.md) on every engine: BeforeEach runs outer to inner, AfterEach runs inner to
// outer, and within one scope AfterEach runs LIFO. Every engine must produce the same sequence.
func TestAfterEachOrderContract(t *testing.T) {
	const want = "before-outer,before-inner,body,after-inner-2,after-inner-1,after-outer-2,after-outer-1"

	type recorder struct {
		mu     sync.Mutex
		events []string
	}
	hook := func(r *recorder, name string) func(*Context) {
		return func(*Context) {
			r.mu.Lock()
			r.events = append(r.events, name)
			r.mu.Unlock()
		}
	}

	engines := []struct {
		name string
		run  func(t *testing.T, r *recorder)
	}{
		{"Spec.It", func(t *testing.T, r *recorder) {
			Describe(t, "suite", func(s *Spec) {
				s.BeforeEach(hook(r, "before-outer"))
				s.AfterEach(hook(r, "after-outer-1"))
				s.AfterEach(hook(r, "after-outer-2"))
				s.Describe("inner", func(s *Spec) {
					s.BeforeEach(hook(r, "before-inner"))
					s.AfterEach(hook(r, "after-inner-1"))
					s.AfterEach(hook(r, "after-inner-2"))
					s.It("spec", hook(r, "body"))
				})
			})
		}},
		{"Spec.ItParallel", func(t *testing.T, r *recorder) {
			Describe(t, "suite", func(s *Spec) {
				s.BeforeEach(hook(r, "before-outer"))
				s.AfterEach(hook(r, "after-outer-1"))
				s.AfterEach(hook(r, "after-outer-2"))
				s.Describe("inner", func(s *Spec) {
					s.BeforeEach(hook(r, "before-inner"))
					s.AfterEach(hook(r, "after-inner-1"))
					s.AfterEach(hook(r, "after-inner-2"))
					s.ItParallel("spec", hook(r, "body"))
				})
			})
		}},
		{"Builder.It", func(t *testing.T, r *recorder) {
			b := NewBuilder()
			b.Describe("suite", func() {
				b.BeforeEach(hook(r, "before-outer"))
				b.AfterEach(hook(r, "after-outer-1"))
				b.AfterEach(hook(r, "after-outer-2"))
				b.Describe("inner", func() {
					b.BeforeEach(hook(r, "before-inner"))
					b.AfterEach(hook(r, "after-inner-1"))
					b.AfterEach(hook(r, "after-inner-2"))
					b.It("spec", hook(r, "body"))
				})
			})
			NewRunner(b.Build()).Run(t)
		}},
		{"Builder.ItParallel", func(t *testing.T, r *recorder) {
			b := NewBuilder()
			b.Describe("suite", func() {
				b.BeforeEach(hook(r, "before-outer"))
				b.AfterEach(hook(r, "after-outer-1"))
				b.AfterEach(hook(r, "after-outer-2"))
				b.Describe("inner", func() {
					b.BeforeEach(hook(r, "before-inner"))
					b.AfterEach(hook(r, "after-inner-1"))
					b.AfterEach(hook(r, "after-inner-2"))
					b.ItParallel("spec", hook(r, "body"))
				})
			})
			NewRunner(b.Build()).Run(t)
		}},
	}

	for _, e := range engines {
		t.Run(e.name, func(t *testing.T) {
			r := &recorder{}
			e.run(t, r)
			r.mu.Lock()
			got := strings.Join(r.events, ",")
			r.mu.Unlock()
			if got != want {
				t.Errorf("hook order\n got: %s\nwant: %s", got, want)
			}
		})
	}
}
