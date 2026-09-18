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
| B1 | `os.Environ` delegates to `syscall.Environ` and records **nothing** in the testlog, so a gate-off scan does not enroll variables in the test-cache key. | §5 *disabled path* | **Required** — cache-stability test: run a package twice with the gate off and assert the second run reports `(cached)`. Must vary **at least two** variables (`GO_SPECS_RUN_ID` *and* `GO_SPECS_REPORT_DIR`), because a run-ID-only test passes while another variable still leaks through `os.Getenv`. Ideally vary every variable in the set, so adding a variable without extending the test is what fails. |
| B2 | `os.Getenv`/`os.LookupEnv` **do** call `testlog.Getenv`, so a gate-on read enrolls the variable and a new `GO_SPECS_RUN_ID` invalidates the cached result for that package. | §5 *Environment, not files, is the transport* | **Required — not listed in §13, added here.** B1 pins the negative half; nothing pins the positive half. If the gate-on path were "simplified" to an `os.Environ` scan for symmetry, B1 still passes, the code looks tidier, and cached packages silently stop republishing shards — indistinguishable from a crash (F4). Test: gate on, run twice with a changed `GO_SPECS_RUN_ID`, assert the second run is **not** `(cached)`. |
| B3 | `cmd/go` never propagates a test binary's exit code; a failed test action sets the literal `1`. This is why the config-error distinction lives in `config-error.json` + `78`/`EX_CONFIG` rather than in an exit status. | §8 | **Required** — a `TestMain` exiting `2` must produce `go test` exit `1`. The entire `config-error.json` design rests on this; if it ever stopped holding, the simpler exit-code design would be correct and this one would be needless machinery. |
| B4 | A cache hit skips `TestMain` **entirely**, so a cached package never publishes a shard — which is why `-count=1` is mandatory for reporting runs. | F4, §6 | **Required** — assert a cached package emits no shard. This is the missing-producer condition that must stay loud; a test that never checks it lets "silently cached" and "crashed" stay indistinguishable. |
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

## Summary

- **Required, from §13 explicitly:** A2.
- **Required, derived from §5/§8/§10 while building this inventory:** A1, A3, A4, A6, B1, B3, B4, B5, C3, C4, D1, E2, E3.
- **Required, not previously listed anywhere — surfaced by this inventory:** B2 (the gate-on cache-enrollment half) and E1 (`O_NOFOLLOW` actually refusing a symlink). Both are cases where the contract states a runtime behaviour, the implementation depends on it, and a plausible future simplification removes it while every other test stays green.
- **Declared, deliberately untested:** A5, A7, C1, C2, D2, D3.

The two additions are the inventory earning its cost. Neither is exotic: each is a place where the
contract is internally consistent, the code would look correct, and the failure would live entirely
in what the toolchain or the kernel does underneath.
