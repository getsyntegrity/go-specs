// equality_matchers_test.go shows the matchers that compare a value with an expectation:
// Equal, NotEqual, BeNil, BeTrue, BeFalse and BeZero.
//
// Use them for the plain "is this the value I expect?" checks that make up most specs. Every matcher
// is an ordinary assert.Matcher, so it works in two places:
//
//   - in a spec, as ctx.Expect(actual).To(specs.Equal(want)) (the first test below);
//   - on its own, as m.Match(actual) or assert.Evaluate(m, actual), which is how the runnable
//     Example functions here show verdicts and failure messages without needing a *testing.T.
//
// The rules worth remembering:
//   - Equal compares like ValuesEqual: an int 1 never equals an int64 1, errors match by identity
//     (see error_matchers_test.go), everything else is structural (reflect.DeepEqual style).
//   - BeNil treats a typed nil (a nil *T, map, slice, func, chan, or an interface holding one) as nil.
//   - BeZero follows reflect.Value.IsZero; an empty but non-nil slice is NOT zero (use BeEmpty).
//
// specs.ExpectT and specs.EqualTo are the typed variants for comparable values; they are shown in
// context_test.go.
package examples_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/assert"
	"github.com/getsyntegrity/go-specs/specs"
)

// The typical usage: matchers inside a spec.
func TestEquality_inASpec(t *testing.T) {
	specs.Describe(t, "equality matchers", func(s *specs.Spec) {
		s.It("compares values", func(ctx *specs.Context) {
			ctx.Expect("hello").To(specs.Equal("hello"))
			ctx.Expect(42).To(specs.NotEqual(43))
			ctx.Expect([]int{1, 2}).To(specs.Equal([]int{1, 2}))
		})
		s.It("checks nil, booleans and zero values", func(ctx *specs.Context) {
			var missing *int
			ctx.Expect(missing).To(specs.BeNil())
			ctx.Expect(2 > 1).To(specs.BeTrue())
			ctx.Expect(1 > 2).To(specs.BeFalse())
			ctx.Expect(0).To(specs.BeZero())
		})
		s.It("negates any matcher with Not", func(ctx *specs.Context) {
			ctx.Expect("a").To(specs.Not(specs.Equal("b")))
		})
	})
}

func Example_equal() {
	fmt.Println(assert.Equal(42).Match(42))
	fmt.Println(assert.Equal(42).Match(43))
	fmt.Println(assert.Equal("go").Match("go"))
	// Composite values compare structurally.
	fmt.Println(assert.Equal(map[string]int{"a": 1}).Match(map[string]int{"a": 1}))
	// Output:
	// true
	// false
	// true
	// true
}

// Types matter: an int is not an int64, and a string is not a []byte.
func Example_equalTypeMismatch() {
	fmt.Println(assert.Equal(1).Match(int64(1)))
	fmt.Println(assert.Equal("a").Match([]byte("a")))
	// The failure message adds the types when the two sides print the same.
	_, failure := assert.Evaluate(assert.Equal(1), int64(1))
	fmt.Println(failure)
	// Output:
	// false
	// false
	// expected 1 (int64) to equal 1 (int)
}

func Example_equalFailureMessage() {
	_, failure := assert.Evaluate(assert.Equal("paid"), "open")
	fmt.Println(failure)
	// Output:
	// expected open to equal paid
}

func Example_notEqual() {
	fmt.Println(assert.NotEqual(42).Match(43))
	fmt.Println(assert.NotEqual(42).Match(42))
	_, failure := assert.Evaluate(assert.NotEqual(42), 42)
	fmt.Println(failure)
	// Output:
	// true
	// false
	// expected 42 not to equal 42
}

// BeNil is true for an untyped nil and for a typed nil, but not for a zero value like 0 or "".
func Example_beNil() {
	var (
		ptr   *int
		items []string
		table map[string]int
		fn    func()
		err   error
	)
	fmt.Println(assert.BeNil().Match(nil))
	fmt.Println(assert.BeNil().Match(ptr))
	fmt.Println(assert.BeNil().Match(items))
	fmt.Println(assert.BeNil().Match(table))
	fmt.Println(assert.BeNil().Match(fn))
	fmt.Println(assert.BeNil().Match(err))
	// Values that are not nil, even if empty or zero.
	fmt.Println(assert.BeNil().Match(0))
	fmt.Println(assert.BeNil().Match(""))
	fmt.Println(assert.BeNil().Match([]string{}))
	// Output:
	// true
	// true
	// true
	// true
	// true
	// true
	// false
	// false
	// false
}

// typedNilPointer returns a nil *int stored in an interface.
func typedNilPointer() any {
	var p *int
	return p
}

// A typed nil stored in an interface is the classic Go trap: `iface == nil` is false, but BeNil
// still reports it as nil because it looks at the value inside.
func Example_beNilTypedNilInInterface() {
	iface := typedNilPointer()
	fmt.Println(iface == nil)
	fmt.Println(assert.BeNil().Match(iface))
	_, failure := assert.Evaluate(assert.BeNil(), 7)
	fmt.Println(failure)
	// Output:
	// false
	// true
	// expected nil, got 7 (int)
}

func Example_beTrueBeFalse() {
	fmt.Println(assert.BeTrue().Match(true))
	fmt.Println(assert.BeFalse().Match(false))
	// Only real booleans match: 1 is not true and nil is not false.
	fmt.Println(assert.BeTrue().Match(1))
	fmt.Println(assert.BeFalse().Match(nil))
	_, failure := assert.Evaluate(assert.BeTrue(), false)
	fmt.Println(failure)
	// Output:
	// true
	// true
	// false
	// false
	// expected true, got false (bool)
}

type equalityPoint struct{ X, Y int }

// BeZero follows reflect.Value.IsZero.
func Example_beZero() {
	var nilSlice []int
	fmt.Println(assert.BeZero().Match(0))
	fmt.Println(assert.BeZero().Match(""))
	fmt.Println(assert.BeZero().Match(equalityPoint{}))
	fmt.Println(assert.BeZero().Match(time.Time{}))
	fmt.Println(assert.BeZero().Match(nilSlice))
	// Not zero: a non-zero field, or an empty slice that is not nil (use BeEmpty for that).
	fmt.Println(assert.BeZero().Match(equalityPoint{X: 1}))
	fmt.Println(assert.BeZero().Match([]int{}))
	_, failure := assert.Evaluate(assert.BeZero(), 5)
	fmt.Println(failure)
	// Output:
	// true
	// true
	// true
	// true
	// true
	// false
	// false
	// expected 5 to be the zero value of int
}
