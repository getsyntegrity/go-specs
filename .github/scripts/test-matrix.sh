#!/usr/bin/env bash
# Builds the GitHub Actions test matrix, splitting slow packages BY TEST.
#
# `gotestsum tool ci-matrix` only distributes whole packages, so one package that
# alone takes minutes leaves every other shard idle. This script keeps ci-matrix for
# the regular packages and splits every package slower than SPLIT_THRESHOLD into
# several shards of top-level tests (balanced with greedy LPT: longest test first,
# into the least-loaded bin). Nothing is hard-coded: slow packages are detected from
# the timings of previous runs, so it keeps working when packages are renamed or moved.
#
# Usage: test-matrix.sh            (writes one JSON object to stdout, logs to stderr)
#   {"include":[{"id":0,"packages":"<import paths>","run":"","estimatedRuntime":"1m4s"}, ...]}
#   run is empty for regular shards, or '^(TestA|TestB)$' for a piece of a split package.
#
# Environment:
#   TEST_SHARDS      shards for the regular (not split) packages. Required.
#   SPLIT_THRESHOLD  seconds; packages slower than this are split by test. Default 90.
#   SPLIT_TARGET     seconds each split shard should take, about. Default 90.
#   TIMINGS_GLOB     gotestsum --jsonfile outputs of previous runs. Default '.test-timings/*.json'.
# Requires: go, jq, gotestsum (for the regular packages).
set -euo pipefail

shards="${TEST_SHARDS:?missing TEST_SHARDS}"
threshold="${SPLIT_THRESHOLD:-90}"
target="${SPLIT_TARGET:-90}"
glob="${TIMINGS_GLOB:-.test-timings/*.json}"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

log() { echo "test-matrix: $*" >&2; }

# Same format as ci-matrix's estimatedRuntime (a Go duration, e.g. 1m4s).
duration() { awk -v s="$1" 'BEGIN {
  s = int(s + 0.5); h = int(s / 3600); m = int((s % 3600) / 60); s = s % 60
  out = (h > 0 ? h "h" : "") (h > 0 || m > 0 ? m "m" : "") s "s"; print out }'; }

# Timing files may not exist yet (first run, cache miss).
files=()
# shellcheck disable=SC2206  # the glob must expand
files=($glob)
[ -e "${files[0]:-}" ] || files=()

# Package times: SUM over the files of the package-level pass|fail event. A package
# that was split ran in several shards (one file each), so the sum is its total time; a
# whole package appears in a single file. The "Save timings" step of ci.yml keeps only
# the files of the latest run, so stale files do not inflate the sum.
# Test times: max over the files for top-level tests (names without '/'; subtests are
# included in their parent).
if [ "${#files[@]}" -gt 0 ]; then
  jq -rn '
    [inputs | select(.Test == null and (.Action == "pass" or .Action == "fail") and .Elapsed != null)
     | {k: .Package, v: .Elapsed}]
    | group_by(.k) | map("\(.[0].k)\t\(map(.v) | add)") | .[]' "${files[@]}" > "$tmp/pkgtimes.tsv"
  jq -rn '
    [inputs | select(.Test != null and (.Test | contains("/") | not)
                     and (.Action == "pass" or .Action == "fail") and .Elapsed != null)
     | {k: "\(.Package)\t\(.Test)", v: .Elapsed}]
    | group_by(.k) | map("\(.[0].k)\t\(map(.v) | max)") | .[]' "${files[@]}" > "$tmp/testtimes.tsv"
else
  : > "$tmp/pkgtimes.tsv"; : > "$tmp/testtimes.tsv"
fi

go list ./... > "$tmp/all.txt"

# Split candidates: package time > threshold.
awk -F'\t' -v t="$threshold" 'NR == FNR { if ($2 + 0 > t + 0) slow[$1] = $2; next }
  ($1 in slow) { print $1 "\t" slow[$1] }' "$tmp/pkgtimes.tsv" "$tmp/all.txt" > "$tmp/slow.tsv" || true

# Each split package becomes bins of tests. On any listing problem the package goes back
# to the regular pool, so no test is ever dropped.
cp "$tmp/all.txt" "$tmp/regular.txt"
while IFS=$'\t' read -r pkg ptime; do
  [ -n "$pkg" ] || continue
  if ! go test -list '.*' "$pkg" 2> "$tmp/list.err" | grep -E '^(Test|Example|Fuzz)' > "$tmp/tests.txt" \
     || [ ! -s "$tmp/tests.txt" ]; then
    log "cannot list tests of $pkg, keeping it whole"
    continue
  fi
  sort -u "$tmp/tests.txt" -o "$tmp/tests.txt"
  ntests=$(wc -l < "$tmp/tests.txt")
  k=$(awk -v p="$ptime" -v t="$target" -v n="$ntests" 'BEGIN {
    k = int(p / t); if (k < p / t) k++; if (k < 2) k = 2; if (k > n) k = n; print k }')
  if [ "$k" -lt 2 ]; then
    log "$pkg has a single test, keeping it whole"
    continue
  fi
  # Bins via greedy LPT. Unknown tests get the mean of the known ones (1s if none known).
  awk -F'\t' -v pkg="$pkg" -v k="$k" -v tf="$tmp/testtimes.tsv" '
    FILENAME == tf { if ($1 == pkg) known[$2] = $3 + 0; next }
    { names[++n] = $1 }
    END {
      for (i = 1; i <= n; i++) if (names[i] in known) { sum += known[names[i]]; c++ }
      mean = c > 0 ? sum / c : 1
      for (i = 1; i <= n; i++) t[i] = (names[i] in known) ? known[names[i]] : mean
      # sort indexes by time desc (insertion sort; n is small), ties by name for determinism
      for (i = 1; i <= n; i++) idx[i] = i
      for (i = 2; i <= n; i++) { v = idx[i]
        for (j = i - 1; j >= 1 && (t[idx[j]] < t[v] || (t[idx[j]] == t[v] && names[idx[j]] > names[v])); j--) idx[j + 1] = idx[j]
        idx[j + 1] = v }
      for (b = 1; b <= k; b++) load[b] = 0
      for (i = 1; i <= n; i++) { m = 1
        for (b = 2; b <= k; b++) if (load[b] < load[m]) m = b
        load[m] += t[idx[i]]; bin[m] = bin[m] (bin[m] == "" ? "" : "|") names[idx[i]] }
      for (b = 1; b <= k; b++) printf "%s\t%s\t%.3f\t%s\n", pkg, b, load[b], bin[b]
    }' "$tmp/testtimes.tsv" "$tmp/tests.txt" >> "$tmp/bins.tsv"
  grep -vxF "$pkg" "$tmp/regular.txt" > "$tmp/regular.new" || true
  mv "$tmp/regular.new" "$tmp/regular.txt"
  log "splitting $pkg ($(duration "$ptime"), $ntests tests) into $k shards"
done < "$tmp/slow.tsv"
[ -e "$tmp/bins.tsv" ] || : > "$tmp/bins.tsv"

# Regular packages: exactly as before.
if [ -s "$tmp/regular.txt" ]; then
  gotestsum tool ci-matrix --partitions "$shards" --timing-files "$glob" < "$tmp/regular.txt" \
    | jq -c '.include[] | {packages, run: "", estimatedRuntime}' > "$tmp/entries.ndjson"
else
  : > "$tmp/entries.ndjson"
fi

# Split entries. Bins that ended up empty are skipped (cannot happen with k <= tests).
while IFS=$'\t' read -r pkg _ load names; do
  [ -n "$names" ] || continue
  jq -cn --arg p "$pkg" --arg r "^($names)\$" --arg e "$(duration "$load")" \
    '{packages: $p, run: $r, estimatedRuntime: $e}'
done < "$tmp/bins.tsv" >> "$tmp/entries.ndjson"

jq -cs '{include: (to_entries | map({id: .key} + .value))}' "$tmp/entries.ndjson"
