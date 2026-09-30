package mock

import "fmt"

type exampleTB struct{}

func (exampleTB) Helper()               {}
func (exampleTB) Cleanup(func())        {}
func (exampleTB) Errorf(string, ...any) {}

func ExampleCaptor() {
	ctrl := NewController(exampleTB{})
	captured := NewCaptor[string]()
	ctrl.Method("Send").Expect(MatchT("an email address", func(s string) bool { return len(s) > 3 })).AtLeast(1)
	ctrl.Method("Save").Expect(captured.Matcher()).AtLeast(1)

	ctrl.Method("Send").Call("a@b.co")
	ctrl.Method("Save").Call("first")
	ctrl.Method("Save").Call("second")

	fmt.Println(captured.Values(), captured.Last())
	// Output: [first second] second
}
