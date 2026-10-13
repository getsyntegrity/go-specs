> **CI policy update (2026-10-13):** GitHub Actions runs only fast validation.
> Race, integration, guards, judges and long checks below are local-only.
> See [the current economical CI policy](docs/ci-economy.md).

# VALIDATION.md

This file defines the minimum validation required before work is considered complete in `go-specs`.

---

## Required Validation

Unless the task explicitly says otherwise, run:

```bash
go test ./...
go test -race ./...
```
