// numeric_matchers_test.go shows the ordering matchers: BeGreaterThan, BeGreaterThanOrEqual,
// BeLessThan, BeLessThanOrEqual, BeBetween and BeCloseTo.
//
// Use them for counts, durations, prices and measurements where "at least", "below" or "about" is
// the real requirement.
//
// Semantics:
//   - Numbers compare with numbers and strings with strings (byte-wise). A number never compares
//     with a string; a bool, nil, struct, slice or time.Time is not orderable at all. The failure
//     message says which of those happened instead of printing a bogus ordering.
//   - Different numeric kinds compare by exact value: an int against a uint64 or a float64 works
//     without overflow or rounding. Named types such as time.Duration work too.
//   - NaN is not ordered: it never matches any of these matchers, on either side.
//   - BeBetween is inclusive at both ends; lo above hi never matches; lo equal to hi matches that value.
//   - BeCloseTo(target, delta) checks |actual-target| <= delta after converting to float64, so it
//     is only as exact as a float64. A negative or NaN delta never matches.
package examples_test

import (
	"fmt"
	"math"
	"time"

	"github.com/getsyntegrity/go-specs/assert"
)

func Example_orderingMatchers() {
	fmt.Println(assert.BeGreaterThan(3).Match(4))
	fmt.Println(assert.BeGreaterThan(3).Match(3))
	fmt.Println(assert.BeGreaterThanOrEqual(3).Match(3))
	fmt.Println(assert.BeLessThan(3).Match(2))
	fmt.Println(assert.BeLessThanOrEqual(3).Match(4))
	_, failure := assert.Evaluate(assert.BeGreaterThan(10), 4)
	fmt.Println(failure)
	// Output:
	// true
	// false
	// true
	// true
	// false
	// expected 4 to be greater than 10
}

// Mixed numeric kinds are compared exactly, and named types work.
func Example_mixedNumericTypes() {
	fmt.Println(assert.BeLessThan(uint64(1)).Match(-1))
	fmt.Println(assert.BeGreaterThan(int64(math.MaxInt64)).Match(uint64(math.MaxInt64) + 1))
	fmt.Println(assert.BeGreaterThan(2.5).Match(3))
	fmt.Println(assert.BeLessThan(time.Second).Match(250 * time.Millisecond))
	// Output:
	// true
	// true
	// true
	// true
}

func Example_stringOrdering() {
	fmt.Println(assert.BeLessThan("banana").Match("apple"))
	fmt.Println(assert.BeGreaterThan("banana").Match("apple"))
	// Output:
	// true
	// false
}

// Values that cannot be ordered against each other get an explanation.
func Example_orderingMisuse() {
	fmt.Println(assert.BeGreaterThan(1).Match("2"))
	fmt.Println(assert.BeGreaterThan(1).FailureMessage("2"))
	fmt.Println(assert.BeGreaterThan(1).FailureMessage(true))
	fmt.Println(assert.BeGreaterThan(1).Match(math.NaN()))
	// Output:
	// false
	// BeGreaterThan: cannot compare string with int
	// BeGreaterThan: bool is not a number or string
	// false
}

func Example_beBetween() {
	m := assert.BeBetween(1, 10)
	fmt.Println(m.Match(1))
	fmt.Println(m.Match(10))
	fmt.Println(m.Match(11))
	fmt.Println(assert.BeBetween(5, 5).Match(5))
	fmt.Println(assert.BeBetween(10, 1).Match(5))
	_, failure := assert.Evaluate(m, 11)
	fmt.Println(failure)
	fmt.Println(assert.BeBetween(10, 1).FailureMessage(5))
	// Output:
	// true
	// true
	// false
	// true
	// false
	// expected 11 to be between 1 and 10 (inclusive)
	// expected 5 to be between 10 and 1 — lower bound is greater than upper bound
}

func Example_beCloseTo() {
	// Floating point results should not be compared with Equal.
	a, b := 0.1, 0.2
	sum := a + b // 0.30000000000000004 at run time
	fmt.Println(assert.Equal(0.3).Match(sum))
	fmt.Println(assert.BeCloseTo(0.3, 1e-9).Match(sum))
	// The delta is inclusive, and any numeric kind works as the actual.
	fmt.Println(assert.BeCloseTo(100, 5).Match(105))
	fmt.Println(assert.BeCloseTo(100, 5).Match(106))
	_, failure := assert.Evaluate(assert.BeCloseTo(100, 5), 106)
	fmt.Println(failure)
	// A negative delta is a mistake in the assertion: it never matches.
	fmt.Println(assert.BeCloseTo(100, -1).Match(100))
	// Output:
	// false
	// true
	// true
	// false
	// expected 106 to be within 5 of 100, difference is 6
	// false
}
