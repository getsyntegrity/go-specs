# Issue #157 — false sharing in parallel runner worker state

Measurement report. **Conclusion: the effect is real but not material. No production change is justified.**

## Layout data

| type | size | per 64-byte cache line |
|---|---|---|
| `parallelBackend` | 24 B | 2.67 |
| `failureRecord` | 48 B | 1.33 |
| `paddedParallelBackend` (experiment only) | 64 B | 1 |

`runParallelWith` allocates `backends := make([]parallelBackend, workers)`. At 24 bytes each, adjacent
workers share cache lines — so the hypothesis has a real physical basis. Each worker writes
`backend.specIndex` once per spec.

## Method

Three arms, all running the same production `runWorker` / `runWorkerBatched`, same context pool, same
atomic counter, same 2000 cheap specs. Only backend placement differs:

| arm | what it is |
|---|---|
| `Baseline` | production `MinimalRunner.RunParallel` — contiguous `[]parallelBackend` |
| `LocalPlain` | **control**: harness-local copy, still contiguous |
| `Padded` | harness-local copy, one backend per cache line |

`LocalPlain` is the arm that makes the result attributable. Without it, `Baseline` vs `Padded` varies
two things at once (padding *and* production-function-vs-harness-copy), and any delta is
unattributable.

Hardware: i7-13620H, hybrid. Pinned to P-cores (`taskset -c 0-11`); E-cores (12-15, 3.6 GHz) excluded.
Go 1.26.8. Arms interleaved round-by-round — `-count=N` runs a benchmark's N repetitions
consecutively, which puts each arm in its own time window and turns systematic drift into a fake
effect (see "Discarded runs").

## Commands

```sh
go test ./specs/ -run TestParallelBackendSize -v        # struct sizes
go test ./specs/ -run '^$' -bench 'FalseSharing' -c -o bench-157/specs.test

# run 3 — full sweep, 20 interleaved rounds
for round in $(seq 1 20); do
  taskset -c 0-11 ./specs.test -test.run '^$' -test.bench 'FalseSharing' \
    -test.benchtime=1s -test.count=1 -test.cpu=1,2,4,8 >> raw3.txt 2>&1
done

# run 4 — focused confirmation at 2 workers, 40 interleaved rounds
for round in $(seq 1 40); do
  taskset -c 0-11 ./specs.test -test.run '^$' -test.bench 'FalseSharing' \
    -test.benchtime=1s -test.count=1 -test.cpu=2 >> raw4.txt 2>&1
done

# run 5 — same as run 4, re-run after rebasing onto origin/develop cf7e387
```

Raw results: `raw3.txt` (480 measurements), `raw4.txt` and `raw5.txt` (240 each).

## Results

### Full sweep (raw3.txt, n=20 per point) — padding effect, `LocalPlain` vs `Padded`

| workers | unpadded | padded | delta |
|---|---|---|---|
| 1 | 72.90µs ± 11% | 70.59µs ± 10% | ~ (p=0.820) |
| 2 | 95.51µs ± 7% | 84.81µs ± 6% | **-11.21%** (p=0.013) |
| 4 | 88.91µs ± 7% | 84.59µs ± 11% | ~ (p=0.242) |
| 8 | 100.7µs ± 6% | 106.3µs ± 13% | ~ (p=0.289) |
| 2, chunk16 | 42.60µs ± 3% | 36.55µs ± 10% | **-14.19%** (p=0.000) |
| 1/4/8, chunk16 | — | — | ~ |

The effect appears **only at 2 workers**, in both chunking strategies, and is absent at 1, 4 and 8.
At 1 worker there is no second worker to share a line with, and `~` there is the control the
hypothesis requires.

### Focused confirmation at 2 workers (raw4.txt, n=40)

Control — `Baseline` vs `LocalPlain`, must be ~0:

| | production | harness copy | delta |
|---|---|---|---|
| chunk1 | 92.24µs ± 2% | 91.82µs ± 6% | ~ (p=0.852) |
| chunk16 | 38.76µs ± 7% | 39.40µs ± 5% | ~ (p=0.807) |
| geomean | | | **+0.59%** |

Padding effect — `LocalPlain` vs `Padded`:

| | unpadded | padded | delta |
|---|---|---|---|
| chunk1 | 91.82µs ± 6% | 84.80µs ± 2% | **-7.64%** (p=0.000) |
| chunk16 | 39.40µs ± 5% | 36.22µs ± 5% | **-8.07%** (p=0.004) |
| geomean | | | **-7.86%** |

Production vs padded: -8.06% (p=0.000) and -6.57% (p=0.009).

### Re-confirmation after rebase (raw5.txt, n=40)

Runs 1-4 were measured on `bd4e957`. Rebasing onto `origin/develop` `cf7e387` brought three commits,
two of them touching code under test: `a897900 fix(specs)!: report every parallel spec failure
without implicit fail-fast` (rewrites `reportFailures` and adds a first-write-wins guard to
`parallelBackend.record`) and `f9eb209 perf(specs): hold the typed expectation's value at its own
type` (the assertion in the spec body). The whole 2-worker experiment was re-run rather than assumed
to carry over.

| | unpadded | padded | delta |
|---|---|---|---|
| chunk1 | 93.56µs ± 14% | 85.49µs ± 10% | **-8.62%** (p=0.005) |
| chunk16 | 39.36µs ± 8% | 34.12µs ± 9% | **-13.30%** (p=0.001) |

Control (`Baseline` vs `LocalPlain`): `~` on both, geomean +2.71%.

The effect reproduces. `parallelBackend` is still 24 bytes with the same field layout, so the
premise is unchanged; the new `record` guard sits on the failure path, which these passing specs
never reach.

## Interpretation

At exactly 2 workers, the two 24-byte backends are 48 bytes total and land on **one** 64-byte line,
guaranteed. Isolating them is worth a reproducible ~7-8%, with a validated control.

At 4 and 8 workers the effect is not detectable: contention on the shared `next *uint32` counter
dominates, and it is a *true* sharing cost that padding the backends cannot address.

## Recommendation: close without a production change

The issue's acceptance criteria ask for a *material* effect. This one is confined to a 2-worker
configuration and vanishes at the worker counts real runs use — `RunParallel(tb, 0)` defaults to
`GOMAXPROCS`, which on any CI runner or dev machine is well above 2. Adding `unsafe`-adjacent
cache-line padding to production to win at the single point of the curve nobody runs does not pay
for itself, which is precisely what the issue said not to do without evidence.

If a future workload does pin the runner to 2 workers, this report is the evidence to revisit it.

## Discarded runs

Two earlier runs were thrown out rather than reported. Both are kept here because the *way* they
failed is the reason the final numbers are trustworthy.

**Run 1** (`raw.txt`) — `-benchtime=200x` gave ~10ms of work per measurement, and the machine's
E-cores were in play. Variance reached ±104%, and the padded arm showed **+24% at 1 worker**, where
padding cannot physically do anything. That single impossible number is what condemned the run.

**Run 2** (`raw2.txt`) — P-core pinned and longer, but used `-count=20`, which groups a benchmark's
repetitions consecutively instead of interleaving arms. It reported -10.4% at **1 worker**
(p=0.014) and flipped sign across chunk16 (-5%, -12%, +14%, +9%). Once the arms were interleaved in
run 3, that -10% disappeared entirely: it was drift between time windows, not an effect.

Run 2 also predates a rebase onto `origin/develop`, which moved 8 commits including
`6d4433f fix(specs): centralize assertion failure recording into one authoritative path` — touching
`parallelBackend.record`, the exact hot write under test, and renaming `parallelFailure` to
`failureRecord`.
