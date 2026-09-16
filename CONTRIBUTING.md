# CONTRIBUTING.md

Thanks for contributing to go-specs.

---

# Development Setup

Requirements:

* Go 1.22+
* make (optional)

Clone:

```
git clone https://github.com/getsyntegrity/go-specs
```

Run tests (from repo root; use `make test` because there is no root module):

```
make test
```

Or run tests per module, e.g. `go test ./specs/... ./gen/... ./snapshots/... ./benchmarks/... ./examples/...` (see `Makefile` for the full list).

Race detector:

```
make test-race
```

---

# Coding Guidelines

Follow standard Go practices.

Important rules:

* avoid reflection when possible
* avoid allocations in hot paths
* prefer simple APIs

---

# Pull Requests

PRs must include:

* tests
* benchmarks (if performance related)
* documentation updates if APIs change

---

# Benchmark Validation

If modifying performance-sensitive code:

```
go test ./benchmarks -run='^$' -bench=. -benchmem
```

Results should not regress significantly.
