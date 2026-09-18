// Package coordination implements the producer side of multi-package test reporting: the
// per-package process that publishes one isolated shard for a single `go test ./...` invocation.
//
// It implements the normative rules in docs/144-report-coordination-contract.md. Comments here
// cite that document as "contract v1.2.6 §N" — sections are amended in place, so a bare section
// number does not identify what was actually relied on.
//
// The division of labour is deliberate and is the reason this is a package of its own:
//
//   - A producer serializes its own process's execution data and nothing else. It never
//     calculates, parses, copies or points at coverage data; the invoker hands the one combined
//     `go test -coverprofile` file straight to the finalizer (contract v1.2.6 §9).
//   - A producer never deletes anything and never reads a sibling's shard. It cannot observe its
//     peers, and a process that dies early must not have been relied on to clean up
//     (contract v1.2.6 §5, F3/F5).
//   - Shard emission is off unless explicitly switched on. GO_SPECS_REPORT_SHARDS is the single
//     activation gate, so a run identifier left exported in a developer's shell is inert and
//     report tooling can never falsify a test result it was never asked to observe
//     (contract v1.2.6 §5, §10).
package coordination
