// Package e2efixture holds the participating packages used to prove the full producer-to-finalizer
// lifecycle (contract v1.2.8 §3-§9) against real `go test` subprocesses and a real combined
// `-coverprofile` file, for issue #146 spec 2.
//
// Unlike report/coordination/internal/shardfixture (which pins isolated runtime claims about the
// toolchain — cache behaviour, exit codes, os.Environ scanning), this fixture exists to be merged:
// producera and producerb each wire the documented TestMain integration and both import shared, so
// a single `go test ./producera/... ./producerb/... -coverpkg=./producera/...,./producerb/...,
// ./shared/...` invocation instruments shared's blocks from two independent test binaries — the
// exact `-coverpkg` overlap scenario contract v1.2.8 §9 (finding F7) requires the finalizer to
// deduplicate, reproduced here against the real toolchain rather than a synthetic profile.
// producera also registers a BeforeAll/AfterAll group hook, so the merged report carries a real
// hook case (#207/#228) through Finalize.
//
// These are ordinary packages kept in their own directory tree, deliberately not living under
// shardfixture, so the existing shardfixture "..." wildcard used by the shard-emission tests keeps
// matching exactly two packages (alpha, beta) and is never disturbed by this fixture's growth.
package e2efixture
