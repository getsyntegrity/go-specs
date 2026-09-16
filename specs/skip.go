// skip.go provides skipped-spec support. SkipIt and Skip() are compile-time only: a skipped spec's
// body is never compiled into a step and never runs. Its name is preserved and, with a Reporter
// attached, reported as skipped (see builder.go's finalize and runner.go's reportSkipped).
package specs

// Skip returns a SpecFn that marks the spec as skipped. Use with It: b.It("name", specs.Skip(fn)).
func Skip(fn func(*Context)) SpecFn {
	return SpecFn{Fn: fn, Skip: true}
}
