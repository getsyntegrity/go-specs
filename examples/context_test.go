// context_test.go shows the *specs.Context every spec receives.
//
// Context is the handle a spec uses to assert, to register cleanup, to report failures from
// helpers and to reach the underlying *testing.T. The most used pieces:
//
//   - ctx.Expect(actual).ToEqual(want) and .To(matcher): dynamic assertions.
//   - specs.EqualTo and specs.ExpectT: typed comparisons for comparable values (no reflection).
//   - ctx.Cleanup: registers teardown that runs when the spec ends, last registered first.
//   - ctx.Errorf: records a failure and lets the spec continue. It fails the spec, so it is
//     described here rather than triggered.
//   - ctx.Helper and ctx.Testing: for helper functions that must not appear in the failure line.
//
// Each Expectation is single-use: build a new one with ctx.Expect for every assertion. Do not keep
// a ctx after its spec ended; use ctx.Go for work that runs concurrently (parallel_test.go).
package examples_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
)

// Expect takes any value. ToEqual compares by value (deep equality for slices, maps and structs); To takes a matcher for richer
// checks. The assert package (see the matcher examples) has the full matcher catalog.
func TestContext_expect(t *testing.T) {
	specs.Describe(t, "expect", func(s *specs.Spec) {
		s.It("compares values and applies matchers", func(ctx *specs.Context) {
			ctx.Expect([]int{1, 2}).ToEqual([]int{1, 2})
			ctx.Expect([]string{"a", "b"}).To(specs.HaveLen(2))
			ctx.Expect(errors.New("boom")).To(specs.Not(specs.BeNil()))
		})
	})
}

// EqualTo and ExpectT are the typed, allocation-friendly variants for comparable types. The
// compiler checks that both sides have the same type, so `EqualTo(ctx, 1, "1")` does not compile.
func TestContext_typedAssertions(t *testing.T) {
	specs.Describe(t, "typed", func(s *specs.Spec) {
		s.It("compares comparable values", func(ctx *specs.Context) {
			specs.EqualTo(ctx, 42, 42)
			specs.ExpectT(ctx, "go").ToEqual("go")
			specs.ExpectT(ctx, true).To(specs.BeTrue())
		})
	})
}

// Cleanup registers teardown that runs when the spec ends, after AfterEach hooks and after any
// ctx.Go tasks, in reverse registration order like defer. Use it next to the code that creates
// the resource, so setup and teardown stay together. The order below is proven by the check after
// Describe.
func TestContext_cleanup(t *testing.T) {
	var order []string

	specs.Describe(t, "cleanup", func(s *specs.Spec) {
		s.It("releases resources in reverse order", func(ctx *specs.Context) {
			ctx.Cleanup(func() { order = append(order, "release database") })
			ctx.Cleanup(func() { order = append(order, "release connection") })
			order = append(order, "spec body")
		})
	})

	got := strings.Join(order, ", ")
	if want := "spec body, release connection, release database"; got != want {
		t.Fatalf("order = %q, want %q", got, want)
	}
}

// Errorf reports a failure without stopping the spec, like testing.T.Errorf, so one spec can list
// several problems at once. It is safe to call from ctx.Go tasks and cleanups. This example does
// not call it, because calling it fails the spec. The usual shape is:
//
//	for _, item := range items {
//		if item.Price < 0 {
//			ctx.Errorf("item %q has a negative price %d", item.Name, item.Price)
//		}
//	}
//
// Testing returns the spec's testing.TB (nil on engines that have none), which is useful to
// print the test name or to hand to libraries that take a testing.TB.
func TestContext_testingHandle(t *testing.T) {
	specs.Describe(t, "testing handle", func(s *specs.Spec) {
		s.It("exposes the underlying test", func(ctx *specs.Context) {
			tb := ctx.Testing()
			ctx.Expect(tb != nil).To(specs.BeTrue())
			ctx.Expect(strings.HasPrefix(tb.Name(), "TestContext_testingHandle/")).To(specs.BeTrue())
		})
	})
}

// A helper that asserts on behalf of a spec should call ctx.Helper() first so that, when a
// helper-based check fails, the report points at the spec line that called the helper.
func expectPositive(ctx *specs.Context, n int) {
	ctx.Helper()
	ctx.Expect(n > 0).To(specs.BeTrue())
}

func TestContext_helper(t *testing.T) {
	specs.Describe(t, "helpers", func(s *specs.Spec) {
		s.It("uses a helper", func(ctx *specs.Context) {
			expectPositive(ctx, 3)
		})
	})
}
