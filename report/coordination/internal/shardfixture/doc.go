// Package shardfixture holds the participating packages used to prove multi-package shard
// emission against a real `go test` invocation.
//
// These are ordinary packages that wire the documented TestMain integration, not mocks. The
// claims they pin are runtime and toolchain behaviour — a cache hit skipping TestMain entirely,
// cmd/go not propagating a test binary's exit code, os.Environ not enrolling a variable in the
// cache key — and none of that is observable from inside a single process
// (docs/145-runtime-claims-inventory.md, claims B1 to B5).
//
// With the activation gate off, which is how the repository's own `go test ./...` runs them, they
// are inert: they publish nothing and behave exactly like any other package.
package shardfixture
