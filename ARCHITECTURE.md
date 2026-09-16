# ARCHITECTURE.md

This document describes the repository layout and package boundaries for `go-specs`.

---

## Monorepo Layout

The repository is a **single Go module** (`github.com/pablogore/go-specs`, root `go.mod`). There is no `go.work` and no per-package `go.mod`; every directory below is a regular package within that one module.

```
go-specs
├── specs        # runner + DSL (package: github.com/pablogore/go-specs/specs)
├── assert       # core assertions / matchers (package: github.com/pablogore/go-specs/assert)
├── gen          # value generators for property testing (package: github.com/pablogore/go-specs/gen)
├── snapshots    # snapshot storage and comparison (package: github.com/pablogore/go-specs/snapshots)
├── mock         # mocking utilities (package: github.com/pablogore/go-specs/mock)
├── report/      # event types and reporter (package: github.com/pablogore/go-specs/report)
├── benchmarks/  # performance benchmarks (go-specs vs Testify vs Gomega)
├── examples/    # usage examples (package: github.com/pablogore/go-specs/examples)
└── tools/
    └── specs-cli/   # CLI (package: github.com/pablogore/go-specs/tools/specs-cli)
```

---

## Package Dependencies

- **specs** → assert, report, snapshots
- **assert** → (none)
- **report** → (none)
- **gen** → (none)
- **snapshots** → (none)
- **mock** → (none)
- **benchmarks** → specs
- **examples** → specs, mock
- **tools/specs-cli** → specs

No cycles: assert, gen, snapshots, and mock do not depend on specs or runner.

---

## Import Paths

Public import paths are unchanged for compatibility:

- `github.com/pablogore/go-specs/specs`
- `github.com/pablogore/go-specs/assert`
- `github.com/pablogore/go-specs/report`
- `github.com/pablogore/go-specs/mock`
- `github.com/pablogore/go-specs/gen/generators`
- `github.com/pablogore/go-specs/snapshots`

Internal code lives under the specs package and uses:

- `github.com/pablogore/go-specs/specs/internal/registry`

---

## Build and Test

From repo root:

- **Build:** `go build ./...`
- **Test:** `go test ./...`
- **Bench:** `go test ./benchmarks -run='^$' -bench=. -benchmem`
- **CLI:** `go build -o specs-cli ./tools/specs-cli`

Or use `make test`, `make bench`, `make build`.
