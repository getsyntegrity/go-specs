# REPORT-002A: Native multi-package reporting coordination contract

Contract version: **v1.2.5** (amended by #148)
Status: amended (design gate for #143; unblocks #145, #146)
Scope: activation, completion barrier, run identity/filesystem lifecycle, cache semantics, exit
semantics, and coverage-merge responsibilities for module-wide `go test ./...` reporting. Does
**not** implement shard emission (#145) or merge/render (#146), and does not change
`NormalizedReport` or any renderer from #142.

Downstream issues and code comments **must cite the contract version plus the section**
(for example "contract v1.2.5 §8"), never a bare section number: sections are amended in place,
so `§8` alone resolves to different normative text depending on when it was read.

### Changelog

| Version | PR | Change |
|---|---|---|
| v1.0 | #147 | Original contract: activation, completion barrier, shard filesystem layout, cache/exit/coverage semantics. |
| v1.1 | #148 (initial) | Added the run-ownership marker (`GO_SPECS_RUN_TOKEN` + exclusively-created `run.json`), a collision-safe shard filename carrying the SHA-256 of the import path, and an invalid-configuration row in §8. |
| v1.2.5 | #148 (review follow-up 5) | Resolves an ambiguity in §10: the local-filesystem requirement is a **declaration, not an enforcement** — no network-mount detection is required or should be implemented, because portable detection does not exist (`statfs` `f_type`, `getmntinfo`, `GetDriveType` disagree and are incomplete against overlay and bind mounts) and a check that reports "local" for an NFS-backed bind mount is worse than none. §10 now states the cost plainly: a `run.json` `O_EXCL` failure on a network mount is unmitigated and unrecoverable by design, while the `link` false-`EEXIST` case is mitigated by the `st_nlink` recipe — making that branch the contract's only network-filesystem defence, and therefore a required test rather than an optional one (§13). |
| v1.2.4 | #148 (review follow-up 4) | Scope fixes on v1.2.3's own rule: the disabled-path `os.Environ()` requirement covers **every** variable this contract introduces (`GO_SPECS_REPORT_DIR` included — a realistic per-invocation value, and the easy one to miss), with later additions inheriting it by default; the cache-stability test must vary at least two variables, since a run-ID-only test passes while another variable still leaks through `os.Getenv`. Adds the macOS `shasum -a 256` form to §7, and a §13 planning item asking #145 to enumerate normative claims that rest on unpinned runtime behaviour (network-mount create-no-replace and the `link`/`st_nlink` fallback being the two currently unpinned). |
| v1.2.3 | #148 (review follow-up 3) | Closes a regression introduced by v1.2.1's own warn-only diagnostic: reading the identity variables with `os.Getenv` on the **disabled** path enrolls them in the test-cache key (`os.Getenv`/`os.LookupEnv` call `testlog.Getenv`; `os.Environ` does not), so a stale per-invocation `GO_SPECS_RUN_ID` would defeat caching for every package on every run while producing no reporting at all. §5 now requires an `os.Environ()` scan on that path, explains why the unusual access pattern is load-bearing, and mandates a cache-stability regression test. Also: the GitHub Actions `RunID` recipe gains `github.job`, since `strategy.job-index` is unique only within one matrix and two job definitions both yield index `0`; §7 records the `xxd -r -p` step needed to reproduce the token digest by hand; digest-of-decoded-bytes wording made consistent across §3, §5 and §7. |
| v1.2.2 | #148 (review follow-up 2) | Correctness fixes, no design change: the run-token digest is taken over the token's **decoded bytes**, not its hex text (the pattern accepts either case, so `AB…` and `ab…` would otherwise hash differently despite being byte-identical); §3 and the partial-combination table restate gate-off behaviour as "read and checked for well-formedness, never acted on" rather than "unread", matching the warn-only diagnostic; the GitHub Actions `RunID` recipe gains the matrix leg (`GITHUB_RUN_ID` + `GITHUB_RUN_ATTEMPT` are shared by every matrix leg) and §5 states normatively that a run is scoped to one job, never a whole matrix; the `st_nlink == 2` inference's two preconditions are made explicit; an NTFS per-component note records that `\\?\` lifts `MAX_PATH` but not the 255-character component limit. |
| v1.2.1 | #148 (review follow-up) | Corrections from a second review pass, no design change: the run-token pattern becomes `^([0-9a-fA-F]{2}){16,64}$` (the previous form admitted odd, undecodable lengths); the shard filename is described as **bounded** at 117 bytes rather than fixed, with the arithmetic tabulated; §8's correction note states the `cmd/go` mechanism precisely (a child's exit code is never propagated — `base.SetExitStatus` keeps a maximum but only ever receives the constant `1`); §5's Windows rationale corrected (Go's `os.fixLongPath` handles `MAX_PATH` itself, so the real constraint is consumer tooling); `renameat2`/`RENAME_NOREPLACE` downgraded from "alternative" to "needs a mandatory `link` fallback"; added the `link`-over-NFS false-`EEXIST` recipe; finalize-on-red extended to cancellation/timeout paths; per-CI `RunID` recipes for GitHub Actions and GitLab; gate-off warn-only stderr validation added so typo detection survives without the power to fail a run. |
| v1.2 | #148 (initial revision) | **Normative changes**: §8's distinct configuration-failure exit code no longer comes from the test binary — a pre-test failure inside a package binary writes a `config-error.json` record and the preflight/finalize layer maps it to `78` (`EX_CONFIG`); §5 replaces `os.Rename` and the pre-rename existence check with a mandatory atomic create-no-replace publish; §5 bounds the readable shard-name prefix to 40 bytes; §3/§5 add `GO_SPECS_REPORT_SHARDS` as the explicit activation switch and define the partial-variable cases; §10 sets a `GO_SPECS_RUN_TOKEN` entropy floor and reframes it as an ownership, not a security, mechanism; §5 defines `run.json` lifecycle/uniqueness and cleanup; §5/§9 define the expected-producer set and its exclusions; §5/§10 add shard-payload ownership binding, symlink/permission hardening, and the NFS/CIFS `O_EXCL` caveat. |

**Amendment note (v1.1)**: §3, §5, §7, §8, and §10 were revised after an adversarial re-read
surfaced three gaps between this contract and the hardening requirements now recorded on
#145/#146: sanitized shard filenames were not collision-free, invalid-configuration exit semantics
were undefined, and `GO_SPECS_RUN_ID` reuse across independent invocations was indistinguishable
from a legitimate second producer of the same run. This amendment closes all three with a
run-ownership marker (`GO_SPECS_RUN_TOKEN` + an exclusively-created `run.json`) and a
collision-safe shard filename. It does not reopen or revert #144/#147; it supersedes the affected
passages in place.

**Amendment note (v1.2)**: a second adversarial review of the v1.1 text found one normatively
false claim about the Go toolchain and several under-specified points that would have forced
hidden architectural decisions into #145/#146. v1.2 corrects them in place, in the same scope:
documentation only, still no reopening or reverting of #144/#147. The corrections are listed in
the changelog row above; the one that changes an already-stated promise is §8's exit-code
carrier, which moved out of the test binary entirely (see §8 and the note there).

## 1. Design-space findings

All findings below were produced by running real `go test` invocations against scratch packages
under `/tmp/go144-experiments/modroot` (module `example.com/multipkg`, Go 1.27.0 darwin/arm64),
not recalled from memory. Exact commands are cited per finding.

### F1 — Forwarded custom test flags break packages that don't register them

```
$ go test ./... -args -goSpecsReport=xml:out.xml
ok      example.com/multipkg/pkgA   0.720s
flag provided but not defined: -goSpecsReport
FAIL    example.com/multipkg/pkgB   0.462s
FAIL
```

`pkgA` registered `-goSpecsReport` via `flag.String` in its own file; `pkgB` did not. `go test`
builds one binary per package and each binary parses `os.Args` (via `testing.M.Run`'s internal
`flag.Parse()`) independently — a flag forwarded through `-args` is not "unknown flags are
ignored," it is a hard parse failure in every binary that didn't register it. **A forwarded
custom flag cannot be the module-wide activation signal** unless every package in the module
registers it, which contradicts "module-wide without modifying participating packages."

### F2 — An environment variable is invisible to non-participating packages

```
$ GO_SPECS_REPORT=xml:out.xml go test ./...
ok      example.com/multipkg/pkgA   0.254s
ok      example.com/multipkg/pkgB   0.516s
```

Same two packages, env var set instead of a forwarded flag: both pass, `pkgB` (which never reads
the var) is completely unaffected. Env vars are inherited by every child process `go test`
spawns regardless of whether that process's code ever reads them — the only mechanism in this
comparison set with zero cost to non-participating packages.

### F3 — No package process can observe its peers; only the invoking command can

```
$ go test ./pkgC/... ./pkgD/... -p 2 -v | grep pid=
pkgC pid= 71397 start= ...05.040875-03:00
pkgC pid= 71397 end=   ...05.343739-03:00
pkgD pid= 71399 start= ...05.310691-03:00   # starts before pkgC ends
pkgD pid= 71399 end=   ...05.613720-03:00
```

Two distinct PIDs with overlapping lifetimes, launched under `go test`'s default parallelism
(`-p` defaults to `GOMAXPROCS`, so this is the *default* behavior on multi-core CI, not an
opt-in). Neither binary is a parent/child of the other and neither has a channel to ask "has my
sibling exited." The only process that structurally knows when every package binary has exited
is the `go test` command itself (it must wait for all of them to return before it can exit) —
and by extension, whatever invoked `go test` and is now looking at its own exit status.

### F4 — A cache hit skips `TestMain` entirely; nothing after `m.Run()` executes

```
$ go test ./pkgA/... -v   # first run
pkgA TestMain RUNNING at ...16:01:39.122944...
ok  example.com/multipkg/pkgA  0.302s
$ go test ./pkgA/... -v   # second run, identical inputs
pkgA TestMain RUNNING at ...16:01:39.122944...   # same timestamp — replayed log, not re-executed
ok  example.com/multipkg/pkgA  (cached)
$ go test ./pkgA/... -count=1 -v   # forces re-run
pkgA TestMain RUNNING at ...16:01:39.614717...   # fresh timestamp
ok  example.com/multipkg/pkgA  0.291s
```

On a cache hit, `go test` never re-executes the binary; it replays the previous run's captured
log. A `TestMain` that would write a shard for run *R* does not run at all on a cache hit for a
different run *R′* — the package is simply silent for *R′*, indistinguishable at the filesystem
level from "still running" or "crashed before writing." Reporting is fundamentally incompatible
with cached test results unless the run forces execution.

### F5 — Abrupt process termination bypasses any code after `m.Run()`

```
$ go test ./pkgA/... -timeout=1s   # TestSlow sleeps 3s
... panic: test timed out after 1s ...
FAIL    example.com/multipkg/pkgA   1.485s
```

`"FLUSH-MARKER-RAN"` (printed by code immediately after `m.Run()` returns) never appears. A
`-timeout` terminates the test binary through the testing runtime's timeout path without returning
control to the caller's post-`m.Run()` code. External `SIGKILL`, an OOM kill, and an unrecovered
panic in a goroutine outside the testing package's recovery boundary likewise terminate the
process before that code can run. This must not be generalized to an ordinary panic inside a
test: the testing package normally recovers that panic, marks the test failed, and allows
`m.Run()` to return. Any finalization logic after `m.Run()` is therefore available after ordinary
test failures (including recovered test panics), but not after abrupt process termination.

### F6 — The combined `-coverprofile` file is written once, by the `go` command, after everything finishes

```
$ go test ./... -coverprofile=cover.out -coverpkg=./...
# file does not exist until the command as a whole completes; then contains one merged profile
```

No package binary writes a fragment of this file — `go test` collects each binary's internal
coverage counters and writes the single combined file itself only once the whole invocation is
done. A package-local process genuinely cannot know, or contribute directly to, this file's
final content.

### F7 — `-coverpkg` overlap produces duplicate raw block lines that Go's own tooling merges, but `report.ParseCoverageProfile` does not

Built `pkgE` (imports `pkgF`) and `pkgF` (tested directly), both instrumented via
`-coverpkg=./pkgE/...,./pkgF/...`:

```
$ go test ./pkgE/... ./pkgF/... -coverpkg=./pkgE/...,./pkgF/... -coverprofile=cover6.out
$ cat cover6.out
mode: set
example.com/multipkg/pkgE/e.go:6.2,7.1 1 0
example.com/multipkg/pkgF/f.go:4.2,5.1 1 1
example.com/multipkg/pkgF/f.go:8.2,9.1 1 0
example.com/multipkg/pkgE/e.go:6.2,7.1 1 1   # same block as line 2, different count
example.com/multipkg/pkgF/f.go:4.2,5.1 1 0   # same block as line 3, different count
example.com/multipkg/pkgF/f.go:8.2,9.1 1 1   # same block as line 4, different count
$ go tool cover -func=cover6.out
example.com/multipkg/pkgE/e.go:5:  DoSub  100.0%
example.com/multipkg/pkgF/f.go:3:  Add    100.0%
example.com/multipkg/pkgF/f.go:7:  Sub    100.0%
total:               (statements)  100.0%
```

`pkgE`'s binary and `pkgF`'s binary both instrument the shared dependency, so the combined
profile has **two entries per shared block**, not one — `go test`'s file-level combination is a
raw concatenation, not a merge. `go tool cover -func` produces the *correct* answer (3
statements, 100%) because `golang.org/x/tools/cover`'s profile loader merges duplicate blocks
keyed by `(file, start, end)`: `mode: set` counts are OR'd, `count`/`atomic` counts are summed.

Read against this repo's `report/coverage.go` (`ParseCoverageProfile`, lines 45–58): it has **no
such keying**. It accumulates `pc.Total += numStmt` and conditionally `pc.Covered += numStmt` for
*every line it reads*, unconditionally. Fed `cover6.out` today, it would report `Total = 6`
statements (double-counted) instead of the correct 3, and `Covered` would depend on line order
rather than "is this block covered by *any* contributing binary." **This is a real, currently
unhandled gap** in already-merged code, triggered specifically by the `-coverpkg` overlap
scenario #143 calls out — not a hypothetical. It must be closed by #146 (see §9).

### F8 — There is no free, Go-provided, shared per-invocation identifier

```
$ go test ./pkgA/... ./pkgB/... -coverprofile=covx.out -v | grep GOCOVERDIR
pkgA GOCOVERDIR= .../go-build462199080/b001/gocoverdir
pkgB GOCOVERDIR= .../go-build462199080/b148/gocoverdir
```

`GOCOVERDIR` exists only when `-coverprofile`/`-cover` is used, and is a **per-package build
directory**, different for every binary in the same invocation — it cannot serve as a shared run
identifier. No other env var is both automatically set and shared identically across every
package binary of one `go test ./...` invocation. A run identifier must be generated externally
(by whatever invoked `go test`) and exported before the command runs.

## 2. Rejected alternatives (with reasons)

| Alternative | Rejected because |
|---|---|
| Forwarded custom test flag (`-args -go-specs.report=...`) as the *module-wide* activation signal | F1: hard `flag provided but not defined` failure in every non-participating package's binary. Still fine as a **per-package** convenience once that package already links go-specs (unchanged from #142), just not usable as the cross-package coordination trigger. |
| "Last writer wins" — whichever package process finishes last performs the merge | F3: no package can know it is last; overlapping lifetimes are the default (`-p`≥2), not an edge case. Rejected per #143's explicit invariant. |
| Lock-file / counter that increments per package and triggers merge at N | Requires knowing N in advance and reliably decrementing on every exit path, including the F5 abrupt-termination cases where no code runs after `m.Run()`. A counter can't observe a process that never got to decrement it — indistinguishable from "still running." |
| Stale-directory heuristic ("no shard written in the last Xs ⇒ assume done") | Race-prone by construction (a slow package looks identical to a finished run) and explicitly rejected by #143 and by #144's acceptance criteria. |
| `TestMain`-only coordination with no external step (some in-process signal after all binaries exit) | F3 proves no such signal exists inside the process model; only the `go` command's own exit is authoritative. |
| Rely on `GOCOVERDIR` or any Go-generated value as the shared run ID | F8: it's per-package-build-directory, not shared, and only exists under `-coverprofile`. |
| Each shard independently computes/embeds its own coverage percentage | Directly contradicts #141 ("a package-local process must not claim to know the module-wide total"); also F7 shows per-shard coverage numbers can't be correctly deduplicated without seeing every other shard's blocks first — that information only exists once, in the single combined `-coverprofile` file (F6). |

## 3. Recommended lifecycle

1. **Before `go test` runs**, the invoking script/CI step turns reporting on explicitly by
   exporting `GO_SPECS_REPORT_SHARDS=1` (§5), and generates a run identifier (`GO_SPECS_RUN_ID`,
   a readable logical name — build number, branch+timestamp, etc.) and, separately, a random
   ownership token (`GO_SPECS_RUN_TOKEN`, ≥16 bytes of `crypto/rand` output, hex-encoded, once per
   invocation — §10), alongside an optional shard base directory (`GO_SPECS_REPORT_DIR`, default
   `.go-specs/runs`). All of them are exported before `go test` runs. This is the *only* place
   these values can originate (F8) — go-specs itself cannot manufacture a value multiple
   independently-launched binaries would agree on. `GO_SPECS_RUN_ID` alone cannot do the ownership
   job: two independent invocations that happen to reuse the same logical run ID (a flaky
   generator, a retried CI step with a stable build number, a copy-pasted script) are
   indistinguishable from each other, and from a legitimate second producer of the same run, using
   the ID alone — every process in both invocations would present the identical
   `GO_SPECS_RUN_ID`. The random, per-invocation `GO_SPECS_RUN_TOKEN` is the only thing that lets
   ownership be verified rather than assumed.

   `GO_SPECS_REPORT_SHARDS` is a separate switch from `GO_SPECS_RUN_ID` on purpose. Activation
   MUST NOT be inferred from the mere presence of a run identifier: a `GO_SPECS_RUN_ID` left
   exported in a developer's interactive shell would otherwise hard-fail (step 3) every
   subsequent `go test` in that shell with zero tests executed — report generation falsifying a
   test result, which §8's own principle forbids. Reporting is off unless it was explicitly
   turned on for that invocation.
2. **Still before `go test` runs**, the same step calls `InitializeRun` (library call or a thin
   `go-specs-report init` CLI), which exclusively creates the run marker
   `<GO_SPECS_REPORT_DIR>/<run-id>/run.json` (§5) recording the run's schema version, `RunID`, and
   the hex SHA-256 digest of `RunToken`'s **decoded bytes** (§5). Exclusive creation (fails if the file already exists) is
   what turns "two invocations reused the same `RunID`" into an immediate, loud, pre-test error
   instead of a silent race to write into the same directory. `RunID` therefore **MUST be unique
   per invocation**, including across reruns of the same CI job (§5, *Run marker lifecycle*). If
   `InitializeRun` fails because the marker already exists, the invoking script must not proceed
   to run `go test` with reporting enabled (§8); `InitializeRun` runs in the preflight command,
   which owns its own exit status, so this failure is directly observable as a distinct exit code
   (§8).
3. **`go test ./... -count=1 ...`** runs exactly as today. Every package that has already wired
   go-specs into its own `TestMain` (unchanged requirement — see §10) reads
   `GO_SPECS_REPORT_SHARDS`, `GO_SPECS_RUN_ID` and `GO_SPECS_RUN_TOKEN` from its environment
   (F2: harmless to packages that don't; §5 requires these reads to go through the environment
   so the `go test` cache treats them as inputs). If `GO_SPECS_REPORT_SHARDS` is absent or false,
   shard emission is disabled and a stale `GO_SPECS_RUN_ID` or `GO_SPECS_RUN_TOKEN` in the
   environment is **inert: read and checked for well-formedness, but never acted on**. "Inert"
   is the precise claim — no effect on exit status, on the filesystem, or on coordination — not
   "unread". A malformed value still produces the warn-only stderr diagnostic (§5), which is why
   it must be read at all. If it is true, the run
   ID and the token must both be present and valid, and the package must be able to confirm,
   against the run marker, that its token matches the marker's stored ownership hash (§5); any of
   those checks failing is a configuration error that must fail loudly, before `m.Run()`, and must
   not continue as though reporting were disabled or degrade to a best-effort write. Because a
   package binary cannot give `go test` a distinguishable exit code (§8), that failure path must
   first write a `config-error.json` record (§5) so the preflight/finalize layer can surface the
   cause. Once ownership is confirmed, that package's `TestMain` writes one shard file after its
   own `m.Run()` returns, via temp-write + fsync + **atomic create-no-replace publish** (§5).
   Packages that never link go-specs do nothing and are never broken by the env vars being
   present (F1 vs F2).
4. **`go test` returns.** Its exit code reflects test results, and nothing else — a pre-test
   configuration/ownership failure detected in step 3 surfaces only as the toolchain's ordinary
   failure status — `cmd/go` reports the package `FAIL` and never propagates the binary's own
   exit code — so it is *not* independently distinguishable there (§8). The distinction is
   carried by the `config-error.json` record, not by `go test`'s exit code.
5. **After `go test` returns**, the same script/CI step — only if it wants a merged module report
   — runs a separate, explicit finalization command/library call
   (`report.Finalize(ctx, opts)` or a thin `go-specs-report finalize` CLI), passing the same run
   ID and token, the shard base dir, the authoritative expected-producer manifest supplied by the
   invoker, and (if used) the path to the single combined `-coverprofile` file. This step is the
   only thing in the system entitled to read shards, merge them, and render the four #142 formats
   module-wide. It MUST be run **even when `go test` failed**, because it is the layer that
   detects a `config-error.json` record and reports it with a distinct exit code (§8); skipping
   finalize on a red `go test` would hide exactly the misconfiguration this contract exists to
   make loud.
6. Finalize reads the run marker first and confirms `RunToken` ownership before touching any
   shard; a missing or mismatched marker is a fail-closed error (§8), not a partial report.
   Finalize then discovers the run's shard directory, accepts only complete (atomically-renamed)
   and valid shards, checks them against the authoritative expected-producer manifest, merges
   execution data deterministically, merges coverage by deduplicating blocks from the single
   combined profile (§9), and renders XML/HTML/TXT/JSON through the existing #142 renderers,
   unchanged.
7. Finalize's own exit code is independent of `go test`'s (§8) and finalize is itself responsible
   for shard-directory cleanup on success (§5) — no package process deletes anything.

Steps 5–7 are optional and separate from steps 1–3: skipping them leaves `go test` fully
functional and native, exactly as #141 requires. Finalization is an "optional
orchestration/finalization step," not a replacement test runner — the user's canonical command is
still, verbatim, `go test ./...`. Steps 1–2 (run ID, token, and marker) are the minimum
pre-flight cost of enabling reporting at all; they run once, outside `go test`, and touch nothing
inside it.

## 4. Event/process sequence

```mermaid
sequenceDiagram
    participant CI as Invoking script / CI step
    participant FS as Shard directory (run R)
    participant GT as go test (parent)
    participant PA as pkgA test binary
    participant PB as pkgB test binary
    participant FIN as go-specs finalize

    CI->>CI: generate unique RunID R, RunToken T (>=16 bytes crypto/rand, hex)
    CI->>FS: InitializeRun: exclusively create run.json {R, sha256hex(T)}
    alt run.json already exists (RunID reused)
        FS-->>CI: exclusive create fails
        CI->>CI: abort before running go test; preflight exits 78 (EX_CONFIG, §8)
    end
    CI->>GT: go test ./... -count=1 -coverprofile=cover.out (env: GO_SPECS_REPORT_SHARDS=1, GO_SPECS_RUN_ID=R, GO_SPECS_RUN_TOKEN=T)
    par package binaries (independent processes, F3)
        GT->>PA: exec
        PA->>FS: read run.json, constant-time compare sha256hex(T)
        alt ownership/config check fails (pre-test)
            PA->>FS: write <base>/<R>/config-error.json (create-no-replace)
            PA-->>GT: fail before m.Run(); go test reports pkg FAIL; child exit code not propagated
        else ownership confirmed
            PA->>PA: m.Run()
            PA->>FS: write .tmp, fsync, close, atomic create-no-replace publish -> prefix(max 40B)--sha256(pkgPath).shard.json
            PA-->>GT: exit(code)
        end
    and
        GT->>PB: exec
        PB->>FS: read run.json, constant-time compare sha256hex(T)
        PB->>PB: m.Run()
        PB->>FS: write .tmp, fsync, close, atomic create-no-replace publish -> prefix(max 40B)--sha256(pkgPath).shard.json
        PB-->>GT: exit(code)
    end
    GT->>GT: merge combined coverage profile (F6), write cover.out
    GT-->>CI: exit(test_rc)
    Note over CI,FIN: finalize runs even when test_rc != 0 — it is the layer that surfaces config-error.json
    CI->>FIN: go-specs-report finalize --run-id=R --run-token=T --coverprofile=cover.out
    FIN->>FS: read run.json, constant-time compare sha256hex(T) (fail-closed otherwise)
    alt config-error.json present
        FS-->>FIN: pre-test configuration failure record
        FIN-->>CI: exit(78) without merging or rendering (§8)
    end
    FIN->>FS: list *.shard.json only (never *.tmp-*)
    FIN->>FIN: verify each shard's RunID + token hash, filename/envelope identity, dedupe coverage blocks (F7), merge, render XML/HTML/TXT/JSON
    FIN-->>CI: exit(finalize_rc)
    CI->>CI: report test_rc and finalize_rc independently
```

## 5. Environment and filesystem contract

Shard emission is **off unless explicitly switched on**. `GO_SPECS_REPORT_SHARDS` is the single
activation gate; when it is absent or false, no other reporting variable is read, validated, or
allowed to fail a test run. When it *is* on, the invocation has explicitly asked for
coordination, so an invalid value, a missing/mismatched run marker, or a missing token is a
configuration error and must fail loudly rather than silently degrading to disabled mode:

| Variable | Set by | Meaning |
|---|---|---|
| `GO_SPECS_REPORT_SHARDS` | invoking script/CI, before `go test` | **Activation gate.** `1`/`true` (case-insensitive) ⇒ shard emission requested. Absent, empty, `0`/`false` ⇒ shard emission disabled: no shard is written and no failure can originate from reporting, though an invalid run identity may still be *reported* on stderr (see *Warn-only validation* below). Any other value ⇒ configuration error (an unparseable gate must not be guessed either way). |
| `GO_SPECS_RUN_ID` | invoking script/CI, before `go test` | Readable, logical run identifier, `^[A-Za-z0-9_.-]{1,128}$`, **unique per invocation** (see *Run marker lifecycle*). Identifies the run for humans and for directory naming. Required whenever the gate is on; absent or invalid while the gate is on ⇒ configuration error. **Not, by itself, proof of ownership** — see `GO_SPECS_RUN_TOKEN` — and not, by itself, an activation signal. |
| `GO_SPECS_RUN_TOKEN` | invoking script/CI, before `go test`, once per invocation | Per-invocation ownership nonce: **at least 16 bytes read from `crypto/rand`, hex-encoded**, matching `^([0-9a-fA-F]{2}){16,64}$` (§10). Required whenever the gate is on. This is what distinguishes a legitimate second producer of *this* run from an unrelated invocation that happens to reuse the same `RunID`: both would present an identical `GO_SPECS_RUN_ID`, but only the genuine invocation holds the matching token. Absent or invalid while the gate is on ⇒ configuration error, fails loudly, same as an invalid run ID. |
| `GO_SPECS_REPORT_DIR` | invoking script/CI (optional) | Base directory for run subdirectories. Default `.go-specs/runs`. Must be on a local filesystem (§10). |
| `GO_SPECS_REPORT` | existing, #142 | Unchanged: per-process local format:path target(s), orthogonal to shard emission. |

**Partial configuration (the most likely real-world failure).** The three-variable set is not
independently meaningful, and every partial combination is defined rather than left to the
implementation:

| `GO_SPECS_REPORT_SHARDS` | `GO_SPECS_RUN_ID` | `GO_SPECS_RUN_TOKEN` | Behaviour |
|---|---|---|---|
| off / absent | absent, or present and valid | absent, or present and valid | Disabled and silent. No shard, no diagnostic, **no effect on exit status whatsoever**. This is the case that protects a developer shell with a leftover export. |
| off / absent | present, **invalid** | any | Disabled, with **no effect on exit status, filesystem, or coordination** — but one stderr line naming `GO_SPECS_RUN_ID` (*Warn-only validation* below). The value is read and checked for well-formedness; it is simply never acted on. |
| off / absent | any | present, **invalid** | Disabled, same as the row above, with the diagnostic naming `GO_SPECS_RUN_TOKEN`. Never echo the value. |
| on | present, valid | present, valid | Normal operation: verify ownership against `run.json`, then emit (§3 step 3). |
| on | present, valid | **absent** | Configuration error before `m.Run()`. Message MUST name `GO_SPECS_RUN_TOKEN` as the missing variable and state that it is generated by the same preflight step that created the run marker. |
| on | **absent** | present, valid | Configuration error before `m.Run()`. Message MUST name `GO_SPECS_RUN_ID` as the missing variable. |
| on | absent | absent | Configuration error before `m.Run()`. The gate is an explicit request; an explicit request that carries no run identity is a misconfiguration, never silence. |
| on | present, **invalid** | any | Configuration error before `m.Run()`, naming `GO_SPECS_RUN_ID` and the pattern it violated. |
| on | any | present, **invalid** | Configuration error before `m.Run()`, naming `GO_SPECS_RUN_TOKEN` and the pattern it violated. Never echo the token value itself into the message or the record. |

**Warn-only validation (gate off).** When the gate is off and `GO_SPECS_RUN_ID` or
`GO_SPECS_RUN_TOKEN` is present but does not match its pattern, the process emits **one** line to
stderr naming the variable and the pattern it violated, and then continues exactly as if
reporting were disabled. This path:

- MUST NOT affect the process's exit status, fail the package, or write anything to disk;
- MUST NOT fire when the value is present and *valid* — a correctly-formed leftover export is
  silent, because there is nothing to tell the user;
- MUST be emitted at most once per process, before `m.Run()`, prefixed distinctly (e.g.
  `go-specs: `) so it is attributable and greppable;
- is a diagnostic, not a contract signal: no tooling may key behaviour off it.

The rationale is that "reject a present invalid value" was protecting against a **typo** —
`GO_SPECS_RUN_TOEKN`, a truncated ID pasted into a CI config — and typo detection does not
require the power to fail a test run. Splitting the two keeps the detection and discards the
falsification. The tradeoff, stated plainly: this emits one stderr line per package binary, so a
malformed leftover export in a shell running `go test ./...` across 30 packages prints 30 lines.
That is noisy but harmless, and it is strictly better than the alternative of 30 hard failures;
implementations that find it unacceptable may gate the diagnostic behind the gate variable being
*present* (rather than *true*), which is where a typo'd value realistically comes from.

Every one of these *error* messages MUST name **the exact environment variable** at fault and, when
the likely cause is a leftover export rather than a genuine CI misconfiguration, MUST state the
exact remedy verbatim — for example: `unset GO_SPECS_REPORT_SHARDS` (turn reporting off for this
shell) or `unset GO_SPECS_RUN_ID GO_SPECS_RUN_TOKEN GO_SPECS_REPORT_SHARDS`. "Invalid reporting
configuration" without a variable name is not an acceptable diagnostic: the whole point of the
gate is that the person hitting this is usually a developer who does not know the variable is set.

**Environment, not files, is the transport — and how it is read is normative.** When the gate is
on, producers MUST read run identity through environment variables using `os.Getenv` /
`os.LookupEnv`, and MUST NOT source it from a config file, a build flag, or a compiled-in value.
`go test` records the environment variables a test binary actually reads into its test-cache key,
so an env-var read makes a new `GO_SPECS_RUN_ID` invalidate the cached result for that package; a
value smuggled in by any other route leaves the cache key unchanged, the package is served from
cache, its binary never executes, and it therefore never publishes a shard (F4) — silently, and
indistinguishably from a crash. This is a supporting reason for the `-count=1` requirement in §6,
not a replacement for it: `-count=1` remains mandatory for reporting runs.

**On the disabled path the access pattern inverts, and this is load-bearing.** When the gate is
off, **every** variable this contract introduces — `GO_SPECS_RUN_ID`, `GO_SPECS_RUN_TOKEN` and
`GO_SPECS_REPORT_DIR` — MUST be obtained by scanning `os.Environ()`, and MUST NOT be read with
`os.Getenv` or `os.LookupEnv`. The rule is the **whole variable set, not just the ones the
diagnostic happens to mention**: `GO_SPECS_REPORT_DIR` is the easy one to miss, and it is a
realistic per-invocation value (CI routinely points it at a run-scoped temp directory), so a
single stray `LookupEnv` for it reintroduces the same cache enrollment at a fraction of the
visibility. Any variable added to the table above later inherits this rule by default; an
exception must be argued explicitly, not assumed from silence.

`GO_SPECS_REPORT` (#142) is deliberately outside this rule: it is read by the existing
per-process reporter on its own path, is not part of run coordination, and its semantics are
unchanged by this contract.

The reason is the same cache mechanism, working against us. `cmd/go` derives test-cache validity
from the testlog, which records every `getenv` event the binary emits; `os.Getenv` and
`os.LookupEnv` both call `testlog.Getenv(key)`, while `os.Environ` delegates straight to
`syscall.Environ` and records nothing. So reading these variables with `Getenv` on the disabled
path enrolls them in the inputs ID of **every package in the module**, and a stale or
per-invocation-varying `GO_SPECS_RUN_ID` sitting in a developer's environment then invalidates the
cache for every package on every run — with no reporting produced in exchange, since the gate is
off. Before this contract added the diagnostic, a gate-off run touched nothing and cached
normally. That regression is silent, cumulative, and typically diagnosed months later as "our CI
got slow"; it is strictly worse than the typo it would be paying for.

Two consequences #145 must honour:

- The `os.Environ()` scan is a **deliberate use of an implementation detail** — that testlog
  instrumentation lives in `os.Getenv`/`os.LookupEnv` and not in `os.Environ` — rather than a
  stylistic choice. It is not a documented API guarantee. It MUST therefore carry a comment at
  the call site stating why `Getenv` is forbidden there, or the first person tidying the code
  reintroduces the problem with a change that looks like a pure simplification.
- It MUST be covered by a regression test asserting that a gate-off run is **cache-stable**: run
  a package twice with the gate off and assert the second run reports `(cached)`. A test that
  only checks the diagnostic text will not catch a `Getenv` creeping back in.

  The test MUST vary **at least two** of the contract's variables between the two runs — use
  `GO_SPECS_RUN_ID` *and* `GO_SPECS_REPORT_DIR` — because a test that varies only the run ID
  passes even when some other variable is still read through `os.Getenv`. That is the exact shape
  of the leak this rule exists to prevent, and a one-variable test would certify the codebase
  against it while leaving it open. Ideally the test varies every variable in the set, so adding
  a variable without extending the test is the thing that fails.

The gate variable itself may be read either way: it is stable within an environment, so enrolling
`GO_SPECS_REPORT_SHARDS` in the cache key costs nothing. Only the identity variables vary per
invocation, and only they must avoid the testlog.

Filesystem layout:

```
<GO_SPECS_REPORT_DIR>/<run-id>/
  run.json                                              # ownership marker, §5
  config-error.json                                     # pre-test configuration failure record, §5/§8
  shards/<bounded-sanitized-prefix>--<sha256-of-package-path>.shard.json
```

- **Run marker (`run.json`)**: created exclusively (fails if already present, e.g. `O_CREATE |
  O_EXCL | O_NOFOLLOW`) by `InitializeRun`, *before* `go test` runs (§3 step 2). Contains a marker
  schema version, the `RunID`, and the **lowercase hex SHA-256 digest of `RunToken`'s decoded
  bytes** (never the raw token, and never a digest of its hex text — see below). Every subsequent reader (producer or finalizer) recomputes the digest of its own
  `GO_SPECS_RUN_TOKEN` and compares it to the marker's stored digest using a **constant-time
  comparison** (`crypto/subtle.ConstantTimeCompare`, never `==` on the decoded values).

  **The digest is taken over the token's decoded bytes, never over its hex text.** The token
  pattern accepts either case, so hashing the ASCII string would make `AB…` and `ab…` produce
  different digests and fail verification on two values that are byte-identical once decoded.
  That defect would surface only when some layer between the generator and the test binary
  case-folds the value — which is exactly what a shell, a CI secret store, or a Windows
  environment round-trip can do — making it both rare and extremely hard to diagnose.
  Implementations therefore `hex.DecodeString` the token first and hash the resulting bytes.
  `run.json` stores that digest as lowercase hex, and a reader normalizes both digests to
  lowercase *before* the constant-time compare (normalizing a public digest leaks nothing;
  normalizing the token itself would be a different matter). `GenerateRunToken` MUST emit
  lowercase hex regardless, so the mixed-case path stays a robustness guarantee rather than a
  routine one.

  Verification outcomes:
  - **matching digest** ⇒ legitimate participant in this run (a retry within the same invocation,
    or another package's producer, or the finalizer) — proceed;
  - **marker present with a different digest, or marker absent while the activation gate is on**
    ⇒ `RunID` collision or misconfiguration — fail closed, loudly, immediately; never proceed as
    if reporting were disabled or as if ownership were unverified-but-fine.
  This is the only mechanism in the contract that can actually tell "another producer of the same
  run" apart from "an unrelated invocation that reused the same `RunID`" — `GO_SPECS_RUN_ID` alone
  cannot, because both cases present identical `RunID` values to every process involved.
- **Run marker lifecycle**: the marker is created once by `InitializeRun` and removed only by the
  two explicit paths below. Nothing else — no producer, no test binary, no finalizer running
  mid-`go test` — may delete it.
  - **Primary rule, normative: `RunID` MUST be unique per invocation.** "Per invocation" includes
    reruns: **any** identifier that a rerun reuses is *not* a conforming `RunID`, because every
    rerun would then fail marker creation permanently. The correct derivation is CI-specific, and
    the two major systems differ in a way that is easy to get wrong when copying an example:
    - **GitHub Actions**: `GITHUB_RUN_ID` is **stable across re-runs**, so it must be combined
      with the attempt counter — and **that pair is still not unique per job**. Every leg of a
      matrix shares both `GITHUB_RUN_ID` and `GITHUB_RUN_ATTEMPT`, so
      `${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT}` makes a fan-out of `go test` jobs derive the
      *same* `RunID` — reintroducing precisely the collision this rule forbids. GitHub exposes no
      environment variable for matrix values, so the leg must be interpolated in the workflow
      itself. **`strategy.job-index` alone is not sufficient**: it is unique within *one* matrix,
      not within the workflow, so two separate matrix job definitions both yield index `0` and
      collide again. The conforming recipe includes the job identity as well:

      ```
      ${{ github.run_id }}-${{ github.run_attempt }}-${{ github.job }}-${{ strategy.job-index }}
      ```

      (or the matrix keys spelled out in place of `job-index`). All four parts are load-bearing:
      `run_id` identifies the workflow run, `run_attempt` separates re-runs, `github.job`
      separates job definitions, and `job-index` separates legs within one definition. Drop any
      one and there is a real configuration that collides while looking correct.
    - **GitLab CI**: `CI_JOB_ID` is **already unique per retry** — a retried job is a new job with
      a new ID — so `${CI_JOB_ID}` alone is conforming, and `parallel:matrix` legs get distinct
      job IDs, so the fan-out problem above does not arise. There is no `CI_JOB_ATTEMPT`
      variable; do not port the GitHub shape across.

    **One run per job, not one run per matrix.** A run is scoped to a single `go test`
    invocation on a single machine, and §10 requires the base directory to be on a local
    filesystem — so a run cannot span matrix legs executing on different runners even in
    principle. Each leg is its own run, with its own `RunID`, marker, shard directory and
    finalize step, producing its own module report. This is consistent with the expected-producer
    set being captured per build configuration (§5): a matrix cell that excludes packages by
    build tag has a different expected set, so sharing one run across cells would be wrong on
    two counts, not just one. Combining per-leg reports afterwards is an artifact-collection
    concern for the CI system, outside this contract.

    When in doubt, or on a CI system whose rerun and fan-out semantics are unclear, append random
    bytes and stop reasoning about it. **Why this is the primary
    rule rather than a TTL**: exclusive creation is only fail-closed if reuse is genuinely
    abnormal. Any scheme that makes reuse routine — and then relies on a heuristic to decide
    whether the previous holder is dead — reintroduces exactly the stale-directory ambiguity §2
    already rejected, because a marker left by a killed run is byte-for-byte identical to one held
    by a slow but living run.
  - **Recovery path for genuine reuse** (a retried step that truly must keep the same logical
    `RunID`): an explicit, operator-invoked `go-specs-report init --force` / `InitializeRunOptions.Force`
    removes the existing marker and its shard directory and recreates the marker with the new
    token. It is always explicit, never automatic, never a fallback after a failed create, and it
    MUST refuse to run while the activation gate is on in the same process environment that is
    about to launch `go test` under a *different* token — the force path is a deliberate operator
    action, not a race resolver.
  - **Abandoned markers** (the run was killed, F5) are cleaned up only by the explicit `gc` verb
    already defined under *Cleanup/retention* below, using the retention window as the sole
    staleness rule: a marker is stale iff its own modification time is older than the configured
    retention window, which MUST default to a value comfortably larger than any plausible test
    run (recommend 24h). `gc` is never triggered automatically mid-run, and never by a producer.
  - `InitializeRun` failing because the marker exists is therefore always a real, actionable
    error: either the `RunID` was not unique (fix the generator) or a previous run was abandoned
    (run `gc`, or `--force` if the reuse is intended).
- **Pre-test configuration failure record (`config-error.json`)**: written by a package binary
  when the checks in §3 step 3 fail before `m.Run()`. It is the *carrier of the distinction* that
  the binary's own exit status cannot carry (§8). It is written at `<base>/<run-id>/config-error.json`
  with the same create-no-replace publish protocol as a shard (first writer wins; a second
  failing package finding the record already present does not overwrite it and does not error on
  that account), and contains at minimum:
  - the marker/record schema version;
  - the `RunID` as observed by the failing process;
  - the failing package's import path;
  - a machine-readable `Reason` (`"missing-run-id"`, `"missing-run-token"`, `"invalid-run-id"`,
    `"invalid-run-token"`, `"invalid-activation-gate"`, `"marker-missing"`, `"marker-mismatch"`,
    `"marker-unreadable"`);
  - the human-readable diagnostic, including the exact variable name and remedy required above;
  - an RFC 3339 timestamp.
  It MUST NOT contain the raw `GO_SPECS_RUN_TOKEN`. Its presence is what the preflight/finalize
  layer maps to a distinct exit code (§8). When the base directory or run directory is itself
  unusable (that is what failed), the binary cannot write the record; it still fails loudly with
  the same diagnostic on stderr, and finalize reports the affected producers as missing (§8).
- **Shard filename**: `<bounded-sanitized-prefix>--<sha256-of-package-path>.shard.json`, where
  `sha256-of-package-path` is the full, lowercase hex SHA-256 digest of the **original,
  unsanitized** import path.

  **Normatively: the SHA-256 digest alone carries producer identity. The prefix is readability
  only** — it is never parsed, never compared, never used to look a shard up, and never used to
  reconstruct a package path. Two shard filenames are "for the same package" iff their digests are
  equal, whatever their prefixes say. This is not merely because the sanitization is lossy but
  because it is not injective: `foo/bar` and `foo_bar` both sanitize to `foo_bar`, and would
  produce the same filename under the sanitized-only scheme this section originally specified.

  The prefix is produced by the existing sanitization rule (`/` → `_`, any byte outside
  `[A-Za-z0-9_.-]` percent-escaped) and then **bounded to at most 40 bytes**. Truncation applies
  to the *sanitized* string, which is pure ASCII by construction — every byte outside
  `[A-Za-z0-9_.-]` has already become a `%XY` triplet — so the only rule that actually bites is
  that truncation MUST NOT split a `%XY` triplet: if the 40-byte boundary falls inside one, back
  off to the preceding safe boundary, yielding a prefix of 38–40 bytes. (Stated for completeness:
  truncation must likewise never split a multi-byte UTF-8 sequence, which is vacuous after
  sanitization but matters if an implementation ever bounds the path *before* sanitizing.)

  The resulting filename is therefore **bounded at 117 bytes, not fixed at 117** — nothing is
  padded, and a short import path produces a short prefix. The budget:

  | Component | Bytes |
  |---|---|
  | sanitized prefix | ≤ 40 |
  | `--` separator | 2 |
  | SHA-256 as lowercase hex (32 bytes × 2) | 64 |
  | `.shard.json` suffix | 11 |
  | **total** | **≤ 117** |

  That is well inside the common 255-byte **component** limit even after escaping. Without the
  bound, the fixed 77 bytes of digest, separator and suffix leave only 178 bytes for the prefix,
  and percent-escaping consumes up to three bytes per unsafe byte — so a legal Go import path
  spanning enough directory components would fail to produce a filename even though the package
  itself builds and tests normally. Nothing may parse this filename by byte offset; the prefix is
  variable-length and the digest is located by splitting on the final `--`, or simply by
  recomputing it from the envelope (§9).

  **On Windows, `MAX_PATH` is a downstream-tooling constraint, not a Go one.** Go's `os` package
  handles this itself: `fixLongPath` transparently rewrites a path to the extended-length
  `\\?\` form once it approaches the 248-byte threshold, and it does so for relative paths too by
  joining them with the working directory first — and it skips the rewrite entirely when the
  system already has `LongPathsEnabled`. So shard creation by go-specs will not fail at 260
  characters, and requiring a long-path opt-in would be redundant.

  Noted so nobody re-derives it: the `\\?\` form lifts the 260-character **total path** limit but
  **not** NTFS's 255-character per-*component* limit, which still applies. The 117-byte filename
  budget above is comfortably inside it, so this is a non-issue here — it is recorded only
  because "extended-length paths remove the limits" is a common over-reading, and the next person
  sizing a filename should not have to rediscover which limit survives.

  What *does* break is the
  **consumer** side: archivers, artifact uploaders, CI runners and editors that still assume
  `MAX_PATH` will choke on a deep run directory that Go itself wrote without complaint.
  Implementations MUST therefore still validate the *assembled absolute path* before creating a
  shard and fail with a clear diagnostic naming the total length and the offending components —
  not because the write would fail, but because a silently-created path that downstream tooling
  cannot read is a worse failure, discovered later and further away. For Windows CI, recommend a
  short `GO_SPECS_REPORT_DIR` and a `GO_SPECS_RUN_ID` of ≤32 characters.

  The `ShardEnvelope` (§7) still carries the original, unsanitized `PackagePath`; the finalizer
  recomputes its SHA-256 and rejects any shard whose filename digest does not match the digest of
  its own envelope's `PackagePath` (corrupt or tampered shard, §9/§10).
- **Ownership/permissions**: run directory and shard directory `0700`; `run.json`,
  `config-error.json` and shard files `0600`. Only the invoking user/CI job needs access; test
  output can include stack traces and assertion messages, so least privilege is the safe default
  even on "our own" CI runners. The full filesystem hardening rules — `O_NOFOLLOW`, the
  group/other-writable refusal, and the local-filesystem requirement — are in §10 and are
  normative for #145/#146, not advisory.
- **Write protocol**: write to `<final-name>.tmp-<pid>` inside the *same* shard directory (never
  `/tmp` or another mount — cross-filesystem `rename` is not atomic on POSIX and must be
  documented as a hard constraint on `GO_SPECS_REPORT_DIR`), `Sync()`, `Close()`, then publish via
  an **atomic create-no-replace** operation. A final shard destination that already exists is
  reported as a duplicate-producer error, never silently overwritten.

  **`os.Rename` is not an acceptable publish primitive, and neither is a pre-rename existence
  check.** `os.Rename` replaces an existing destination silently on both supported platform
  families, and "check that the destination is absent, then rename" is a TOCTOU race: two
  producers can both observe an absent destination and the later rename then destroys the earlier
  shard. `InitializeRun`'s exclusive marker creation does not rescue that check — it excludes
  *foreign invocations* from the run directory, it does not serialize producers *inside* the
  legitimate invocation, which is precisely the case a duplicate-producer error must catch. The
  atomicity must come from a single filesystem operation that fails when the destination exists:
  - **Unix**: `link(2)` the temp file to the final name — it fails with `EEXIST` if the name is
    taken — then `unlink(2)` the temp file. This is the baseline, and implementations should
    simply use it.

    `renameat2` with `RENAME_NOREPLACE` is **not** offered as a general alternative. It is not
    merely Linux-specific: support is per-*filesystem*, and the flag returns `EINVAL` or
    `ENOSYS` on filesystems whose `rename` implementation does not handle it — including some
    overlay and network mounts. An implementation that reaches for it must therefore treat
    `EINVAL`/`ENOSYS` as "unsupported here" and fall back to `link`+`unlink`, never as a publish
    failure, and never as licence to fall back to a replacing rename. Given that fallback is
    mandatory anyway, `link`+`unlink` alone is the simpler conforming choice.
  - **Windows**: note explicitly that Go's `os.Rename` maps to `MoveFileEx` **with**
    `MOVEFILE_REPLACE_EXISTING`, so it is a replacing rename and cannot be used here. A no-replace
    publish requires either creating the destination with `CREATE_NEW` disposition (which fails
    with `ERROR_FILE_EXISTS`) and writing through it, or calling `MoveFileEx` directly *without*
    the `MOVEFILE_REPLACE_EXISTING` flag.
  An implementation that cannot obtain a create-no-replace primitive on its target platform must
  fail closed rather than fall back to a replacing rename.

  The finalizer only ever globs the final naming pattern and never opens a `.tmp-*` file — a shard
  is atomically either wholly absent or wholly present, satisfying "aggregation cannot read a
  shard that is still being written."
- **Duplicate producer identity**: because the shard filename now encodes a collision-free digest
  of the package path (previous bullet) and publication is atomic create-no-replace (previous
  bullet), two shards for the same package path can no longer silently collapse into one file. A
  second write attempt for an already-published package fails explicitly at publish time, and the
  finalizer additionally rejects, as corrupt/ambiguous (§9), any case in which it still observes
  two valid shard files whose envelopes report the same `PackagePath` — never "pick one."
- **Shard payload ownership binding**: every shard envelope carries the `RunID` **and** the same
  hex SHA-256 digest of `RunToken`'s decoded bytes stored in `run.json` (§7, `ShardEnvelope.RunTokenHash`), and
  the finalizer verifies both — with a constant-time comparison for the digest — before the shard
  contributes anything to the merge. The filename digest proves *which package* a shard claims to
  be; it proves nothing about *which run* wrote it. Without the payload binding, any process that
  can write into the run directory can drop a well-formed file with a plausible name into
  `shards/` and have it merged into the module report. A shard whose `RunID` or token digest does
  not match is rejected (`"wrong-run-id"` / `"ownership-mismatch"`, §7), never merged and never
  silently skipped. The raw token is still never written to disk — only its digest, which the
  producer already holds because it had to verify ownership before writing (§3 step 3).
- **Expected producer set**: the finalizer validates discovered shards against an authoritative
  expected-producer list supplied by the invoker (§7). That list is defined here so #145/#146 do
  not have to invent it. The expected set is exactly the packages that satisfy **all** of:
  1. they are inside the invocation's own package selection (`./...` or whatever narrower pattern
     the invoker actually passed — the manifest is captured for *that* selection, not for the
     module as a whole);
  2. they contain at least one Go test file for the current build configuration, so `go test`
     actually builds and executes a binary for them;
  3. they are not excluded by build constraints under the invocation's `GOOS`/`GOARCH`/`-tags`;
  4. they link go-specs and wire a `ShardWriter` into their own `TestMain` (§3 step 3).

  and the corresponding **exclusions**, stated explicitly because each one is a false
  missing-producer failure waiting to happen:
  - **Packages with no test files** — `go test` reports them as `? pkg [no test files]` and never
    launches a binary, so no shard can exist. Excluded.
  - **Packages excluded by build tags** for the current configuration. Excluded, and because this
    is configuration-dependent the manifest is only valid for the configuration it was captured
    under; a matrix build captures one manifest per matrix cell.
  - **Packages that do not link go-specs**. Excluded — this is why the list cannot be
    `go list ./...`, which returns the broader module-discovery set (§7).
  - **Packages whose tests are all filtered out by `-run`, `-skip` or `-short`** are **not**
    excluded. Filtering selects *tests*, not *packages*: the binary is still built and executed,
    `TestMain` still runs, and it must still publish a shard — one that legitimately reports zero
    executed tests. A shard with an empty result set is a valid shard, not a missing producer, and
    the finalizer must treat it as such. This is the case most likely to be mis-implemented as
    "no tests ran, so skip the shard", which would make a filtered run indistinguishable from a
    crashed one.
  - **Packages skipped by the build cache** are **not** excluded either; that is exactly the
    missing-producer condition §6 requires to stay loud, and why `-count=1` is mandatory.

  The list is normalized (canonical import paths), de-duplicated, and captured **before**
  `go test` starts, so the finalizer validates the producer set the invocation intended rather
  than one re-derived afterwards from a tree that may have changed.
- **Cleanup/retention**: package processes never delete anything (F5/F3: a process that dies
  early must not have been relied on to clean up, and no process should have delete authority
  over a sibling it can't see). The finalizer prunes its own run's shard directory (including
  `run.json` and `config-error.json`) after a successful merge (configurable; default keep) — and
  only after a *successful* merge, so a `config-error.json` that caused a non-zero finalize exit
  is still on disk for the operator to read. A separate, explicit `gc`-style operation (library
  call or CLI verb) removes run directories older than a retention window — never triggered
  automatically mid-run, since an old-looking directory cannot be distinguished from a
  slow-but-alive peer without external knowledge. `gc` is the only mechanism that removes the
  marker of a run that was killed before finalize (§5, *Run marker lifecycle*).

## 6. Cache, failure, and coverage semantics

**Cache (F4):** a cache hit skips `TestMain` entirely, so a cached package writes no shard for
the current run. Reporting therefore **requires `-count=1`** (or `GOFLAGS=-count=1`) to
guarantee every discovered package actually executes and has a chance to write a fresh shard.
This is a real, user-visible tradeoff — enabling reporting removes `go test`'s caching benefit
for that invocation — and must be stated plainly in docs, not softened. The finalizer must never
assume a missing shard means "unchanged, reuse the last one"; a missing shard is always reported
as missing (§8), because there is no way to distinguish "cached and skipped" from "crashed"
(F5) or "never built."

`-count=1` is the requirement; reading run identity from the environment (§5) is a supporting
property, not a substitute. Because `go test` records the environment variables a test binary
actually reads into its cache key, a new `GO_SPECS_RUN_ID` does invalidate that package's cached
result — but only for packages that already read it, which is why identity must never be
delivered by any other route, and why `-count=1` stays mandatory for the packages that were
otherwise unchanged and read nothing new.

**Exit semantics** — see §8, kept as its own section since #144 requires every state defined
independently (five in v1.0; eight since v1.2, after the configuration-failure row was split into
its preflight and in-binary cases).

**Coverage** — see §9.

## 7. API/interface sketches

Illustrative, not final signatures — #145/#146 own the concrete implementation. New surface
lives alongside `report` (same module, possibly a `report/coordination` subpackage) and does not
touch `NormalizedReport` or any `Render*` function.

```go
// RunID identifies one module-wide `go test` invocation across all its package processes.
// It must come from the environment (GO_SPECS_RUN_ID) — no in-process mechanism can produce
// a value multiple independently-launched package binaries would agree on (see F8).
type RunID string

// RunToken is the random, per-invocation ownership nonce (GO_SPECS_RUN_TOKEN). Unlike RunID it
// is never used for path construction; it exists only to be hashed and compared against the run
// marker (§5), so a RunID collision between unrelated invocations is detectable and rejectable
// rather than assumed away.
type RunToken string

// ValidateRunID enforces ^[A-Za-z0-9_.-]{1,128}$, rejecting empty values and anything that
// could act as a path separator or traversal segment.
func ValidateRunID(s string) (RunID, error)

// ValidateRunToken enforces ^([0-9a-fA-F]{2}){16,64}$: the hex encoding of at
// least 16 bytes drawn from crypto/rand (§10). Validation is a length/charset check — it cannot
// verify the *source* of the entropy, which is why §10 states the crypto/rand requirement
// normatively rather than leaving "recommend high entropy" to implementers.
func ValidateRunToken(s string) (RunToken, error)

// GenerateRunToken produces a conforming token: 16+ bytes from crypto/rand, hex-encoded in
// LOWERCASE. Provided so the preflight step has one obviously-correct path and nobody reaches for
// math/rand or a clock-derived value.
func GenerateRunToken() (RunToken, error)

// HashRunToken hex-decodes the token and returns the lowercase hex SHA-256 digest of the
// resulting BYTES — never of the token's hex text, which would make AB… and ab… hash differently
// despite decoding to identical bytes (§5). This is the only form ever written to disk, in
// run.json (§5) and in every ShardEnvelope. Comparisons of these digests normalize to lowercase
// and then use crypto/subtle.ConstantTimeCompare, never ==.
//
// Reproducing the digest by hand, for anyone debugging an ownership mismatch: the obvious
// one-liner is WRONG, because it hashes the hex text rather than the bytes it encodes.
//
//	printf %s "$GO_SPECS_RUN_TOKEN" | sha256sum                 # wrong — hashes the hex text
//	printf %s "$GO_SPECS_RUN_TOKEN" | xxd -r -p | sha256sum     # correct — hashes the bytes
//
// On macOS, sha256sum is not installed by default; use shasum -a 256 instead:
//
//	printf %s "$GO_SPECS_RUN_TOKEN" | xxd -r -p | shasum -a 256
//
// The `xxd -r -p` step (or `basenc --decode --base16` on an uppercase token) is what makes the
// result match run.json. Expect to need this exactly once, at 2am.
//
// It returns an error because hex decoding can fail; callers that already ran ValidateRunToken
// may treat that error as unreachable, but must not discard it silently.
func HashRunToken(t RunToken) (string, error)

// RunOwnership is the result of successfully claiming or verifying a run's marker file. It is
// the evidence a ShardWriter (or Finalize) needs before it is allowed to touch a shard: proof
// that this process's RunToken matches the one recorded when the run was initialized.
type RunOwnership struct {
	RunID       RunID
	MarkerPath  string // <BaseDir>/<RunID>/run.json
	TokenHash   string // lowercase hex sha256 of RunToken's DECODED BYTES (§5), matching the
	                   // marker's stored value
}

// InitializeRunOptions configures the pre-flight step that must run once, before `go test`,
// per §3 step 2.
type InitializeRunOptions struct {
	RunID   RunID
	Token   RunToken
	BaseDir string // from GO_SPECS_REPORT_DIR, default ".go-specs/runs"
	Force   bool   // explicit operator recovery only (§5, Run marker lifecycle): removes an
	               // existing marker and its shard directory before recreating. Never set
	               // automatically as a fallback after a failed create.
}

// InitializeRun exclusively creates the run marker (<BaseDir>/<RunID>/run.json, §5) recording
// RunID and hash(Token). It fails if the marker already exists — that failure means the RunID
// was reused by another invocation (or a stale marker was left behind) and the caller must not
// proceed to run `go test` with reporting enabled (§8). This is the only code path allowed to
// create a run marker; ShardConfigFromEnv and Finalize only ever read and verify it.
func InitializeRun(ctx context.Context, opts InitializeRunOptions) (RunOwnership, error)

// ShardConfig is resolved once per package process, typically inside TestMain.
type ShardConfig struct {
	Activated   bool     // from GO_SPECS_REPORT_SHARDS; false means every field below is ignored
	RunID       RunID    // from GO_SPECS_RUN_ID; required when Activated
	Token       RunToken // from GO_SPECS_RUN_TOKEN; required when Activated
	BaseDir     string   // from GO_SPECS_REPORT_DIR, default ".go-specs/runs"
	PackagePath string   // this package's import path
	Ownership   RunOwnership // verified against run.json (§5); populated by ShardConfigFromEnv
}

// ShardConfigFromEnv reads GO_SPECS_REPORT_SHARDS first. When the gate is absent or false it
// returns a disabled config with no error — a stale export in a developer shell must never
// hard-fail a test run (§3 step 1, §5). In that state it may still emit the single warn-only
// stderr diagnostic for a present-but-invalid GO_SPECS_RUN_ID / GO_SPECS_RUN_TOKEN (§5), which
// never affects the returned error or the process's exit status — and it MUST obtain those two
// values by scanning os.Environ(), never via os.Getenv/os.LookupEnv, which would enroll them in
// the test cache key and defeat caching for every package on the disabled path (§5).
// When the gate is on, it reads GO_SPECS_RUN_ID /
// GO_SPECS_RUN_TOKEN / GO_SPECS_REPORT_DIR and verifies the run marker (§5), populating
// ShardConfig.Ownership on success. When either identity variable is absent or invalid, the
// marker is missing, or the marker's stored digest does not match this process's Token,
// ShardConfigFromEnv returns an error naming the exact offending variable and remedy (§5) that
// the caller must surface before m.Run() (§3 step 3); invalid configuration or a failed ownership
// check must never be treated as disabled reporting.
//
// All environment reads go through os.Getenv so `go test` records them in its cache key (§5).
func ShardConfigFromEnv(packagePath string) (ShardConfig, error)
func (c ShardConfig) Enabled() bool

// WriteConfigError records a pre-test configuration/ownership failure at
// <BaseDir>/<RunID>/config-error.json using the same create-no-replace publish protocol as a
// shard (§5). It is the carrier of the distinction that a test binary's exit status cannot
// carry (§8): the finalizer maps the presence of this record to exit code 78. It never writes the
// raw token. Callers invoke it on the ShardConfigFromEnv error path, then fail the binary.
func WriteConfigError(baseDir string, runID RunID, packagePath string, reason ConfigErrorReason, diagnostic string) error

// ConfigErrorReason is the machine-readable cause recorded in config-error.json: one of
// "missing-run-id", "missing-run-token", "invalid-run-id", "invalid-run-token",
// "invalid-activation-gate", "marker-missing", "marker-mismatch", "marker-unreadable" (§5).
type ConfigErrorReason string

// ShardWriter is the #145 producer's entry point: one call, from TestMain, after m.Run()
// returns and before os.Exit — mirroring MultiFormatReporter.Flush's existing placement so the
// two compose in the same TestMain body.
type ShardWriter struct{ /* unexported */ }

func NewShardWriter(cfg ShardConfig) *ShardWriter

// Write serializes rep into a versioned ShardEnvelope and publishes it via temp-write + fsync +
// close + an atomic create-no-replace publish (link+unlink on Unix; CREATE_NEW or a
// non-replacing MoveFileEx on Windows — os.Rename is explicitly NOT acceptable, §5). No-op
// returning nil when cfg.Enabled() is false.
func (w *ShardWriter) Write(rep NormalizedReport) error

// ShardEnvelope is the on-disk shape of one shard file. It carries execution data only: neither
// pre-aggregated coverage numbers nor a coverage-profile path. The invoker gives the one combined
// coverage profile directly to the finalizer, so producers never own or describe coverage (§9).
type ShardEnvelope struct {
	ShardSchemaVersion string // versions this envelope, independent of report.SchemaVersion
	RunID              RunID
	RunTokenHash       string // lowercase hex sha256 of RunToken's DECODED BYTES, identical to
	                          // run.json's stored digest (§5).
	                          // Binds the shard to the run that owns the directory, so a shard
	                          // dropped in by a foreign process is rejected, not merged. Verified
	                          // by the finalizer with a constant-time comparison. The raw token is
	                          // never serialized.
	PackagePath        string
	ProducedAt         time.Time
	Report             NormalizedReport // #142's model, unchanged
}

// --- #146 finalizer ---

type FinalizeOptions struct {
	RunID          RunID
	Token          RunToken // verified against run.json (§5) before any shard is read; fail-closed on mismatch
	BaseDir        string
	CoverProfile   string   // path to the one combined `go test -coverprofile` file, optional
	ExpectedProducers []string // required authoritative list of packages expected to emit shards
	Targets        []Target // module-wide XML/HTML/TXT/JSON outputs, reusing report.Target
	Cleanup        bool     // prune the run's shard directory after a successful merge
}

// Finalize is the only code path allowed to read shard files, merge them, and render module-wide
// reports. It verifies run ownership against run.json first, then checks for a config-error.json
// record and — if one is present — reports it and returns without merging or rendering, so the
// caller can exit 78 (§8). Otherwise it lists only final (non-.tmp) shard names, verifies each
// shard's RunID and RunTokenHash, checks them against ExpectedProducers, rejects
// corrupt/duplicate/unexpected/wrong-run/ownership-mismatched/incompatible-version shards
// explicitly (never silently drops or silently picks one), merges coverage from CoverProfile with
// block-level deduplication (§9), and renders every Target through #142's existing
// RenderXML/RenderHTML/RenderTXT/RenderJSON — unchanged.
func Finalize(ctx context.Context, opts FinalizeOptions) (FinalizeResult, error)

type FinalizeResult struct {
	Merged          NormalizedReport
	PackagesFound   []string
	PackagesMissing []string        // in ExpectedProducers but no valid shard found
	Rejected        []RejectedShard
	ConfigError     *ConfigErrorRecord // non-nil when a pre-test configuration failure was
	                                   // recorded by a package binary (§5); the caller maps a
	                                   // non-nil value to exit code 78 (§8)
}

type ConfigErrorRecord struct {
	SchemaVersion string
	RunID         RunID
	PackagePath   string
	Reason        ConfigErrorReason
	Diagnostic    string
	ObservedAt    time.Time
}

type RejectedShard struct {
	Path   string
	Reason string // "corrupt" | "duplicate-package" | "unexpected-package" | "schema-version-mismatch" | "wrong-run-id" | "ownership-mismatch" | "filename-hash-mismatch" | "stale"
}
```

`ExpectedProducers` is authoritative input owned by the invoker, not a value the finalizer may
infer with `go list ./...`. Module discovery includes packages that do not integrate go-specs and
therefore are not shard producers; treating that broader set as expected would create false
missing-shard failures. The list must come from explicit configuration (for example, a checked-in
manifest or repeated CLI argument), be normalized and de-duplicated, and be captured before
`go test` starts so the finalizer validates the same producer set the invocation intended.

**What belongs in that list is defined normatively in §5 (*Expected producer set*)** — the four
membership conditions and, more importantly, the exclusions: packages with no test files,
packages excluded by build tags, and packages that do not link go-specs are out; packages whose
tests were all filtered away by `-run`/`-skip`/`-short` are **in** and must still publish a shard
reporting zero executed tests. #146 must not re-derive or reinterpret that set; the definition
exists precisely so the rule is not invented in code.

A thin `cmd/go-specs-report` CLI wrapping `Finalize` (subcommands `finalize`, `gc`) is a
reasonable #146 deliverable given #141 names GitHub Actions/Shipwright/generic-CI as consumers,
but the library entry point is the actual contract; the CLI is optional sugar.

## 8. Exit semantics

| State | `go test` exit code | Finalize exit code | Notes |
|---|---|---|---|
| Tests pass, reporting succeeds | unchanged (0) | 0 | |
| Tests fail, reporting succeeds | unchanged (1) | 0 | Per-process shard write still happens (`TestMain`'s post-`m.Run()` code runs on ordinary failure, including a panic recovered by the testing package; only abrupt process termination bypasses it — F5) so the failure is fully represented in the merged report. |
| Tests pass, reporting fails (finalize) | unchanged (0) | non-zero, distinct from `go test`'s codes | |
| Tests fail and reporting also fails | unchanged (1) | non-zero | Two independent signals, never collapsed into one. |
| Abrupt process termination: timeout/SIGKILL/OOM/unrecovered out-of-band panic (F5) | whatever `go test`/the OS already reports for a kill/timeout | Finalize reports that package under `PackagesMissing` or `Rejected`, not silently | The killed package's shard was never written; finalize's job is to make that fact loud, not to guess. |
| Invalid protocol configuration detected in the **preflight** step: `InitializeRun` marker-creation failure, or invalid `GO_SPECS_RUN_ID`/`GO_SPECS_RUN_TOKEN`/`GO_SPECS_REPORT_SHARDS` validated there (§3 steps 1–2) | does not run — the invoker aborts before launching it | n/a; the preflight command exits `78` (`EX_CONFIG`) | The preflight command owns its own exit status, so here a distinct code is genuinely available. |
| Invalid protocol configuration detected **inside a package binary** before `m.Run()`: a per-package ownership check that fails against `run.json`, or a partial/invalid variable set observed there (§3 step 3, §5) | non-zero, **but not distinguishable from an ordinary test failure** — see the note below | `78` (`EX_CONFIG`), on discovering `config-error.json` | The binary writes `config-error.json` (§5) and fails; the record, not the exit code, carries the distinction. Finalize must therefore be run even on a red `go test` (§3 step 5). |
| Normal failure publishing a shard (write/publish error, unexpected/duplicate producer, create-no-replace rejection) after a valid, ownership-verified `m.Run()` | original test result preserved, unchanged | non-zero, due to a missing or rejected producer (`PackagesMissing`/`Rejected`) | Reporting failure never overwrites or falsifies the test result that already happened. |

> **Correction in contract v1.2.** v1.1 of this section stated that a pre-test
> configuration/ownership failure detected inside a package binary would surface as a distinct
> `go test` exit code, "recommended `2`". **That is not achievable and the promise is withdrawn.**
>
> The precise mechanism, since an imprecise one invites a bug report against this note: a test
> binary's own exit code is **never propagated** to the `go` command's exit status at all. On a
> failed test action `cmd/go` calls `base.SetExitStatus(1)` with the literal constant `1`,
> having already reported the package as `FAIL`; it never passes the child's status through.
> `base.SetExitStatus` itself keeps the *maximum* of the values it is given
> (`if exitStatus < n { exitStatus = n }`), so it is not the call that discards the `2` — the `2`
> simply never reaches it. A `TestMain` calling `os.Exit(2)` therefore produces the text
> `exit status 2` in the output while `go test` exits `1`, and CI cannot branch on it.
> Independently, `2` would be a poor
> reservation even when a test binary is executed directly, because the Go runtime already exits
> with status `2` on an unrecovered panic, so the value is ambiguous on its own terms. Any
> reserved code must therefore sit outside the runtime's range: this contract uses **`78`
> (`EX_CONFIG`)**, and never `1` or `2`.
>
> `78` is reserved for *configuration* failures specifically. An ordinary reporting failure
> (missing or rejected producers, merge or render errors) exits non-zero with some **other** code
> — `1` is fine there, since finalize is a separate command whose `1` cannot be confused with
> `go test`'s — so that "reporting was misconfigured" and "reporting ran and found a problem"
> stay distinguishable in CI.

Consequently, **`go test`'s exit code reflects test results and nothing else.** The three-way
framing is unchanged; only the carrier of the third distinction moved:

- **`go test` fails** ⇒ that is the test result, full stop; reporting outcome never changes it
  (row two, row eight).
- **Report generation fails after a valid test execution** ⇒ the test result is never falsified;
  only Finalize's own, independent exit code reflects the reporting failure (rows three, four,
  eight).
- **Protocol configuration is invalid before any test ran** ⇒ this is an infrastructure/config
  error, not a test outcome, and it is still surfaced deliberately and loudly with a distinct exit
  code `78` so CI can tell "tests failed" apart from "reporting was misconfigured" — but that code
  is issued by the **preflight or finalize command**, the layer that owns its own exit status
  (rows six and seven), never by a package test binary. A package binary's contribution to this
  case is the `config-error.json` record (§5), which is the evidence the distinct code is derived
  from.

Multi-package coordination therefore adds **no** way for a test binary to change its own exit
status meaningfully based on reporting outcome. F5 already makes that impossible for the kill
case; the toolchain never propagating a child's exit code makes it impossible for the configuration case; and
the design keeps it impossible for every post-`m.Run()` case too, for consistency.

**CI wiring implication.** Because `78` can only arrive from the preflight or finalize step, a
pipeline that runs `go test` and skips finalize when it fails will see a misconfigured reporting
run as an ordinary red test run. Recommended wiring: preflight → `go test` (record its status,
do not abort the job on it) → finalize (always) → fail the job on either status, reported
separately.

"Always" here means **every termination path, not merely a non-zero exit**. A job that is
cancelled, or whose test step is killed by a step/job timeout, is exactly where the
`config-error.json` record and the `PackagesMissing` list are most informative — and exactly where
naive wiring skips finalize. Concretely, on GitHub Actions a condition keyed on `failure()` does
**not** run on cancellation, so the finalize step needs `if: always()` (which per GitHub's own
documentation does run when cancelled) or `if: !cancelled()` when cancellation should be exempt.
The one case no workflow condition can rescue is a hard kill of the runner itself on job timeout,
where nothing further executes; that is a genuine limitation of the environment, not of this
contract, and it is why the shard directory is left in place for the `gc` path (§5) rather than
assumed to be cleaned up by the run that created it.

Finalize's exit code is a **separate command's** exit code by construction (§3 step 5), so it can
never retroactively overwrite what `go test` already returned. Recommended CI wiring keeps `go
test` and `finalize` as two distinct steps/checks (not one combined shell `&&`/exit-code-max
trick) so both failures stay independently attributable, per #144's acceptance criterion.

## 9. Coverage lifecycle and merge responsibility

- `go test ./... -coverprofile=path` produces exactly **one** combined file, written by the `go`
  command itself only after every package binary has finished (F6) — no package process
  contributes a coverage *fragment* to this file; it's not shard-like at all.
- Under `-coverpkg` overlap, that single combined file legitimately contains multiple raw entries
  for the same instrumented block, one per contributing test binary (F7) — this is normal, not
  corruption.
- Go's own tooling resolves this by merging on `(file, start, end)`, using OR for `mode: set` and
  summation for `mode: count`/`atomic` (F7, cross-checked against `go tool cover -func`'s
  correct output). **`report.ParseCoverageProfile` (#142) does not do this merge today** and
  will double-count `Total` on an overlapping profile — verified by inspection of
  `report/coverage.go` lines 45–58 against the `cover6.out` fixture in F7.

Responsibility split:

- **#145 (shard producers) must not compute or embed per-package coverage numbers.** A
  package-local process cannot know the module-wide picture (per #141, explicitly), and per F7
  it can't even correctly de-duplicate against sibling packages' contributions — that
  information only exists once everyone's output has landed in the single combined profile.
  Shards carry execution data (`NormalizedReport` minus `Coverage`, or with `Coverage` left
  zero-value) only. They must not carry a coverage-profile path; the invoker passes the single
  combined profile directly to the finalizer.
- **#146 (finalizer) owns coverage exclusively**, reading the one combined `-coverprofile` file
  named in `FinalizeOptions.CoverProfile` and parsing it through a **new or extended**
  block-deduplicating parser (key: `file, startLine.startCol, endLine.endCol, numStmt`;
  `set`→OR, `count`/`atomic`→sum) before computing `Covered`/`Total`/percentages. This can be
  added as new/extended logic in the `report` package without breaking
  `ParseCoverageProfile`'s existing documented behavior for its current (non-overlapping)
  callers — additive, not a break of #142's contract.
- Aggregate percentages remain summed-statement-based, never averaged (unchanged from #142);
  packages with zero statements remain absent from the profile and contribute nothing (unchanged
  Go coverage instrumentation behavior, already documented in `coverage.go`).

## 10. Security considerations

- **Run ID**: validated against `^[A-Za-z0-9_.-]{1,128}$` before being used as a path segment;
  rejects empty, `..`, `/`, and control characters — blocks path traversal via a crafted
  `GO_SPECS_RUN_ID`. `RunID` alone is a readable label, never proof of ownership — see Run Token.
- **Run Token — what it is not.** `GO_SPECS_RUN_TOKEN` is an **ownership and collision-detection
  mechanism, not a security boundary**, and must never be documented or relied on as one. It
  travels in the environment, so every process in the test's own process tree — every package
  binary, every subprocess a test spawns, anything reading `/proc/<pid>/environ` as the same user
  — can read it. It defends against *accident* (a reused `RunID`, a stale marker, a second
  invocation writing into the same namespace), not against a local adversary who is already
  running code as the invoking user. Nothing in #145/#146 may treat a matching token as an
  authorization decision.
- **Run Token — entropy floor (normative).** The token MUST be **at least 16 bytes read from
  `crypto/rand`**, hex-encoded, and is validated against `^([0-9a-fA-F]{2}){16,64}$` — the pair
  quantifier is deliberate: `^[0-9a-fA-F]{32,128}$` would admit odd lengths (33, 35, …), which
  are not valid hex encodings of any byte string and fail in `hex.DecodeString` at the point of
  use rather than at validation. The pair form matches the ≥16-byte generator rule exactly, so
  validation accepts precisely the values the generator can produce.
  It is never used as a path segment; it exists solely to be hashed and compared against
  the run marker (§5). The floor is normative rather than a recommendation because without it a
  perfectly conformant implementation may seed `math/rand` from the wall clock — and two CI jobs
  starting in the same second then produce the *same* token, reproducing exactly the collision the
  marker exists to detect, while the marker cheerfully reports a match. A charset/length check
  alone cannot detect that, which is why `GenerateRunToken` (§7) is provided.
  `run.json` stores the **hex SHA-256 digest** of the token, never the raw value, and every
  verification uses a **constant-time comparison** (`crypto/subtle.ConstantTimeCompare`). This is
  what makes RunID reuse across independent invocations detectable: two invocations presenting the
  same `RunID` but different `RunToken` values fail closed at marker verification instead of
  silently sharing a run namespace.
- **Activation gate**: `GO_SPECS_REPORT_SHARDS` is the only thing that turns coordination on. A
  run identifier left exported in an environment must be inert (§5), so that report tooling can
  never falsify a test result it was never asked to observe.
- **Shard filename collision**: the sanitized package-path prefix is not injective (`/` → `_`
  collides, e.g. `foo/bar` and `foo_bar`), so identity is carried **solely** by the full SHA-256
  of the original, unsanitized import path (§5); the bounded 40-byte prefix is readability only
  and is never parsed or compared. The finalizer recomputes that digest and checks it against each
  shard's own envelope, closing the collision that a sanitized-only filename would allow. Bounding
  the prefix also removes the opposite failure — a legal but long import path producing a filename
  that exceeds the filesystem's component limit, or the assembled path exceeding Windows'
  260-character total-path limit (§5).
- **Foreign shards in the run directory**: filename identity proves which *package* a shard claims
  to be, never which *run* produced it. Each shard envelope therefore also carries the `RunID` and
  the run's token digest, both verified before merge (§5, §7). Without that binding, anything able
  to write into `<base>/<run-id>/shards/` could have well-formed content merged into the module
  report.
- **Package-path-to-filename mapping**: sanitized and then containment-checked
  (`filepath.Clean` + prefix check against the shard directory) both when a shard is written and
  when its self-reported `PackagePath` is read back by the finalizer — a shard's own content is
  never trusted as a write/read destination without revalidation.
- **Permissions**: run/shard directories `0700`, `run.json`/`config-error.json`/shard files
  `0600` — least privilege even on single-tenant CI, since shard contents (failure messages,
  stack traces, file paths) are not meant to be world-readable by default.
- **Predictable-path symlink hardening**: `<base>/<run-id>/...` is a predictable path, and the
  base directory frequently lives somewhere another local user can reach (a shared CI workspace,
  `/var/tmp`-style scratch). Three normative rules close the resulting symlink/pre-creation
  attacks:
  1. **`O_NOFOLLOW` on every open** of `run.json`, `config-error.json`, shard temp files and shard
     final names, on both the write and the read side. A pre-planted symlink at a shard path must
     cause a failure, not a write through it to an attacker-chosen destination.
  2. **Refuse to operate when the base directory, or any of its parent directories, is
     group-writable or other-writable** (and is not sticky). If any ancestor is writable by
     someone else, another user can swap a directory component and every downstream check is
     meaningless. This is a startup check in `InitializeRun` and in every producer before it
     writes, and it fails closed with a diagnostic naming the offending directory and its mode.
  3. Directories the implementation creates itself are created with mode `0700` and their mode is
     **verified after creation** (`umask` does not apply to a subsequent `Chmod`, and an
     attacker-pre-created directory will not have been created by us at all — a pre-existing
     run directory is already a marker-creation failure by §5).
- **Untrusted shard contents**: shard files are treated as untrusted input at parse time — bound
  the read size before decoding (a corrupt or adversarially large file must not be able to OOM
  the finalizer), use a safe self-describing format (JSON; no `gob`/anything that can execute
  code on decode), and check `ShardSchemaVersion` before trusting any other field. An
  incompatible or unparseable shard is rejected explicitly (`RejectedShard`), never partially
  trusted.
- **Cross-filesystem publish**: documented hard constraint — `GO_SPECS_REPORT_DIR` must be on a
  single local filesystem; temp files are written inside the destination shard directory
  specifically so the create-no-replace publish stays atomic (neither POSIX `rename` nor `link`
  atomicity holds across filesystem/mount boundaries).
- **Network filesystems — a declaration, not an enforcement.** The exclusivity this contract
  depends on — `O_EXCL` for `run.json` and `link`'s `EEXIST` for shard publication — **degrades on
  NFS and CIFS/SMB**, where client-side caching and non-atomic server semantics can let two
  clients both believe they won. The reporting base directory MUST therefore be on a local
  filesystem.

  **No detection is required, and none should be implemented.** This is a documented constraint on
  the caller, not a runtime check: `GO_SPECS_REPORT_DIR` pointing at a network mount is
  unsupported, and the implementation will not notice. The reason is that portable detection does
  not exist — it would mean `statfs`/`fstatfs` `f_type` constants on Linux, `getmntinfo` on macOS
  and `GetDriveType` on Windows, none of which agree with each other and all of which are
  incomplete against overlay and bind mounts. A check that confidently reports "local" for an
  NFS-backed bind mount is **worse than no check**, because it converts a documented constraint the
  operator can reason about into a false assurance they cannot. If an implementation offers such a
  check at all it must be opt-in and advisory, and nothing in the contract may depend on it.

  Stated plainly, because a declaration without its consequences is not honest: on a network
  mount, an exclusivity failure is **not distinguished from a local one**, and the contract
  degrades silently. The two exposures are not equally mitigated:
  - **`run.json` `O_EXCL` may not be exclusive** ⇒ two independent invocations can both believe
    they own the run and write into the same namespace. **Nothing in this contract mitigates
    this.** It is the whole reason the local-filesystem constraint exists, and it is unrecoverable
    by design rather than by oversight.
  - **`link` may report a false `EEXIST`** ⇒ a correct publish looks like a duplicate-producer
    error. The `st_nlink` recipe in the next bullet mitigates this one, and is the *only*
    mitigation the contract offers for any network-filesystem behaviour.

  That asymmetry raises the stakes on the `st_nlink` branch rather than lowering them: it is the
  contract's single line of defence in an environment it has declared unsupported, which is
  exactly the branch least likely to be exercised before it matters. §13 records it as a claim
  that must be pinned by a test.
- **`link(2)` false-negative over NFS**: related, and worth stating explicitly because it produces
  the *opposite* error from the one an implementer expects. Over NFS, `link` can report `EEXIST`
  for an operation that actually **succeeded** — the server performed the link, the reply was
  dropped, and the client's automatic retry then collided with the client's own earlier,
  successful link. Treating that as a duplicate-producer error would fail a perfectly correct
  publish. The conforming sequence is therefore: `link` → on error, `stat` the **temp** file and
  accept the publish if `st_nlink == 2`, otherwise report the duplicate. Checking the temp file's
  link count (not the destination's) is what preserves genuine duplicate detection: if our own
  link succeeded the temp file has two names, whereas if another producer created the destination
  first our temp file still has exactly one.

  The `st_nlink == 2` inference rests on two preconditions, stated here because they are
  otherwise invisible dependencies: (a) the temp file is linked **nowhere else** — it is created
  by this process, inside the destination shard directory, under a name containing its own PID,
  and nothing in this contract ever links it a second time; and (b) nothing may unlink the
  published shard while the run is in flight, which the contract already guarantees by giving no
  producer delete authority and by restricting `gc` to explicit, out-of-run invocation over
  directories older than the retention window (§5). An implementation that adds any other linking
  or pruning path invalidates the inference and must revisit it. This is defence in depth — the
  local-filesystem
  requirement above already puts the contract outside NFS's semantics — but the check costs one
  `stat` on an error path that should never be hit, and its absence turns a dropped packet into a
  spurious hard failure.
- **Concurrent independent runs**: isolated by construction, since each run's shards live under
  `<base>/<run-id>/shards/` and run IDs are validated to be non-empty path segments supplied by
  the caller. A caller reusing the same `RunID` across two independent invocations is no longer a
  gap tolerated by the design: `InitializeRun`'s exclusive creation of `run.json` (§3, §5) rejects
  the reuse outright before `go test` runs, and every subsequent producer/finalizer re-verifies
  its `RunToken` against that marker before touching a shard. This collision detection is
  mandatory, not an opt-in hardening #145 may choose to add. It is fail-closed only because
  `RunID` reuse is itself abnormal (§5, *Run marker lifecycle*): a `RunID` MUST be unique per
  invocation, reruns included, and a genuinely abandoned marker is cleared by the explicit `gc` or
  `--force` paths, never by an automatic staleness guess mid-run.

## 11. Acceptance-criteria mapping (issue #144)

| # | Criterion | How this design satisfies it |
|---|---|---|
| 1 | Lifecycle described invocation → final report publication | §3, §4 |
| 2 | No package process elected by timing or "last writer wins" | Rejected explicitly in §2 (F3); finalization is a separate, externally-triggered step, not an election among package processes |
| 3 | Aggregation cannot read a shard still being written | §5: temp-write + fsync + close + an **atomic create-no-replace publish** (`link`+`unlink` on Unix; `CREATE_NEW`/non-replacing `MoveFileEx` on Windows — `os.Rename` and pre-rename existence checks are explicitly rejected as racy); finalizer globs only final names, never `.tmp-*`, and verifies run ownership via `run.json` before reading any shard |
| 4 | Existing Go flags and package discovery retain normal behavior | No new required test flags (§1 F1, §2); activation is env-var only (`GO_SPECS_REPORT_SHARDS`, §5), `go test`'s own discovery remains untouched. Expected shard producers are supplied separately and authoritatively, with membership and exclusions defined in §5, never inferred by changing or reinterpreting `go test` discovery. |
| 5 | Packages without go-specs integration do not fail due to unknown flags | §1 F1/F2: no forwarded flag is used for module-wide activation; env vars are silently ignored by non-participating packages. §5 additionally makes the identity variables inert unless `GO_SPECS_REPORT_SHARDS` is on, so an integrating package is not broken by a stale export either. |
| 6 | Cache behavior explicit and tested in the later integration slice | §6/§1 F4: `-count=1` required, documented as a stated tradeoff; §5 additionally requires run identity to be read from the environment so it participates in the test-cache key; the finalizer compares shards with the authoritative expected-producer list, so a cache-skipped producer is reported missing and specified for #146 to test |
| 7 | Test failure and reporting failure exit semantics defined independently | §8, full 8-state table. The two signals are carried by **different commands**: `go test`'s exit code reports test results only, while configuration failures and reporting failures are reported by the preflight/finalize commands (`78` = `EX_CONFIG` for configuration, other non-zero for reporting). A configuration failure detected inside a package binary is carried by the `config-error.json` record (§5), not by an exit code, because `cmd/go` never propagates a test binary's own exit code — it reports the package `FAIL` and sets status `1` for any failed test action. |
| 8 | Run directories cannot collide across concurrent invocations | §3/§5/§10: run ID is a validated, caller-supplied, non-empty path segment namespacing `<base>/<run-id>/shards/`, and `InitializeRun`'s exclusively-created `run.json` marker plus per-participant `RunToken` verification makes an actual `RunID` reuse a fail-closed error rather than a probabilistic non-event. §5 closes the lifecycle around that marker (uniqueness per invocation including reruns, explicit `--force` recovery, `gc` for abandoned markers) and §10 sets the token's `crypto/rand` entropy floor, without which two clock-seeded invocations could produce identical tokens and defeat the check |
| 9 | #141 and #143 contain no contradictory execution requirements after this decision | §12: no contradiction found; #141 explicitly pre-authorized exactly this "document the limitation, propose an external coordinator" outcome |
| 10 | Design identifies exact work owned by REPORT-002B and REPORT-002C | §7 (API sketch split), §9 (coverage responsibility split), §12 |

## 12. Scope updates needed in #141 / #143 / #145 / #146

**No contradiction was found that requires a product decision or a weakening of #141.** #141
already anticipates this exact outcome ("If Go's execution model makes single-command final
aggregation impossible without an external coordinator, the implementation must document that
limitation before coding and propose the smallest Go-native-compatible mechanism") — this
document *is* that documentation, and §3's optional, separate `Finalize` step *is* that smallest
mechanism: `go test` itself is never wrapped, replaced, or given new required flags.

Recommended (non-blocking) updates:

- **#141**: add a pointer from "Multi-package constraint" to this design doc; no requirement
  text needs to change.
- **#143**: add this doc's path to References; the architectural invariants list is already
  fully satisfied and needs no edits.
- **#145**: scope should be tightened to explicitly exclude all per-package coverage ownership
  (§9) — shards carry execution data only, with neither coverage values nor a coverage-profile
  path — and
  should reference `ShardConfig`/`ShardWriter`/`ShardEnvelope` (§7) as its contract surface.
  Contract v1.2 adds the following producer-side requirements, each of which needs a test:
  `GO_SPECS_REPORT_SHARDS` as the sole activation gate, with a gate-off run that cannot fail and
  a gate-off warn-only stderr diagnostic for a malformed identity variable; the `os.Environ()`
  access pattern on the disabled path for **every** variable this contract introduces, with the
  call-site comment and the **cache-stability regression test** §5 requires (two runs, gate off,
  varying at least `GO_SPECS_RUN_ID` *and* `GO_SPECS_REPORT_DIR`, second must report `(cached)`);
  every partial-variable row in §5 with the exact variable named in the
  diagnostic; the
  `config-error.json` record on the pre-test failure path (and *no* reliance on a distinguishable
  `go test` exit code); the atomic create-no-replace publish, with a concurrency test (and an NFS-style false-EEXIST unit test for the nlink recipe) proving two
  producers for the same package path do not silently collapse; the 40-byte bounded prefix plus
  assembled-total-path validation; `crypto/rand`-sourced tokens and constant-time digest
  comparison; and `RunTokenHash` in the envelope.
- **#144/#147 follow-up**: the exit-code promise amended here (§8) changed after #145's
  "Invalid configuration semantics" hardening item was written. That item's requirement — invalid
  configuration must fail explicitly, never a stderr-only warning that continues — is unchanged
  and still satisfied. Only its parenthetical expectation of a distinguishing `go test` exit code
  is superseded: the distinction now lives in `config-error.json` plus a `78` from the
  preflight/finalize layer.

**One #145 scope line needs rewording** (the only place contract v1.2 diverges from text already
recorded on an issue). #145's scope currently reads:

> Treat `GO_SPECS_RUN_ID` absence as disabled coordination, but reject a present invalid value
> explicitly; never silently downgrade invalid configuration to disabled reporting.

Under v1.2 the trigger for *failing* on a present invalid value is the **activation gate**, not
the presence of `GO_SPECS_RUN_ID`. The old rule means a stale `GO_SPECS_RUN_ID` exported in a
developer's shell hard-fails every subsequent `go test` in that shell with zero tests executed —
report generation falsifying a test result, exactly what §8's own principle forbids, and a far
more likely event than the misconfiguration the rule was written to catch.

What that rule was actually protecting, though, is **typo detection**, and v1.2 keeps it: with the
gate off, a present-but-invalid run ID or token still produces a one-line stderr diagnostic naming
the variable (§5, *Warn-only validation*). Detection is preserved; only the power to fail a test
run is withdrawn. The anti-downgrade half of the rule is untouched and still absolute: once the
gate is on, invalid configuration is never silently downgraded to disabled reporting. #145's scope
line should be restated as:

> Treat an absent or false `GO_SPECS_REPORT_SHARDS` as disabled coordination: write no shard and
> never affect exit status, but still emit a single stderr diagnostic if `GO_SPECS_RUN_ID` or
> `GO_SPECS_RUN_TOKEN` is present and malformed. When the gate is on, reject an absent or invalid
> `GO_SPECS_RUN_ID`/`GO_SPECS_RUN_TOKEN` explicitly; never silently downgrade invalid
> configuration to disabled reporting.
- **#146**: scope should explicitly add (a) a block-deduplicating coverage merge keyed by
  `(file, start, end, numStmt)` with `set`→OR / `count|atomic`→sum semantics, since existing
  `ParseCoverageProfile` does not do this and will double-count under `-coverpkg` overlap (F7);
  and (b) explicit, tested handling of "missing shard" against the invoker-supplied authoritative
  expected-producer list (cache skip, abrupt termination, or genuine failure to launch — §6/§8),
  never silently treated as "package had nothing to report" and never inferred from `go list ./...`.
  Contract v1.2 adds (c) detecting `config-error.json` and exiting `78` (`EX_CONFIG`) without
  merging or rendering; (d) verifying each shard's `RunID` and `RunTokenHash` before merging, so a
  foreign shard is rejected rather than merged; and (e) honouring §5's expected-producer
  definition verbatim — in particular that a package whose tests were all filtered out by
  `-run`/`-skip`/`-short` still publishes a valid shard reporting zero executed tests and must not
  be treated as a missing producer.

## 13. Unresolved decisions (non-blocking, flagged for a quick maintainer call)

- **Run ID generation recipe**: left to the invoking CI wrapper (uuidgen, CI-native build ID,
  timestamp+random) — go-specs only validates, never generates, since no in-process mechanism
  can produce a value shared across independently-launched binaries (F8). Recommend documenting
  2–3 concrete recipes in #146's docs rather than prescribing one.
- **Thin CLI vs. library-only finalizer**: recommend shipping both (a small `cmd/go-specs-report`
  wrapping `Finalize`), since #141 names GitHub Actions/Shipwright/generic CI as consumers and a
  CLI is the lowest-friction integration point — but this is a #146 implementation-time call.
- **Strict vs. lenient missing-shard default**: the producer list itself is mandatory and
  authoritative; recommend strict-by-default (fail the finalize step when an expected producer
  has no valid shard) with an explicit opt-out for local/dev use. The finalizer must never replace
  that list with `go list ./...`; final strictness is a #146/product call.
- **Activation gate spelling** (v1.2): `GO_SPECS_REPORT_SHARDS` was chosen over overloading the
  existing `GO_SPECS_REPORT` because the latter is #142's per-process local target list and
  carries a `format:path` value, not a boolean — overloading it would make "write a local XML
  file" and "join a coordinated module run" the same switch. The *name* is a #145 implementation
  detail as long as the semantics in §5 hold; the semantics are not negotiable.
- **Bounded prefix budget** (v1.2): 40 bytes sits in the middle of the 32–48 range that keeps
  filenames comfortably inside both the 255-byte component limit and, with a short base dir, the
  Windows 260-character total-path limit. The exact number is a #145 call; the requirement that
  the prefix be *bounded by a fixed budget* and carry no identity is not.
- **Windows long-path opt-in** (v1.2): this contract requires validating the assembled total path
  and failing clearly, rather than requiring `\\?\` prefixing or the `LongPathsEnabled` manifest
  setting. Whether #145 additionally opts in to long paths is left open — it changes the failure
  threshold, not the contract.
- **Which normative claims rest on unpinned runtime behaviour** (v1.2.4, flagged for #145 to
  answer early rather than at the end). Three of the four defects found while reviewing this
  amendment — the token digest's case sensitivity, the matrix `RunID` collision, and the
  test-cache enrollment on the disabled path — shared a shape: the contract was internally
  consistent, and the failure lived in what the environment does *underneath* it. Internal review
  does not catch that class; only a test that exercises the real behaviour does. The
  cache-stability test above is the pattern.

  #145 should therefore enumerate, before implementation ends, every normative claim in this
  contract that depends on runtime or platform behaviour and is not pinned by a test. Known
  members of that list today:
  - **Atomic create-no-replace under concurrency** — pinned by the concurrent-writer test §12
    already requires, on a local filesystem.
  - **Create-no-replace on a network mount** — *not* pinned, and now *not required to be*: §10
    resolves this as a **declaration rather than an enforcement**, with no detection implemented,
    so there is no detection code to test. What that costs is stated there explicitly — a
    `run.json` exclusivity failure on a network mount is unmitigated and unrecoverable by design.
    The item stays on this list only so that the decision is re-read rather than re-litigated.
  - **The `link`-over-NFS `st_nlink == 2` fallback** — *not* pinned, and the clearest example of
    the risk: written once, never exercised, and load-bearing at the moment it is least
    observable. Because §10 declines to detect network mounts, this branch is the **only**
    mitigation the contract offers for any network-filesystem behaviour, which makes pinning it a
    requirement rather than a nicety. At minimum it must be unit-tested against an injected
    `EEXIST` with a stubbed `stat` returning `st_nlink == 2` (publish accepted) and `st_nlink == 1`
    (duplicate reported), so both arms run even where the environment that triggers them does not.
  - **`os.Environ` not routing through testlog** — pinned by the cache-stability test above,
    deliberately, because it is an implementation detail rather than a documented guarantee.

  This is a #145 planning item, not a blocker for this contract; recorded here so the question is
  asked while the code is being written rather than after.
- **Producer-manifest generation recipe** (v1.2): §5 defines *what* belongs in the
  expected-producer set and what is excluded, but deliberately does not prescribe the tooling that
  produces it (a checked-in file, a `go list` pipeline filtered by a go-specs import check, a
  generator). That is a #146/docs call; what must not happen is the finalizer deriving the list
  itself at merge time.
