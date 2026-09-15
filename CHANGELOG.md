# Changelog

All notable changes to this project are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); this project is pre-1.0, so any release may include breaking changes (see [README.md#project-status](README.md#project-status)).

Entries for `v0.0.1`–`v0.0.9` predate this file — see [GitHub Releases](https://github.com/pablogore/go-specs/releases) for their auto-generated notes.

## [Unreleased]

### Changed

- Sequential specs now run as a subtest named by their full `Describe`/`When`/`It` breadcrumb
  instead of the leaf `It` name alone, in both sequential execution models (`Describe`/`Spec` and
  `Builder`/`Runner`). Two specs sharing an `It` name under different scopes are no longer told
  apart by `testing`'s incidental `#01` suffix, and `go test -run 'TestX/Suite/when_b/does_a_thing'`
  selects one behaviour by its declared context. The breadcrumb is joined with `/` and never
  escaped, so the guarantee is stated over the name `testing` actually uses: two specs are
  independently identifiable whenever their *normalized* breadcrumbs differ. Breadcrumbs that
  normalize to the same string — `When("a/b")` against `Describe("a")`/`When("b")`, or `When("when a")`
  against `When("when_a")` — stay ambiguous and keep `testing`'s `#01` numbering, the same ambiguity
  `testing.T.Run` has on its own; this is accepted so that `-run` patterns stay typeable from the
  declared names. The mapping is an internal detail and is not exported. Reporter `Name` and `Path`
  values are unchanged — they remain the framework's own unsanitized values.

  This applies to every run whose backend is a real `*testing.T`, including `DescribeFlat` and
  `DescribeFast`: despite their names, both create one subtest per spec exactly like `Describe`, so
  both are renamed by this change too. Only a `*testing.B` backend runs without subtests and is
  genuinely unaffected.

  **Migration.** A `-run` pattern that named a spec by its `It` name alone no longer matches; it now
  needs the full breadcrumb. `go test -run 'TestCart/has_no_items'` becomes
  `go test -run 'TestCart/Cart/empty/has_no_items'`. This affects CI scripts that pin specific
  specs, and saved per-test re-run buttons in IDEs, which will silently select nothing until the
  stored pattern is regenerated.

  Two pre-existing behaviours become easier to meet now that `-run` is a documented way to select
  specs. Under a narrow `-run` pattern the `Builder`/`Runner` model still executes the group
  `BeforeEach`/`AfterEach` hooks of specs the pattern discarded, because those hooks run outside the
  subtest; the `Describe`/`Spec` model does not. And in either model a spec whose subtest the filter
  discarded is still reported to an attached reporter as started and finished without failing, so it
  appears as passed although its body never ran. Both are tracked separately.
  ([#102](https://github.com/getsyntegrity/go-specs/issues/102))
