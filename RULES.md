# RULES.md

This file defines hard rules for working in `go-specs`.

---

## 1. Public DSL Stability

Do not break the public user-facing DSL unless explicitly requested.

Stable forms include:

```go
specs.Describe(...)
specs.When(...)
specs.It(...)
ctx.Expect(...).ToEqual(...)
specs.Paths(...)
```

There is no `ToBeNil` method. An earlier version of this list named
`ctx.Expect(...).ToBeNil(...)` as a stable form; no such method has ever existed in the module and
calling it is a compile error. The nil check is the matcher form:

```go
ctx.Expect(value).To(specs.BeNil())
```

See [ADR-0011](docs/adr/0011-public-dsl-stability-contract.md) for the correction, and
[docs/02-DSL.md](docs/02-DSL.md) for the authoritative DSL reference.
