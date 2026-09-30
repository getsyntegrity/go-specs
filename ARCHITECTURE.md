# ARCHITECTURE.md

This document describes the repository layout and package boundaries for `go-specs`.

---

## Monorepo Layout

The repository is a **single Go module** (`github.com/getsyntegrity/go-specs`, root `go.mod`). There is no `go.work` and no per-package `go.mod`; every directory below is a regular package within that one module.

```
go-specs
├── specs        # runner + DSL (package: github.com/getsyntegrity/go-specs/specs)
├── assert       # core assertions / matchers (package: github.com/getsyntegrity/go-specs/assert)
├── snapshots    # snapshot storage and comparison (package: github.com/getsyntegrity/go-specs/snapshots)
├── mock         # mocking utilities (package: github.com/getsyntegrity/go-specs/mock)
├── report/      # event types and reporter (package: github.com/getsyntegrity/go-specs/report)
├── benchmarks/  # performance benchmarks (go-specs vs Testify vs Gomega)
├── examples/    # usage examples (package: github.com/getsyntegrity/go-specs/examples)
└── tools/
    └── specs-cli/   # CLI (package: github.com/getsyntegrity/go-specs/tools/specs-cli)
```

---

## Package Dependencies

- **specs** → assert, report, snapshots
- **assert** → (none)
- **report** → (none)
- **snapshots** → (none)
- **mock** → (none)
- **benchmarks** → specs
- **examples** → specs, mock
- **tools/specs-cli** → specs

No cycles: assert, snapshots, and mock do not depend on specs or runner.

`mock` stays independent of `specs` even though `*specs.Context` is what most specs pass to `mock.NewController`: the controller asks for a small `mock.TB` interface (`Helper`, `Cleanup`, `Errorf`) that `*testing.T`, `*testing.B` and `*specs.Context` all satisfy, and it looks for an optional `Testing() testing.TB` method (which `*specs.Context` provides) only to mark its own frames as helpers on a real `*testing.T`. Tests that exercise `mock` under go-specs live in the external test package `mock_test`, which may import `specs`.

---

## Import Paths

Public import paths are unchanged for compatibility:

- `github.com/getsyntegrity/go-specs/specs`
- `github.com/getsyntegrity/go-specs/assert`
- `github.com/getsyntegrity/go-specs/report`
- `github.com/getsyntegrity/go-specs/mock`
- `github.com/getsyntegrity/go-specs/snapshots`

---

## Build and Test

From repo root:

- **Build:** `go build ./...`
- **Test:** `go test ./...`
- **Bench:** `go test ./benchmarks -run='^$' -bench=. -benchmem`
- **CLI:** `go build -o specs-cli ./tools/specs-cli`

Or use `make test`, `make bench`, `make build`.
