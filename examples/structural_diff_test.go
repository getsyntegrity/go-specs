// structural_diff_test.go shows what a failed Equal says when the values are composite: structs,
// maps, slices, arrays and pointers.
//
// A failed comparison of two big values used to read "expected {...} to equal {...}" and leave you
// hunting for the difference. Now the message adds a "differences:" section that names the path of
// each difference (Order.Items[1].Price) with the expected and actual value found there.
//
// You get this for free from specs.Equal, ctx.Expect(...).ToEqual(...) and assert.Equal. To see the
// message without failing a test, call assert.Evaluate(assert.Equal(want), got) or
// assert.EqualFailureMessage(want, got), as the examples do.
//
// What to know about the diff:
//   - It never decides anything: the verdict is Equal's, and the diff is only built after a mismatch.
//   - It mirrors reflect.DeepEqual: a nil slice differs from an empty one, and funcs never match.
//   - It is bounded: at most 10 differences are listed, paths go at most 8 segments deep, and each
//     rendered value is cut at 80 characters. Whatever is cut off is said in the message.
//   - Scalars and errors keep their one-line messages, without a diff section.
//   - Map entries are listed in a deterministic (sorted) order, so the output is stable.
package examples_test

import (
	"fmt"

	"github.com/getsyntegrity/go-specs/assert"
)

type diffAddress struct {
	City string
	Zip  string
}

type diffLine struct {
	SKU   string
	Price int
}

type diffOrder struct {
	ID       int
	Ship     diffAddress
	Items    []diffLine
	Metadata map[string]string
}

func Example_structDiff() {
	want := diffOrder{ID: 7, Ship: diffAddress{City: "Oslo", Zip: "0150"}}
	got := diffOrder{ID: 7, Ship: diffAddress{City: "Bergen", Zip: "0150"}}
	fmt.Println(assert.EqualFailureMessage(want, got))
	// Output:
	// expected {7 {Bergen 0150} [] map[]} to equal {7 {Oslo 0150} [] map[]}
	// differences:
	//   diffOrder.Ship.City: expected "Oslo", actual "Bergen"
}

func Example_sliceAndMapDiff() {
	want := diffOrder{
		Items:    []diffLine{{"pen", 2}, {"ink", 5}},
		Metadata: map[string]string{"channel": "web", "coupon": "SAVE"},
	}
	got := diffOrder{
		Items:    []diffLine{{"pen", 2}, {"ink", 9}, {"pad", 1}},
		Metadata: map[string]string{"channel": "app"},
	}
	_, failure := assert.Evaluate(assert.Equal(want), got)
	fmt.Println(failure)
	// Output:
	// expected {0 { } [{pen 2} {ink 9} {pad 1}] map[channel:app]} to equal {0 { } [{pen 2} {ink 5}] map[channel:web coupon:SAVE]}
	// differences:
	//   diffOrder.Items: length: expected 2, actual 3
	//   diffOrder.Items[1].Price: expected 5, actual 9
	//   diffOrder.Items[2]: unexpected in actual ({SKU: "pad", Price: 1})
	//   diffOrder.Metadata["channel"]: expected "web", actual "app"
	//   diffOrder.Metadata["coupon"]: missing in actual (expected "SAVE")
}

// Scalars and errors do not get a diff section.
func Example_noDiffForScalars() {
	fmt.Println(assert.EqualFailureMessage("paid", "open"))
	fmt.Println(assert.EqualFailureMessage(3, 4))
	// Output:
	// expected open to equal paid
	// expected 4 to equal 3
}

// nil and empty are different values, and the diff says so.
func Example_nilVersusEmpty() {
	fmt.Println(assert.Equal([]int{}).Match([]int(nil)))
	fmt.Println(assert.EqualFailureMessage(diffOrder{Items: []diffLine{}}, diffOrder{}))
	// Output:
	// false
	// expected {0 { } [] map[]} (examples_test.diffOrder) to equal {0 { } [] map[]} (examples_test.diffOrder)
	// differences:
	//   diffOrder.Items: expected empty non-nil slice, actual nil slice
}

// Big differences are bounded: only the first ten are listed and the rest is counted.
func Example_diffIsBounded() {
	want := make([]int, 15)
	got := make([]int, 15)
	for i := range got {
		got[i] = i + 1
	}
	fmt.Println(assert.EqualFailureMessage(want, got))
	// Output:
	// expected [1 2 3 4 5 6 7 8 9 10 11 12 13 14 15] to equal [0 0 0 0 0 0 0 0 0 0 0 0 0 0 0]
	// differences:
	//   [0]: expected 0, actual 1
	//   [1]: expected 0, actual 2
	//   [2]: expected 0, actual 3
	//   [3]: expected 0, actual 4
	//   [4]: expected 0, actual 5
	//   [5]: expected 0, actual 6
	//   [6]: expected 0, actual 7
	//   [7]: expected 0, actual 8
	//   [8]: expected 0, actual 9
	//   [9]: expected 0, actual 10
	//   ... more differences not shown (limit 10)
}
