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
ctx.Expect(...).ToBeNil(...)
```
