// pending.go provides pending-spec support: a spec that is declared but not implemented yet.
// PendingIt and Pending() are compile-time only, like SkipIt/Skip — the spec's body never compiles
// into a step and never runs. Pending is a different fact from Skip, though: Skip means
// "intentionally not executed" (environment, temporary exclusion); Pending means "the
// specification exists, the implementation does not". Folding the two together would lose that
// distinction in every report and weaken a red/green TDD workflow, where a pending list is a
// to-do list rather than an exclusion list — see report/model.go's StatusPending and issue #208.
package specs

// Pending returns a SpecFn that marks the spec as pending. Use with ItWith:
// b.ItWith("name", specs.Pending(fn)). fn may be nil — a pending spec often has no body yet.
func Pending(fn func(*Context)) SpecFn {
	return SpecFn{Fn: fn, Pending: true}
}
