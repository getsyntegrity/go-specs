# SKILLS.md

This file describes common high-value tasks in `go-specs` and the preferred implementation strategy.

---

## Skill: Add a Matcher

### Goal
Ship a matcher whose failure output is as trustworthy as its `Match` result.

### Preferred Approach
- treat `FailureMessage` text as public contract: it is the documentation a user reads when an
  assertion goes wrong
- write one test per `FailureMessage` branch, asserting the exact message, not only that `Match`
  returned false; `assert/error_matchers_failure_test.go` is the reference pattern
- include the degenerate inputs in those cases: a nil actual, a nil error interface, and an actual
  of the wrong type; a type assertion rejects an untyped nil, so a nil check placed after it is dead
  code (issue #200)
- when the matcher wraps an API that can panic on bad arguments, add a case proving it reports a
  failure instead of panicking
- a branch no test can reach is a defect to remove or fix, not coverage to skip

### Validate
```bash
go test ./assert/... ./specs/...
```

---

## Skill: Optimize Matchers

### Goal
Reduce allocations and time spent in expectations/matchers.

### Preferred Approach
- keep public assertion API stable
- add typed fast paths first
- use reflection only as fallback
- replace per-call matcher closures with reusable structs or direct methods

### Validate
```bash
go test ./...
go test -race ./...
go test ./benchmarks -run='^$' -bench=. -benchmem
```
