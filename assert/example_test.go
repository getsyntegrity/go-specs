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
