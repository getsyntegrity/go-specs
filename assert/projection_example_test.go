package assert

import "fmt"

// ExampleProject checks an order's status and every one of its items without a hand-written boolean:
// each line is a projection plus an ordinary matcher, and a failure names the field that failed.
// Per-item checks are built with a loop today; a quantified collection matcher will replace the loop
// and compose the same way, since it is just another Matcher.
func ExampleProject() {
	type item struct {
		SKU      string
		Quantity int
	}
	type order struct {
		Status string
		Items  []item
	}
	o := order{Status: "open", Items: []item{{"a", 1}, {"b", 0}}}

	checks := []Matcher{Project("Status", func(o order) string { return o.Status }, Equal("paid"))}
	for i := range o.Items {
		checks = append(checks, Project(fmt.Sprintf("Items[%d]", i),
			func(o order) item { return o.Items[i] },
			Project("Quantity", func(it item) int { return it.Quantity }, BeGreaterThan(0))))
	}

	_, failure := Evaluate(All(checks...), o)
	fmt.Println(failure)
	// Output:
	// All: #1: "Status: expected open to equal paid"; #3: "Items[1].Quantity: expected 0 to be greater than 0"
}
