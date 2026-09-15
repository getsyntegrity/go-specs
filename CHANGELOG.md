# Changelog

All notable changes to this project are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); this project is pre-1.0, so any release may include breaking changes (see [README.md#project-status](README.md#project-status)).

Entries for `v0.0.1`–`v0.0.9` predate this file — see [GitHub Releases](https://github.com/pablogore/go-specs/releases) for their auto-generated notes.

## [Unreleased]

### Changed

- Generated `Paths()` candidates now run under a subtest name that identifies them — `<spec breadcrumb>/case-<n>[-seed<s>][-<values>]` — instead of the shared literal `generated`, which Go disambiguated as `generated#01`. `go test -v` failure output names the candidate and the value that failed, and a Cartesian or `Sample` candidate can be re-run on its own with `go test -run` by pasting the name back in; the candidate part of the name carries no regexp metacharacter other than `.`. Selecting a single `Explore`/`ExploreCoverage`/`ExploreSmart` candidate with `-run` is not supported, because the explorer needs the feedback of the candidates `-run` skips — the values embedded in the name mean a diverging candidate does not match the pattern rather than silently running under it. Names are bounded (16 runes per value, 64 per values block, plus a `~<hash>` suffix when truncated); values that would render a pointer address collapse to a stable kind word, and a value type redacts itself from test names by implementing `fmt.Stringer`. See [docs/EXECUTION_MODEL.md](docs/EXECUTION_MODEL.md#generated-candidate-identity). ([#103](https://github.com/pablogore/go-specs/issues/103))
- Reporter events for generated candidates now carry the candidate's own name — `includes tier [tier=pro] #2` — instead of the bare spec name repeated per candidate, so each executed candidate is distinguishable in report output and the ordinal links it to its `go test -v` subtest. ([#103](https://github.com/pablogore/go-specs/issues/103))

### Added

- `benchmarks/paths_bench_test.go`: path-exploration benchmarks for a large Cartesian run and a sampled run, covering the third cost center `BENCHMARKS.md` names.
