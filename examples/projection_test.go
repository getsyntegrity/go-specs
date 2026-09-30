// projection_test.go shows assert.Project (also specs.Project): check one field or derived value of
// a domain object with an ordinary matcher, and get a failure that names the field.
//
// Use it instead of Satisfy when you would otherwise write a boolean over a struct. Satisfy can
// only say "expected {...} to satisfy \"status is paid\"". Project keeps the child matcher's own
// explanation and puts the field name in front of it: "Status: expected open to equal paid".
//
// Project[T, V](name, project, child):
//   - name is the path shown in failures ("Status", "Items[0].Quantity", "len(Items)"); an empty
//     name reads as "projection". Projections nest: inner names are joined with ".", or directly
//     when the inner name starts with "[".
//   - The result is an ordinary Matcher, so it works with Not, All, Any and the quantified
//     collection matchers.
//   - If the actual is not a T (or is an untyped nil), the matcher does not match and neither the
//     projection nor the child runs. A typed nil (a nil *Order for T = *Order) is a T and reaches
//     the projection.
//   - If project panics (say, a nil dereference) the panic becomes a failure message. A nil project
//     or child never matches. A panic inside the child is not recovered, as with Satisfy.
package examples_test

import (
	"fmt"
	"testing"

	"github.com/getsyntegrity/go-specs/assert"
	"github.com/getsyntegrity/go-specs/specs"
)

type projItem struct {
	SKU      string
	Quantity int
}

type projCustomer struct {
	Name string
	Tier string
}

type projOrder struct {
	Status   string
	Customer *projCustomer
	Items    []projItem
}

func projStatus() func(projOrder) string { return func(o projOrder) string { return o.Status } }

func TestProjection_inASpec(t *testing.T) {
	specs.Describe(t, "order", func(s *specs.Spec) {
		s.It("checks fields through projections", func(ctx *specs.Context) {
			order := projOrder{Status: "paid", Items: []projItem{{"pen", 2}}}
			ctx.Expect(order).To(specs.Project("Status", projStatus(), specs.Equal("paid")))
			ctx.Expect(order).To(specs.Project("len(Items)", func(o projOrder) int { return len(o.Items) }, specs.Equal(1)))
		})
	})
}

func Example_projectFieldDiagnostics() {
	open := projOrder{Status: "open"}
	m := assert.Project("Status", projStatus(), assert.Equal("paid"))
	fmt.Println(m.Match(projOrder{Status: "paid"}))
	_, failure := assert.Evaluate(m, open)
	fmt.Println(failure)
	// Output:
	// true
	// Status: expected open to equal paid
}

// Several fields at once: All reports every failing projection with its own field name.
func Example_projectSeveralFields() {
	order := projOrder{Status: "open", Items: []projItem{{"a", 1}, {"b", 0}}}
	m := assert.All(
		assert.Project("Status", projStatus(), assert.Equal("paid")),
		assert.Project("len(Items)", func(o projOrder) int { return len(o.Items) }, assert.Equal(2)),
		assert.Project("Items", func(o projOrder) []projItem { return o.Items },
			assert.EveryElement(assert.Project("Quantity", func(i projItem) int { return i.Quantity }, assert.BeGreaterThan(0)))),
	)
	_, failure := assert.Evaluate(m, order)
	fmt.Println(failure)
	// Output:
	// All: #1: "Status: expected open to equal paid"; #3: "Items: EveryElement: 1 of 2 elements failed — [1] {b 0}: \"Quantity: expected 0 to be greater than 0\""
}

// Nested projections join their names into one path.
func Example_projectNested() {
	order := projOrder{Status: "paid", Customer: &projCustomer{Name: "Ann", Tier: "basic"}}
	m := assert.Project("Customer", func(o projOrder) *projCustomer { return o.Customer },
		assert.Project("Tier", func(c *projCustomer) string { return c.Tier }, assert.Equal("gold")))
	_, failure := assert.Evaluate(m, order)
	fmt.Println(failure)

	// An inner name that starts with "[" attaches directly, like an index.
	first := assert.Project("Items", func(o projOrder) []projItem { return o.Items },
		assert.Project("[0]", func(items []projItem) projItem { return items[0] },
			assert.Project("SKU", func(i projItem) string { return i.SKU }, assert.Equal("pen"))))
	_, failure = assert.Evaluate(first, projOrder{Items: []projItem{{"ink", 1}}})
	fmt.Println(failure)
	// Output:
	// Customer.Tier: expected basic to equal gold
	// Items[0].SKU: expected ink to equal pen
}

// Per-item checks: a projection inside EveryElement names the failing element by index and the field.
func Example_projectInsideEveryElement() {
	items := []projItem{{"pen", 2}, {"ink", 0}, {"pad", 4}}
	m := assert.EveryElement(assert.Project("Quantity", func(i projItem) int { return i.Quantity }, assert.BeGreaterThan(0)))
	_, failure := assert.Evaluate(m, items)
	fmt.Println(failure)
	// Output:
	// EveryElement: 1 of 3 elements failed — [1] {ink 0}: "Quantity: expected 0 to be greater than 0"
}

// Type mismatch, nil inputs and panics all become failures. Nothing panics out of the matcher.
func Example_projectMisuse() {
	m := assert.Project("Status", projStatus(), assert.Equal("paid"))

	// The actual is not a projOrder (T), or is nil: neither the projection nor the child runs.
	_, failure := assert.Evaluate(m, "not an order")
	fmt.Println(failure)
	_, failure = assert.Evaluate(m, nil)
	fmt.Println(failure)

	// A typed nil pointer is a T, so it reaches the projection. Here that projection dereferences it.
	byName := assert.Project("Customer.Name", func(o *projOrder) string { return o.Customer.Name }, assert.Equal("Ann"))
	var missing *projOrder
	_, failure = assert.Evaluate(byName, missing)
	fmt.Println(failure)

	// A missing projection function or child is reported too.
	_, failure = assert.Evaluate(assert.Project[projOrder, string]("Status", nil, assert.Equal("x")), projOrder{})
	fmt.Println(failure)
	_, failure = assert.Evaluate(assert.Project("Status", projStatus(), nil), projOrder{})
	fmt.Println(failure)
	// Output:
	// Status: expected input of type examples_test.projOrder, got string
	// Status: expected input of type examples_test.projOrder, got nil
	// Customer.Name: projection panicked: runtime error: invalid memory address or nil pointer dereference
	// Status: no projection function given
	// Status: nil matcher (never matches)
}
