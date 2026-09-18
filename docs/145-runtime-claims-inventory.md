# REPORT-002B: Runtime-dependent claims inventory

Satisfies issue #145 hardening item 4, which carries contract v1.2.7 §13's planning item into
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

- **Pinned** — a test in this repository fails if the claim stops holding, and the test is named in the row.
- **Unpinned** — no test fails if the claim stops holding. Either a gap, or a claim no test can
  distinguish; the row says which.
- **Declared** — deliberately not tested; the contract resolves it as a constraint on the caller,
  not as behaviour to enforce. Listed so the decision is re-read rather than re-litigated.

## Inventory

### A. Filesystem exclusivity

| # | Claim | Source | Status |
|---|---|---|---|
| A1 | `link(2)` fails with `EEXIST` when the destination name is taken, atomically, on a local filesystem. This is the entire publish primitive. | §5 *Write protocol* | **Pinned** — `TestConcurrentPublishersOfOneDestinationProduceExactlyOneWinner`: 24 racers, exactly one nil error, every other `ErrDuplicateProducer`, no litter. Mutation-checked — publishing via `os.Rename` fails it. |
| A2 | Over NFS, `link` can report `EEXIST` for an operation that **succeeded** (server linked, reply dropped, client retried onto its own link). The conforming recovery is `stat` the **temp** file and accept when `st_nlink == 2`. | §10 *`link(2)` false-negative over NFS* | **Pinned, beyond what this row asked for** — `TestNFSFalseEEXISTIsAcceptedWhenOurOwnLinkSucceeded` (`st_nlink == 2` ⇒ accepted, only the temp file removed) and `TestGenuineDuplicateIsStillReportedWhenTheTempFileHasOneName` (`st_nlink == 1` ⇒ duplicate). `TestDuplicateIsReportedWhenTheLinkCountCannotBeObtained` adds the third case this row missed: an unreadable link count is not evidence of success, so it fails closed. |
| A3 | The `st_nlink == 2` inference depends on two invisible preconditions: the temp file is linked nowhere else, and nothing unlinks a published shard while the run is in flight. | §10 | **Pinned** — `TestTempFileLivesInTheDestinationDirectoryAndCarriesThePID` for the first precondition, `TestPublishNeverRemovesAPublishedFile` for the second, which drives all four publish outcomes and asserts the final path is never the argument to `remove`. |
| A4 | `O_CREATE\|O_EXCL\|O_NOFOLLOW` gives exclusive creation of `run.json` on a local filesystem, so a second invocation reusing a `RunID` fails at marker creation. | §5 *Run marker*, §10 | **Pinned** — `TestInitializeRunIsExclusiveUnderConcurrency` (16 racers, exactly one winner) plus `TestInitializeRunRefusesAnExistingMarker`, which asserts the loser's message names the run, `gc` and `--force`, since "exactly one wins" without an actionable message only half-satisfies the row. |
| A5 | `O_EXCL` exclusivity and `link`'s `EEXIST` **degrade on NFS/CIFS**; the `run.json` exposure is unmitigated and unrecoverable by design. | §10 *Network filesystems* | **Declared** — §10 resolves this as a declaration, not an enforcement: portable network-mount detection does not exist, and a check reporting "local" for an NFS-backed bind mount is worse than no check. No detection code exists, so there is nothing to test. |
| A6 | Neither `rename` nor `link` is atomic across a filesystem boundary (`link` fails `EXDEV`), which is why the temp file must live inside the destination shard directory. | §5, §10 | **Pinned (structural)** — `TestTempFileLivesInTheDestinationDirectoryAndCarriesThePID` asserts `tempName` returns a bare name with no directory component, so it can only ever be joined into the destination directory. Provoking a real `EXDEV` needs two mounts and is not portable in CI. |
| A7 | `os.Rename` replaces an existing destination **silently** on both supported platform families, and on Windows maps to `MoveFileEx` **with** `MOVEFILE_REPLACE_EXISTING`. | §5, §10 | **Declared, and the guard this row proposed was NOT adopted.** No code path uses `os.Rename`, so there is no behaviour to pin — but the suggested source-level check that the package never calls it does not exist. In practice the protection is indirect: `TestPublishRefusesAnExistingDestinationAndLeavesItIntact` and the concurrency test both fail if a replacing rename is reintroduced. Recorded as a known gap rather than quietly upgraded to Pinned. |

### B. Test cache and the toolchain

| # | Claim | Source | Status |
|---|---|---|---|
| B1 | `os.Environ` delegates to `syscall.Environ` and records **nothing** in the testlog, so a gate-off scan does not enroll variables in the test-cache key. | §5 *disabled path* | **Pinned, by `TestTheEnvironScanIsActuallyAnEnvironScan` — and only by it.** Mutation-checked: rewriting `envread.Scan`'s body as `os.LookupEnv` makes it fail. Getting there took two corrections worth recording, because both are ways a test can look like a pin and not be one. See **The B1 pin** below. |
| B2 | ~~`os.Getenv`/`os.LookupEnv` route through `testlog`, so a gate-on read enrolls the variable and a new `GO_SPECS_RUN_ID` invalidates that package's cached result.~~ **Refuted, and now withdrawn from the contract.** | §5 *Environment, not files, is the transport* | **Closed.** Measured here, then withdrawn by contract v1.2.7 (#194), which also narrows §13's pattern. The access rule survives with a new justification — the uniformity of the activation surface — and `-count=1` becomes the sole cache-correctness mechanism. Detail in **The B2 finding** below, kept because the *method* is the reusable part. |
| B3 | `cmd/go` never propagates a test binary's exit code; a failed test action sets the literal `1`. This is why the config-error distinction lives in `config-error.json` + `78`/`EX_CONFIG` rather than in an exit status. | §8 | **Pinned, and stronger than §8 claims.** §8's note says a `TestMain` calling `os.Exit(2)` at least "produces the text `exit status 2` in the output". Measured: a binary whose tests passed and which then exits `78` leaves no trace of `78` anywhere — `cmd/go` prints `PASS`, then `FAIL` for the package, and exits `1`. CI cannot branch on the code even by scraping the log. |
| B4 | A cache hit skips `TestMain` **entirely**, so a cached package never publishes a shard — which is why `-count=1` is mandatory for reporting runs. | F4, §6 | **Pinned.** Confirmed directly, and it is worse than "no shard": with the gate on and an invalid configuration, a cached package reports `ok (cached)` and exits `0` — no failure, no `config-error.json`, nothing to distinguish a misconfigured reporting run from a healthy green one. |
| B5 | `TestMain`'s post-`m.Run()` code runs on ordinary test failure, including a panic recovered by the testing package; only abrupt termination (F5) bypasses it. | §8 row 2, F5 | **Pinned** — `TestAFailingPackageStillPublishesACompleteShard` runs the fixtures with both a failing and a panicking spec, asserts `go test` goes red, and asserts both shards exist and both record the failure. |

### C. Paths and names

| # | Claim | Source | Status |
|---|---|---|---|
| C1 | Go's `os.fixLongPath` transparently rewrites to the extended-length `\\?\` form near 248 bytes, including for relative paths, so shard creation does not fail at Windows' 260-character total-path limit. | §5, §10 | **Declared** — a claim about the Go standard library, relied on only to justify *not* adding a long-path opt-in. Nothing in go-specs branches on it. |
| C2 | The `\\?\` form lifts the 260-character **total path** limit but not NTFS's 255-character per-**component** limit. | §5 | **Declared** — recorded to stop the common over-reading. The 117-byte filename budget is comfortably inside it, and C3 pins the budget, which is the part we control. |
| C3 | The assembled filename is bounded at ≤117 bytes: ≤40 prefix + 2 + 64 + 11. | §5 *Shard filename* | **Pinned** — `TestShardFileNameStaysInsideItsByteBudget` over eight adversarial paths (very long, non-ASCII, all-unsafe bytes, and three that put an escape exactly on the 40-byte seam) asserting ≤117 bytes and no split `%XY`; `TestBoundedPrefixBacksOffToASafeBoundary` asserts the back-off never costs more than two bytes. |
| C4 | The sanitization `/` → `_` is **not injective**: `foo/bar` and `foo_bar` collide. Identity is carried solely by the SHA-256 of the original, unsanitized import path. | §5, §10, #145 hardening 1 | **Pinned** — `TestShardFileNameSeparatesCollidingSanitizations` at the unit level, and `TestTwoPackagesWithCollidingPrefixesProduceTwoShards` end to end, which also asserts both envelopes report their own package. |

### D. Identity and ownership

| # | Claim | Source | Status |
|---|---|---|---|
| D1 | A layer between the generator and the test binary can case-fold the token (a shell, a CI secret store, a Windows environment round-trip), so the digest must be taken over the token's **decoded bytes**, never its hex text. | §5 *Run marker* | **Pinned** — `TestHashRunTokenHashesDecodedBytesNotHexText` (which also asserts the wrong one-liner's digest is *not* produced) and `TestVerifyRunOwnershipAcceptsACaseFoldedToken` end to end. Mutation-checked: hashing the hex text fails both. |
| D2 | `GO_SPECS_RUN_ID` derivation collides in real CI: `GITHUB_RUN_ID` is stable across re-runs, and `run_id`+`run_attempt` is shared by every leg of a matrix; `strategy.job-index` alone is unique only within one matrix. | §5 *Run marker lifecycle* | **Declared (documentation)** — a claim about CI platforms, unobservable from this repository's tests. Its enforcement is A4: a colliding `RunID` fails closed at marker creation regardless of how it was derived. |
| D3 | The token is readable by every process in the test's process tree and by anything reading `/proc/<pid>/environ` as the same user — it is an accident-detection mechanism, never a security boundary. | §10 | **Declared** — a property of the environment as transport. Nothing may treat a matching token as an authorization decision; that is a review constraint, not a testable behaviour. |

### E. Symlink and permission hardening

| # | Claim | Source | Status |
|---|---|---|---|
| E1 | `O_NOFOLLOW` causes an open to **fail** rather than write through a pre-planted symlink, on every open of `run.json`, `config-error.json`, shard temp files and shard final names, on both the read and the write side. | §10 rule 1 | **Pinned at one site of four, and the other three cannot be pinned for it.** `TestVerifyRunOwnershipRefusesToFollowASymlinkedMarker` is the real pin, mutation-checked: dropping `O_NOFOLLOW` makes it accept a symlinked `run.json` pointing at a marker that would otherwise verify. The three write sites — `TestInitializeRunRefusesToWriteThroughASymlinkedMarker`, `TestWriteAndPublishRefusesASymlinkedTempPath` and `TestWriteConfigErrorRefusesASymlinkedTempPath` — all pass with the flag removed, because `O_CREATE|O_EXCL` already fails on an existing path. That is not a coverage gap to close: at those sites `O_NOFOLLOW` is redundant by construction and no test can distinguish it. Removing it from the read site is a real regression; removing it from a write site is merely pointless. |
| E2 | `umask` does not apply to a subsequent `Chmod`, which is why a directory this implementation creates must have its mode **verified after creation** rather than assumed from the requested `0700`. | §10 rule 3 | **Pinned for the case that can fail; the case this row described cannot.** `TestPreExistingRunDirectoryWithAWiderModeIsRefused` pins the part that matters — a directory this implementation did not create is refused, not silently tightened. The umask test this row asked for is not achievable: `umask` can only clear bits, and the only bits it could clear from `0700` are owner bits, which break `MkdirAll` before the `Chmod` is ever reached. `TestCreatedDirectoriesGetModeSevenHundredRegardlessOfUmask` therefore asserts the mode but cannot be failed by any umask. Stated rather than left implying a pin that does not exist. |
| E3 | A group- or other-writable ancestor of the base directory lets another local user swap a path component, making every downstream check meaningless — so operation must be refused, fail-closed, naming the offending directory and its mode. | §10 rule 2 | **Pinned** — `TestReportingRefusesAForeignWritableAncestor` (refused by both `InitializeRun` and the producer, message names the directory and `0777`), `TestStickyAncestorsAreAccepted` for the exemption, and `TestAForeignWritableAncestorIsFoundThroughASymlink` for the lexical-walk bypass found in review. |

## The B1 pin

B1 was first written as two tests, and neither one pinned it. The sequence is worth keeping,
because each failure mode looks exactly like coverage.

**Attempt 1 — the process-level cache test the contract used to ask for.**
`TestGateOffIsCacheStableAcrossEveryCoordinationVariable` varied `GO_SPECS_RUN_ID` and
`GO_SPECS_REPORT_DIR` across two gate-off runs and asserted `(cached)`. It passed. It also passed
with `os.Getenv` everywhere, because the reads happen in `TestMain` where the test log does not
exist yet — see *The B2 finding*. Vacuous. Contract v1.2.7 §5 withdraws it and forbids
reintroducing it, and it has been **deleted** from this branch rather than relabelled: a test that
cannot fail is worse than no test, because it occupies the space where a real one would go.

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

**This is not the test v1.2.7 forbids, and the difference is the whole point.** The withdrawn one
reads from `TestMain`, where neither access path reaches the cache key, so it cannot fail. This one
reads from inside a test function, where they are distinguishable, and it has been shown to fail
when `envread.Scan` is rewritten as `os.LookupEnv`. §5 requires the seam assertion as the pin and
this branch has it; this test exists because the seam assertion alone does **not** catch that
rewrite — it pins which method the resolver calls, not what the method does. That gap was found by
adversarial review of this branch, not by reasoning about it.

Since then the status has changed, and the passage above should be read as history rather than as
a justification: contract v1.2.8 §5 folded this gap into the contract, so the body pin and its
control arm are **required**, not tolerated. What began here as a defence of an extra test is now
the rule.

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

## How this document is kept true

This inventory was written **before** the implementation, so its Status column originally recorded
a *plan*: eleven rows said "Required — not pinned today". That is the right state for a planning
document and the wrong state for a register. After #145 shipped, the Summary below was updated and
the table was not, so the document contradicted itself — the Summary called a claim pinned while
its own row still said it was not.

That was found in review, not by its author, and it is the third instance in this work of one
failure: a statement settled enough to be used as a premise, by someone who had not checked it. The
first was the contract's cache-enrollment claim; the second was v1.2.7 asserting that the seam test
"asserts the rule exactly".

**The reconciliation was done by reading each test against its row**, not by matching names. That
distinction earned its cost twice:

- **E1** looked pinned by four symlink tests. Only one of them actually depends on `O_NOFOLLOW`; the
  three write sites pass with the flag removed, because `O_CREATE|O_EXCL` already fails on an
  existing path. Name-matching would have recorded four pins where there is one.
- **E2** looked unpinned and is partly unpinnable: the umask test its row demanded cannot be
  written, because `umask` can only clear bits that also break `MkdirAll` before the `Chmod` runs.

**The rule going forward:** a row's Status names the test that fails when the claim stops holding,
or says plainly that nothing does. "A test exists whose name mentions this" is not a status. When a
claim's status changes, this table changes in the same commit — the contract's §13 points here
precisely so there is one register rather than two that must agree.

## Summary

- **Pinned:** A1, A2, A3, A4, A6, B1, B3, B4, B5, C3, C4, D1, E3, and E1 at its one distinguishable
  site.
- **Mutation-checked**, meaning the test was *observed* to fail when the behaviour is removed:
  A1/A2 (publish via `os.Rename`), B1 (`Scan` rewritten as `os.LookupEnv`), D1 (`HashRunToken` over
  the hex text), E1 (`O_NOFOLLOW` dropped from the read site). A pin never seen to fail is a claim,
  not a pin.
- **Partly unpinnable, and said so rather than rounded up:** E1 at its three write sites, where
  `O_EXCL` makes the flag redundant and no test can distinguish it; E2's created-directory mode,
  where no umask can falsify the assertion.
- **Known gap:** A7. The source-level guard that row proposed — a check that the package never calls
  `os.Rename` — was never implemented. The behaviour is covered indirectly by the duplicate and
  concurrency tests, which is why it has not bitten, but the row says the guard is absent.
- **Declared, deliberately untested:** A5, C1, C2, D2, D3.
- **Refuted:** B2 — see *The B2 finding*. The contract amendment landed as v1.2.7 (#194); the
  implementation needed no change, because `-count=1` was already mandatory.

The two additions are the inventory earning its cost. Neither is exotic: each is a place where the
contract is internally consistent, the code would look correct, and the failure would live entirely
in what the toolchain or the kernel does underneath.
