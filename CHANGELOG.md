# Changelog

All notable changes to this project are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); this project is pre-1.0, so any release may include breaking changes (see [README.md#project-status](README.md#project-status)).

Entries for `v0.0.1`–`v0.0.9` predate this file — see [GitHub Releases](https://github.com/pablogore/go-specs/releases) for their auto-generated notes.

## [Unreleased]

### Added

- `specs.SubtestName` and `specs.SubtestSeparator` document the mapping from a spec's declared
  `Describe`/`When`/`It` names to its Go subtest identity, including what `testing` does with
  spaces, slashes, empty names, and duplicates. ([#102](https://github.com/getsyntegrity/go-specs/issues/102))

### Changed

- Sequential specs now run as a subtest named by their full `Describe`/`When`/`It` breadcrumb
  instead of the leaf `It` name alone, in both sequential execution models (`Describe`/`Spec` and
  `Builder`/`Runner`). Two specs sharing an `It` name under different scopes are no longer told
  apart by `testing`'s incidental `#01` suffix, and `go test -run 'TestX/Suite/when_b/does_a_thing'`
  selects one behaviour by its declared context. Reporter `Name` and `Path` values are unchanged —
  they remain the framework's own unsanitized values. `DescribeFlat`, `DescribeFast`, and
  `*testing.B` runs create no subtests and are unaffected.
  ([#102](https://github.com/getsyntegrity/go-specs/issues/102))
