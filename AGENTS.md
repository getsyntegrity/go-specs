# AGENTS.md

This file is the root instruction file for AI agents working in the `go-specs` repository.

When an agent is explicitly pointed to `@AGENTS.md`, it must also load and follow the repository companion guidance files:

- `ARCHITECTURE.md`
- `BENCHMARKS.md`
- `RULES.md`
- `VALIDATION.md`
- `SKILLS.md`
- `CONTRIBUTING.md`

If any of those files exist, they are considered part of the effective repository instructions.

---

## Repository Purpose

`go-specs` is a Go BDD/spec-style testing framework focused on:

- stable DSL ergonomics
- deterministic execution
- low allocations
- typed expectations

Agents must optimize and extend the framework **without degrading correctness, determinism, or benchmark integrity**.

---

## Instruction Precedence

When working in this repository, apply instructions in this order:

1. direct user request
2. `AGENTS.md`
3. companion repository docs referenced by `AGENTS.md`
4. existing code structure and test expectations

If two instructions conflict, prefer the one that is more specific and closer to the concrete task.

---

## Mandatory Companion Files

When `@AGENTS.md` is referenced, agents must also read and follow:

### `ARCHITECTURE.md`
Internal design constraints and system boundaries.

### `BENCHMARKS.md`
How performance work must be measured.

### `RULES.md`
Hard repository rules that must not be violated.

### `VALIDATION.md`
Required commands and checks before considering work complete.

### `SKILLS.md`
Common high-value tasks and the preferred way to execute them.

### `CONTRIBUTING.md`
General contribution and code hygiene guidance.

---

## Core Project Rules

Agents must preserve the public DSL.

These forms are stable:

```go
specs.Describe(...)
specs.When(...)
specs.It(...)
ctx.Expect(...).ToEqual(...)
```
