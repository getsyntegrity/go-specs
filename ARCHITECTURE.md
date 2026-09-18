# ARCHITECTURE.md

This document describes the repository layout and package boundaries for `go-specs`.

The full architectural reference — dependency rules, the execution pipeline, state and concurrency
boundaries — lives in [`docs/01-ARCHITECTURE.md`](docs/01-ARCHITECTURE.md). This file is the quick
map.

---

## Layout

The repository is a **single Go module** (`github.com/getsyntegrity/go-specs`, root `go.mod`). There
is no `go.work` and no per-package `go.mod`; every directory below is a regular package within that
one module. There is no `internal/` directory.

```
go-specs
├── specs/            # runner + DSL          (github.com/getsyntegrity/go-specs/specs)
├── assert/           # matchers, equality    (github.com/getsyntegrity/go-specs/assert)
├── mock/             # spies, call recording (github.com/getsyntegrity/go-specs/mock)
├── snapshots/        # snapshot storage      (github.com/getsyntegrity/go-specs/snapshots)
├── report/           # events, model, renderers (github.com/getsyntegrity/go-specs/report)
├── gen/generators/   # value generators      (github.com/getsyntegrity/go-specs/gen/generators)
├── tools/perfcheck/  # benchmark regression CLI (package main)
├── benchmarks/       # go-specs vs Testify vs Gomega
├── examples/         # usage examples
├── scripts/          # benchmark chart generation
└── docs/             # documentation suite and ADRs
```

---

## Package dependencies

- **specs** → assert, report, snapshots
- **assert**, **report**, **snapshots**, **mock**, **gen/generators** → (none)
- **benchmarks** → specs
- **examples** → specs, mock
- **tools/perfcheck** → (none; it parses `go test -json` output)

No cycles: no surface package imports `specs`. `specs` never imports `mock` at all.

---

## Import paths

- `github.com/getsyntegrity/go-specs/specs`
- `github.com/getsyntegrity/go-specs/assert`
- `github.com/getsyntegrity/go-specs/report`
- `github.com/getsyntegrity/go-specs/mock`
- `github.com/getsyntegrity/go-specs/snapshots`
- `github.com/getsyntegrity/go-specs/gen/generators`

---

## Build and test

From the repository root:

- **Build:** `go build ./...`
- **Test:** `go test ./...`
- **Race:** `go test -race ./...`
- **Bench:** `go test ./benchmarks -run='^$' -bench=. -benchmem`

Or use `make build`, `make test`, `make test-race`, `make bench`.

**There is no CLI.** go-specs ships as a Go module only; `.goreleaser.yaml` builds no binary. Earlier
revisions of this file described a `tools/specs-cli` that has never existed on this branch — see
[ADR-0015](docs/adr/0015-library-only-release-artifact.md). `tools/perfcheck` is a contributor tool
run with `go run`, not a released artifact.
