// focus.go provides focused-spec support. FIt and Focus() are compile-time only;
// the runner does not branch on focus.
package specs

// SpecFn wraps a spec function with options (Focus or Skip). Pass to It: b.It("name", specs.Focus(fn)).
type SpecFn struct {
	Fn    func(*Context)
	Skip  bool
	Focus bool
}

// Focus returns a SpecFn that marks the spec as focused. Use with It: b.It("name", specs.Focus(fn)).
// If any spec is focused, only focused specs are compiled into the program.
func Focus(fn func(*Context)) SpecFn {
	return SpecFn{Fn: fn, Focus: true}
}
