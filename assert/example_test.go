package assert

import "fmt"

func ExampleEqual() {
	m := Equal(42)
	fmt.Println(m.Match(42))
	fmt.Println(m.Match(43))
	// Output:
	// true
	// false
}

func ExampleNotEqual() {
	m := NotEqual(42)
	fmt.Println(m.Match(42))
	fmt.Println(m.Match(43))
	// Output:
	// false
	// true
}

func ExampleBeNil() {
	m := BeNil()
	var p *int
	fmt.Println(m.Match(nil))
	fmt.Println(m.Match(p))
	fmt.Println(m.Match(0))
	// Output:
	// true
	// true
	// false
}

func ExampleBeTrue() {
	m := BeTrue()
	fmt.Println(m.Match(true))
	fmt.Println(m.Match(false))
	// Output:
	// true
	// false
}

func ExampleBeFalse() {
	m := BeFalse()
	fmt.Println(m.Match(false))
	fmt.Println(m.Match(true))
	// Output:
	// true
	// false
}

func ExampleContain() {
	fmt.Println(Contain("wor").Match("hello world"))
	fmt.Println(Contain(3).Match([]int{1, 2, 3}))
	fmt.Println(Contain(4).Match([]int{1, 2, 3}))
	// Output:
	// true
	// true
	// false
}

// A failed Contain names why: a genuine missing element reads as before, but an actual Contain has
// no strategy for at all (here, an int) or an expected value whose type could never match the
// actual's elements (here, a string needle against []int) get their own explicit reason instead of
// both looking like "not present" (issue #277). Map-key containment specifically is not supported —
// a map actual gets the same "unsupported actual" diagnosis as any other unsupported type.
func ExampleContain_failureMessage() {
	fmt.Println(Contain(4).FailureMessage([]int{1, 2, 3}))
	fmt.Println(Contain(1).FailureMessage(42))
	fmt.Println(Contain("x").FailureMessage([]int{1, 2, 3}))
	// Output:
	// expected [1 2 3] to contain 4
	// expected 42 to contain 1 — int is not a supported Contain actual (want string, slice, or array)
	// expected [1 2 3] to contain x — []int actual needs an int expected value, got string
}
