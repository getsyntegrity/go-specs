# Legacy architecture parity evidence

Recorded as part of [#127](https://github.com/getsyntegrity/go-specs/issues/127) (PR-B):
evidence that `develop`'s flat `specs` package fully replaces the public surface
`main`'s old `specs/{compiler,dsl,runner,property}` split exposed via its
`specs/*_reexport.go` shims, before those packages and shims are dropped for good.

## Method

`main`'s five reexport files (`compiler_reexport.go`, `context_reexport.go`,
`dsl_reexport.go`, `property_reexport.go`, `runner_reexport.go`) exist for exactly one
reason: keep `specs.X` stable while the implementation lived in subpackages. Their
combined type/const/var/func declarations are therefore the actual public contract
those subpackages backed — not the subpackages themselves, which nothing outside
`specs/` on `main` imports directly.

Every identifier declared across those five files (120 total, some names count
twice across categories) was checked against `develop`'s current flat `specs`
package with `go doc ./specs <Identifier>`.

## Result

**104 / 120 (87%) exist on `develop` under the same name**, including every
identifier [RULES.md](../../RULES.md) names as part of the stable public DSL:
`Describe`, `When`/`It` (as `Spec`/`SuiteTree` methods), `Paths`, `ctx.Expect(...).ToEqual`.

> **Correction.** This list originally also named `ctx.Expect(...).ToBeNil`, inherited from
> `RULES.md` rather than verified with `go doc`. No such method exists on `develop` or on any earlier
> branch; calling it is a compile error. The nil check is `ctx.Expect(x).To(specs.BeNil())`. The
> parity count above is unaffected, because the identifier was never part of either API.
> See [ADR-0011](../adr/0011-public-dsl-stability-contract.md).

The 16 that don't resolve under their old name fall into two groups, neither of
which is a capability regression:

### Internalized, same signature or purpose (11)

`develop` kept the implementation but stopped exporting it — a deliberate
tightening (per `CONTRIBUTING.md`'s "prefer simple APIs"), not a loss:

| `main` (exported) | `develop` (current) |
|---|---|
| `TestBackend`, `AsTestBackend`, `PutTestBackend`, `GetRunSeed` | `testBackend`, `asTestBackend`, `putTestBackend` (`specs/context.go`, `specs/block_runner.go`) |
| `RunSnapshot` | `runSnapshot` (`specs/snapshot.go`) |
| `RunBeforeHooks`, `RunAfterHooks` | internal hook-running in `specs/context.go` / `specs/block_runner.go`, no longer a standalone public entry point |
| `NewPathGenerator` (`vars []PathVar, filters []PathFilter, samples int, seed int64, hasSeed bool, exploreIterations, exploreCoverageIterations, exploreSmartIterations int`) | `newPathGenerator` — identical signature, `specs/path_generator.go` |
| `IntRangeVar` | folded into `PathBuilder.IntRange` (`specs/path_builder.go`) — same capability via the builder API users actually call |
| `SetCaptureCallerLocation` | not found under any name — see below |

### Superseded by the incremental-execution rewrite (5)

`BuildPlanFromArena`, `ShardProgram`, `NewSpecForTest`, `SpecBlock`,
`NewRunnerFromProgram`, `RunCompiledSuite`. These are `Program`/`Runner`-object glue
from the old compile-then-run model. `develop`'s exclusive commit history (89
commits since the `main`/`develop` merge-base) is largely the incremental-execution
rewrite — `RunShard`/`RunShardWithReporter` (`specs/scheduler.go`) and friends now
execute directly instead of building an intermediate `Program`/`Runner` pair. There
is no equivalent to restore; the execution model itself changed, and every runner/
Paths fix #104 wants to release depends on the new model.

`SetCaptureCallerLocation` genuinely has no equivalent on `develop` — it was a
debug toggle for arena caller-location capture, not part of the executed change of
architecture. Flagged here rather than silently dropped; it's a config knob, not a
DSL capability, so it does not block this issue, but a caller relying on it should
know it's gone.

## Open risk (not resolvable from inside this repo)

`specs/compiler`, `specs/dsl`, `specs/runner`, and `specs/property` were never
under `internal/`, so nothing prevents an external module from having imported
them directly instead of going through `specs.X`. This repo has no telemetry on
downstream usage, and the project is pre-1.0 with an explicit "any release may
include breaking changes" disclaimer (`README.md#project-status`), which is the
existing, accepted mitigation for exactly this kind of risk.

## Conclusion

No resurrection needed. `develop`'s flat `specs` package is the current
implementation of the same public contract `main`'s subpackage split protected via
its reexport shims, with a handful of internal-only APIs intentionally narrowed
and the old `Program`/`Runner` glue superseded by the incremental execution model.
`specs/{compiler,dsl,runner,property}` and the `*_reexport.go` shims can be
retired without further migration work.
