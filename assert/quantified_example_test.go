package assert

import "fmt"

// Examples over domain objects. The failure text is part of the contract: it names the failing
// elements by index, so the output below is checked by `go test`.

type exampleLine struct {
	SKU     string
	Qty     int
	Shipped bool
}

func lineShipped() Matcher {
	return Satisfy("a shipped line", func(v any) bool { l, ok := v.(exampleLine); return ok && l.Shipped })
}

func ExampleEveryElement() {
	lines := []exampleLine{{"pen", 2, true}, {"ink", 1, false}, {"pad", 4, true}}
	_, failure := Evaluate(EveryElement(lineShipped()), lines)
	fmt.Println(failure)
	fmt.Println(EveryElement(lineShipped()).Match([]exampleLine{}))
	// Output:
	// EveryElement: 1 of 3 elements failed — [1] {ink 1 false}: "expected {ink 1 false} to satisfy \"a shipped line\""
	// true
}

func ExampleExactlyNElements() {
	lines := []exampleLine{{"pen", 2, true}, {"ink", 1, false}, {"pad", 4, true}}
	fmt.Println(ExactlyNElements(2, lineShipped()).Match(lines))
	_, failure := Evaluate(ExactlyNElements(1, lineShipped()), lines)
	fmt.Println(failure)
	// Output:
	// true
	// ExactlyNElements: expected exactly 1 of 3 elements to match, 2 did — matching indices [0 2]
}

func ExampleNoElement() {
	lines := []exampleLine{{"pen", 2, true}, {"ink", 1, false}}
	_, failure := Evaluate(NoElement(lineShipped()), lines)
	fmt.Println(failure)
	// Output:
	// NoElement: 1 of 2 elements matched — [0] {pen 2 true}
}

func ExampleHaveElementsInOrder() {
	events := []string{"created", "paid", "shipped"}
	fmt.Println(HaveElementsInOrder(Equal("created"), Equal("paid"), Equal("shipped")).Match(events))
	_, failure := Evaluate(HaveElementsInOrder(Equal("created"), Equal("shipped")), events)
	fmt.Println(failure)
	// Output:
	// true
	// HaveElementsInOrder: expected 2 elements, got 3 — unexpected [2] shipped; 1 of 2 positions failed — [1] paid: "expected paid to equal shipped"
}

func ExampleContainElementsInOrder() {
	events := []string{"created", "paid", "packed", "shipped"}
	fmt.Println(ContainElementsInOrder(Equal("paid"), Equal("shipped")).Match(events))
	_, failure := Evaluate(ContainElementsInOrder(Equal("shipped"), Equal("paid")), events)
	fmt.Println(failure)
	// Output:
	// true
	// ContainElementsInOrder: matched 1 of 2 matchers in order; matcher #2 (equal to paid) matched no element after index 3
}
