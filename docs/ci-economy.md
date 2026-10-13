# Economical CI policy

Maintainer decision, 2026-10-13. This policy supersedes older documentation
that says GitHub Actions runs the complete local quality suite.

## GitHub Actions

One Linux validation job per pull request, with a five-minute hard limit.
No duplicate validation on push after merge, no matrices, no scheduled checks.
Documentation-only changes complete the same check with a visible not-applicable
result before installing Go. Go caches are shared by this single validation path.

The job checks formatting, builds affected packages and runs short in-process
unit tests, including transitive consumers through production and test imports.
Dependency/workspace/workflow changes and ambiguous changes select all eligible
packages. It validates the PR merge tree, not only the isolated branch head.

No race detector, integration/system tests, architecture guards, judges,
mutations, coverage gates, fuzz campaigns, benchmarks, or long scans run in
Actions, including dispatch and release workflows. Existing commands and tests
remain available for voluntary local execution; they are not represented as
passing CI checks.

## Test selection

`.github/scripts/ci-fast.go` parses Go source with the standard AST parser.
It excludes named integration/architecture/guard/judge/timing/fixture suites,
subprocesses, real sockets and explicit waits, and follows calls to local
helpers. Unsafe `init` or `TestMain` excludes that package's test binary.
The selector is deliberately conservative and prints packages with no eligible
unit tests. It is a source-based filter, not a sandbox: indirect calls through
interfaces or other packages require the same explicit integration naming.
Tests marked with `testing.Short()` are excluded rather than assuming that
`-short` alone prevents every external test. Benchmarks, examples and fuzz
targets are not selected. Test code still compiles; each unit package has a
30-second execution timeout and the command has a four-minute total budget.

Run the same path locally: `make ci-fast`. Without `CI_BASE_SHA`, it selects
all eligible packages. Set `CI_BASE_SHA` to a local commit for impact selection.
Complete local verification commands remain separate from this target.

## Repository settings

Require only the actual fast-validation check named below. Remove required
checks for deleted jobs; no dummy success statuses are created for them.
Branch-protection reads returned HTTP 403 through the connected GitHub app;
settings must be updated by an owner when this PR is integrated. Rules about
reviews and merge methods remain unchanged.

## Cost

The new validation uses one job: at most five runner minutes per PR, rather
than a fresh runner for each gate or shard. This is a hard budget, not a
measured prediction. Actual savings depend on caching and changed packages.
No paid runs were launched or retried for this change.

Required fast check: **ci-ok**.

Validation before opening this PR: local fast path, selector regression tests,
transitive-consumer/documentation selection fixture, YAML parsing, shell syntax,
and actionlint (shellcheck unavailable). No race/integration/judge suites run.

Measured sample: [run 37945614570](https://github.com/getsyntegrity/go-specs/actions/runs/37945614570): 14 non-skipped jobs, 15 runner minutes after per-job rounding.
This repository is public; runner time is not necessarily billed quota.
Publication and changelog sync remain separate, each capped at five minutes,
with no validation smoke tests or notifications. Require `ci-ok`; remove `pr-meta`.
