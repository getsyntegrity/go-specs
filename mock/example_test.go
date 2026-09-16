package mock

import "fmt"

func ExampleSpy() {
	spy := NewSpy()
	spy.Call("user@example.com", 42)

	fmt.Println(spy.CallCount())
	fmt.Println(spy.CalledWith(Equal("user@example.com"), Any()))
	fmt.Println(spy.CalledWith(Equal("someone-else@example.com"), Any()))
	// Output:
	// 1
	// true
	// false
}
