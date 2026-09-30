// analysis_test.go shows specs.Analyze, which builds the spec tree without running it.
//
// Analyze is for tools built on top of go-specs: listing specs, checking naming conventions or
// generating documentation. Inside its callback, Describe is given a nil testing.TB, so nothing
// executes; you get a SuiteTree that you can print with Tree or visit with Walk. Each node carries
// its name, type and (unless disabled) the file and line where it was declared.
package examples_test

import (
	"fmt"

	"github.com/getsyntegrity/go-specs/specs"
)

func analyzedSuite() *specs.SuiteTree {
	return specs.Analyze(func() {
		specs.Describe(nil, "calculator", func(s *specs.Spec) {
			s.Describe("addition", func(s *specs.Spec) {
				s.It("adds two numbers", func(ctx *specs.Context) {})
			})
			s.When("dividing by zero", func(s *specs.Spec) {
				s.It("returns an error", func(ctx *specs.Context) {})
			})
		})
	})
}

// Tree renders the declared structure, one indented line per group and spec.
func Example_analyzeTree() {
	fmt.Println(analyzedSuite().Tree())
	// Output:
	// calculator
	//   addition
	//     adds two numbers
	//   dividing by zero
	//     returns an error
}

// Walk visits every node depth-first. The callback receives a node index into the tree's arena,
// from which you read the node's name and type.
func Example_analyzeWalk() {
	tree := analyzedSuite()
	specCount := 0
	tree.Walk(func(id int) {
		if tree.Arena.Nodes[id].Type == specs.ItNode {
			specCount++
		}
	})
	fmt.Println("specs declared:", specCount)
	// Output: specs declared: 2
}
