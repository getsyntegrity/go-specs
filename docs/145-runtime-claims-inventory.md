# REPORT-002B: Runtime-dependent claims inventory

Satisfies issue #145 hardening item 4, which carries contract v1.2.6 §13's planning item into
this slice. Produced **before** implementation, deliberately: an inventory written after the code
exists documents the tests that were going to be written anyway, which is the opposite of the
point.

## Why this document exists

Three of the four defects found while amending the coordination contract shared one shape: the
contract was internally consistent, and the failure lived in what the runtime or the platform
actually does underneath it — hex case-folding across an environment round-trip, a matrix `RunID`
collision, and test-cache enrollment through `testlog`. Reading the contract harder does not find
that class. Only a test that exercises the real behaviour does.

So this enumerates every normative claim in the contract that rests on runtime, toolchain or
platform behaviour rather than on go-specs' own logic, and states for each whether a test pins it.

## Legend

- **Pinned** — a test in this repository fails if the claim stops holding.
- **Required** — not pinned today; #145 must pin it before the issue closes.
- **Declared** — deliberately not tested; the contract resolves it as a constraint on the caller,
  not as behaviour to enforce. Listed so the decision is re-read rather than re-litigated.

## Inventory

### A. Filesystem exclusivity

| # | Claim | Source | Status |
|---|---|---|---|
| A1 | `link(2)` fails with `EEXIST` when the destination name is taken, atomically, on a local filesystem. This is the entire publish primitive. | §5 *Write protocol* | **Required** — concurrent-writer test: N goroutines/processes publish the same shard name, exactly one succeeds and the rest report duplicate-producer. |
| A2 | Over NFS, `link` can report `EEXIST` for an operation that **succeeded** (server linked, reply dropped, client retried onto its own link). The conforming recovery is `stat` the **temp** file and accept when `st_nlink == 2`. | §10 *`link(2)` false-negative over NFS* | **Required** — both arms, against an injected `EEXIST` with a stubbed `stat`: `st_nlink == 2` ⇒ publish accepted; `st_nlink == 1` ⇒ duplicate reported. This is the contract's only network-filesystem defence and it is written once, never exercised, and load-bearing exactly when least observable. |
| A3 | The `st_nlink == 2` inference depends on two invisible preconditions: the temp file is linked nowhere else, and nothing unlinks a published shard while the run is in flight. | §10 | **Required** — assert the preconditions structurally: the temp name contains the PID and is created inside the destination directory, and no producer code path calls `unlink`/`Remove` on a published shard. A test that only exercises A2's arms does not notice a future second linking path invalidating the inference. |
| A4 | `O_CREATE\|O_EXCL\|O_NOFOLLOW` gives exclusive creation of `run.json` on a local filesystem, so a second invocation reusing a `RunID` fails at marker creation. | §5 *Run marker*, §10 | **Required** — two concurrent `InitializeRun` calls on one `RunID`: exactly one wins, the loser's error is actionable and names reuse. |
| A5 | `O_EXCL` exclusivity and `link`'s `EEXIST` **degrade on NFS/CIFS**; the `run.json` exposure is unmitigated and unrecoverable by design. | §10 *Network filesystems* | **Declared** — §10 resolves this as a declaration, not an enforcement: portable network-mount detection does not exist, and a check reporting "local" for an NFS-backed bind mount is worse than no check. No detection code exists, so there is nothing to test. |
| A6 | Neither `rename` nor `link` is atomic across a filesystem boundary (`link` fails `EXDEV`), which is why the temp file must live inside the destination shard directory. | §5, §10 | **Required (structural)** — pin the property, not the errno: assert the temp path's directory equals the destination shard directory. Provoking a real `EXDEV` needs two mounts and is not portable in CI. |
| A7 | `os.Rename` replaces an existing destination **silently** on both supported platform families, and on Windows maps to `MoveFileEx` **with** `MOVEFILE_REPLACE_EXISTING`. | §5, §10 | **Declared** — this is the claim that motivates rejecting `os.Rename`; since no code path uses it, there is no behaviour to pin. Guard it the cheap way instead: a source-level check that the producer package never calls `os.Rename`, so the primitive cannot be reintroduced by a future "simplification". |

### B. Test cache and the toolchain

| # | Claim | Source | Status |
|---|---|---|---|
| B1 | `os.Environ` delegates to `syscall.Environ` and records **nothing** in the testlog, so a gate-off scan does not enroll variables in the test-cache key. | §5 *disabled path* | **Pinned, by `TestTheEnvironScanIsActuallyAnEnvironScan` — and only by it.** Mutation-checked: rewriting `envread.Scan`'s body as `os.LookupEnv` makes it fail. Getting there took two corrections worth recording, because both are ways a test can look like a pin and not be one. See **The B1 pin** below. |
| B2 | ~~`os.Getenv`/`os.LookupEnv` route through `testlog`, so a gate-on read enrolls the variable and a new `GO_SPECS_RUN_ID` invalidates that package's cached result.~~ **FALSE at this contract's call site.** | §5 *Environment, not files, is the transport* | **Pinned as refuted.** Measured: a variable read via `os.Getenv` **inside a test function** does invalidate the cache; the same read **in `TestMain` before `m.Run()`** does not — the value can change on every run and `go test` still reports `(cached)`. The mechanism is that `testlog`'s logger is installed by `m.before()`, which runs inside `m.Run()`; before that, `testlog.Getenv` sees a nil logger and records nothing. Contract §3 step 3 mandates the read happen exactly there. Consequences in **The B2 finding** below. |
| B3 | `cmd/go` never propagates a test binary's exit code; a failed test action sets the literal `1`. This is why the config-error distinction lives in `config-error.json` + `78`/`EX_CONFIG` rather than in an exit status. | §8 | **Pinned, and stronger than §8 claims.** §8's note says a `TestMain` calling `os.Exit(2)` at least "produces the text `exit status 2` in the output". Measured: a binary whose tests passed and which then exits `78` leaves no trace of `78` anywhere — `cmd/go` prints `PASS`, then `FAIL` for the package, and exits `1`. CI cannot branch on the code even by scraping the log. |
| B4 | A cache hit skips `TestMain` **entirely**, so a cached package never publishes a shard — which is why `-count=1` is mandatory for reporting runs. | F4, §6 | **Pinned.** Confirmed directly, and it is worse than "no shard": with the gate on and an invalid configuration, a cached package reports `ok (cached)` and exits `0` — no failure, no `config-error.json`, nothing to distinguish a misconfigured reporting run from a healthy green one. |
| B5 | `TestMain`'s post-`m.Run()` code runs on ordinary test failure, including a panic recovered by the testing package; only abrupt termination (F5) bypasses it. | §8 row 2, F5 | **Required** — a package with a failing test and a panicking test still publishes a complete shard. Issue #145 lists this as an acceptance criterion in its own right. |

### C. Paths and names

| # | Claim | Source | Status |
|---|---|---|---|
| C1 | Go's `os.fixLongPath` transparently rewrites to the extended-length `\\?\` form near 248 bytes, including for relative paths, so shard creation does not fail at Windows' 260-character total-path limit. | §5, §10 | **Declared** — a claim about the Go standard library, relied on only to justify *not* adding a long-path opt-in. Nothing in go-specs branches on it. |
| C2 | The `\\?\` form lifts the 260-character **total path** limit but not NTFS's 255-character per-**component** limit. | §5 | **Declared** — recorded to stop the common over-reading. The 117-byte filename budget is comfortably inside it, and C3 pins the budget, which is the part we control. |
| C3 | The assembled filename is bounded at ≤117 bytes: ≤40 prefix + 2 + 64 + 11. | §5 *Shard filename* | **Required** — property test over adversarial import paths (very long, non-ASCII, many separators): every produced filename is ≤117 bytes, and truncation never splits a `%XY` triplet, yielding a 38–40 byte prefix. |
| C4 | The sanitization `/` → `_` is **not injective**: `foo/bar` and `foo_bar` collide. Identity is carried solely by the SHA-256 of the original, unsanitized import path. | §5, §10, #145 hardening 1 | **Required** — two distinct package paths sanitizing to the same prefix must produce two distinct shard files, not one overwriting the other. |

### D. Identity and ownership

| # | Claim | Source | Status |
|---|---|---|---|
| D1 | A layer between the generator and the test binary can case-fold the token (a shell, a CI secret store, a Windows environment round-trip), so the digest must be taken over the token's **decoded bytes**, never its hex text. | §5 *Run marker* | **Required** — a mixed-case `GO_SPECS_RUN_TOKEN` verifies successfully against a marker written from the lowercase form. This is the defect class that surfaces only in an environment, never in review. |
| D2 | `GO_SPECS_RUN_ID` derivation collides in real CI: `GITHUB_RUN_ID` is stable across re-runs, and `run_id`+`run_attempt` is shared by every leg of a matrix; `strategy.job-index` alone is unique only within one matrix. | §5 *Run marker lifecycle* | **Declared (documentation)** — a claim about CI platforms, unobservable from this repository's tests. Its enforcement is A4: a colliding `RunID` fails closed at marker creation regardless of how it was derived. Pin the documentation instead: the workflow recipe in the docs must include all four parts. |
| D3 | The token is readable by every process in the test's process tree and by anything reading `/proc/<pid>/environ` as the same user — it is an accident-detection mechanism, never a security boundary. | §10 | **Declared** — a property of the environment as transport. Nothing may treat a matching token as an authorization decision; that is a review constraint, not a testable behaviour. |

### E. Symlink and permission hardening

| # | Claim | Source | Status |
|---|---|---|---|
| E1 | `O_NOFOLLOW` causes an open to **fail** rather than write through a pre-planted symlink, on every open of `run.json`, `config-error.json`, shard temp files and shard final names, on both the read and the write side. | §10 rule 1 | **Required — not listed in §13, added here.** Load-bearing security behaviour that is pure runtime semantics, and an `O_NOFOLLOW` accidentally dropped from one of the four open sites is invisible to every other test in this inventory. Test on Unix: plant a symlink at each path and assert the operation fails without touching the target. |
| E2 | `umask` does not apply to a subsequent `Chmod`, which is why a directory this implementation creates must have its mode **verified after creation** rather than assumed from the requested `0700`. | §10 rule 3 | **Required** — create a run directory under a permissive umask (e.g. `0000`) and assert the resulting mode is exactly `0700`. Under the default `0022` this test passes whether or not the verification exists, so the umask must be set explicitly by the test. |
| E3 | A group- or other-writable ancestor of the base directory lets another local user swap a path component, making every downstream check meaningless — so operation must be refused, fail-closed, naming the offending directory and its mode. | §10 rule 2 | **Required** — a base directory under a `0777` non-sticky parent is refused by both `InitializeRun` and the producer, and the sticky exemption is honoured. |

## The B1 pin

B1 was first written as two tests, and neither one pinned it. The sequence is worth keeping,
because each failure mode looks exactly like coverage.

**Attempt 1 — the process-level cache test the contract asks for.**
`TestGateOffIsCacheStableAcrossEveryCoordinationVariable` varies `GO_SPECS_RUN_ID` and
`GO_SPECS_REPORT_DIR` across two gate-off runs and asserts `(cached)`. It passes. It also passes
with `os.Getenv` everywhere, because the reads happen in `TestMain` where the test log does not
exist yet — see *The B2 finding*. Vacuous. It is kept, relabelled in its own doc comment, as a
guard against these reads **moving** somewhere the cache can see them.

**Attempt 2 — the unit tests over the `environment` seam.** A fake records whether each variable
arrived through `Lookup` or `Scan`, in both directions. These are good tests and they stay, but
they pin the *resolver's choice of method*. The production `osEnvironment.Scan` was referenced by
no test at all (`rg osEnvironment --glob '*_test.go'` returned nothing), so its body could be
replaced with `os.LookupEnv` and the entire suite stayed green. The seam was pinned; the thing the
seam exists to guarantee was not.

**What finally works.** The two access paths are distinguishable only where the test log is live,
which is inside a test function. `internal/envread` holds the real implementations so a test can
call them directly, and `internal/shardfixture/envprobe` calls one of them from a test function
while the caller varies the probed variable across two runs. `Scan` must stay `(cached)`; `Lookup`
must re-run.

The control arm is load-bearing rather than decorative: without it, a change making *both* paths
uncacheable would leave the assertion passing for the wrong reason, and a change making both
cacheable would make it vacuous again. The test fails loudly with "proves nothing" in that case
instead of passing.

Two gotchas, both of which produced a wrong answer before being fixed: do not pass `-count=1` to
the seeding run, because it bypasses the cache and seeds nothing; and give every execution a fresh
random nonce, because otherwise the second execution finds the control arm's value already cached
from the first and reports a false failure.

## The B2 finding

Pinning B2 refuted it, and the correction matters more than the claim did.

**What §5 says.** Producers must read run identity with `os.Getenv`/`os.LookupEnv` because `go test`
records those reads in the test-cache key, so a new `GO_SPECS_RUN_ID` invalidates a stale cached
result for that package. §5 presents this as "a supporting reason for the `-count=1` requirement in
§6, not a replacement for it".

**What actually happens.** Nothing. `testlog`'s logger is installed inside `m.Run()`, and §3 step 3
requires the configuration read to happen in `TestMain` **before** `m.Run()`. Every coordination
variable — including the activation gate — is therefore read where the cache cannot see it. The
supporting mechanism §5 describes does not exist at the place §3 puts the code.

**Why it matters in both directions.**

- The disabled-path `os.Environ()` rule, introduced in v1.2.3 and widened in v1.2.4, is protecting
  against a harm that cannot occur at its specified call site. It is not wrong and it should not be
  removed — it becomes load-bearing the moment any of these reads is called from inside a test or a
  helper a test invokes, which is one refactor away — but its stated rationale and its mandated
  regression test both describe a mechanism that is inert here.
- `-count=1` is not a supplement. It is the **only** thing preventing a cached package from silently
  never publishing a shard, and the only thing preventing a misconfiguration from passing green.
  That belongs in the documentation as an operational requirement, not a performance footnote.

**Why review could not have caught it.** The contract is internally consistent: `os.Getenv` does
route through `testlog`, `os.Environ` genuinely does not, and both statements are true in isolation.
The failure lives entirely in *when* the logger exists — which is visible only by running the thing
or by reading `testing.M.before`. This is the §13 class in its purest form, found by the exact
exercise §13 asked for.

## Summary

- **Pinned by this slice:** A1, A2, A3, A4, A6, B1, B3, B4, B5, C3, C4, D1, E1, E2, E3.
- **Mutation-checked, meaning the test was shown to fail when the behaviour is removed:** B1 (`Scan` rewritten as `os.LookupEnv`), E1 (`O_NOFOLLOW` dropped), A1/A2 (publish via `os.Rename`), D1 (`HashRunToken` over the hex text). A pin that has never been seen to fail is a claim, not a pin.
- **Refuted by this slice:** B2 — see *The B2 finding* above. Contract §5 needs an amendment; nothing in the implementation needs to change, because `-count=1` was already mandatory.
- **Declared, deliberately untested:** A5, A7, C1, C2, D2, D3.
- **Surfaced by this inventory and by nothing else:** B2 and E1. E1 was mutation-checked — removing `O_NOFOLLOW` makes its test fail, because a symlinked `run.json` pointing at a marker that would otherwise verify is then accepted. B2 is the finding above.

The two additions are the inventory earning its cost. Neither is exotic: each is a place where the
contract is internally consistent, the code would look correct, and the failure would live entirely
in what the toolchain or the kernel does underneath.
