package main

import (
	"errors"
	"strings"
	"testing"
)

const fixtureWithOneRelease = `# Changelog

All notable changes to this project are documented here.

## [Unreleased]

### Added

- something new

## [0.1.0] - 2026-09-16

### Added

- the first release
`

const fixtureFreshUnreleased = `# Changelog

## [Unreleased]

## [0.1.0] - 2026-09-16

### Added

- the first release
`

func TestRewriteChangelog_RewritesUnreleasedHeading(t *testing.T) {
	got, changed, err := RewriteChangelog(fixtureWithOneRelease, ChangelogOptions{
		Version: "v0.2.0",
		Date:    "2026-10-01",
		Repo:    "getsyntegrity/go-specs",
	})
	if err != nil {
		t.Fatalf("RewriteChangelog: unexpected error: %v", err)
	}
	if !changed {
		t.Fatalf("RewriteChangelog: changed = false, want true")
	}

	wantHeadingBlock := "## [Unreleased]\n\n## [v0.2.0] - 2026-10-01\n\n### Added\n\n- something new"
	if !strings.Contains(got, wantHeadingBlock) {
		t.Errorf("RewriteChangelog: output missing expected heading block.\nwant substring:\n%s\n\ngot:\n%s", wantHeadingBlock, got)
	}

	// The prior release's own heading and body must survive untouched.
	if !strings.Contains(got, "## [0.1.0] - 2026-09-16") {
		t.Errorf("RewriteChangelog: prior release heading missing from output:\n%s", got)
	}
	if !strings.Contains(got, "- the first release") {
		t.Errorf("RewriteChangelog: prior release body missing from output:\n%s", got)
	}
}

func TestRewriteChangelog_AddsCompareLinks(t *testing.T) {
	got, _, err := RewriteChangelog(fixtureWithOneRelease, ChangelogOptions{
		Version: "v0.2.0",
		Date:    "2026-10-01",
		Repo:    "getsyntegrity/go-specs",
	})
	if err != nil {
		t.Fatalf("RewriteChangelog: unexpected error: %v", err)
	}

	wantUnreleased := "[Unreleased]: https://github.com/getsyntegrity/go-specs/compare/v0.2.0...HEAD"
	if !strings.Contains(got, wantUnreleased) {
		t.Errorf("RewriteChangelog: missing %q in:\n%s", wantUnreleased, got)
	}

	// prev is derived from the heading below the new one: "0.1.0" in the fixture, normalized
	// to the tag form "v0.1.0" for the compare URL.
	wantVersion := "[v0.2.0]: https://github.com/getsyntegrity/go-specs/compare/v0.1.0...v0.2.0"
	if !strings.Contains(got, wantVersion) {
		t.Errorf("RewriteChangelog: missing %q in:\n%s", wantVersion, got)
	}
}

func TestRewriteChangelog_PrevOverride(t *testing.T) {
	got, _, err := RewriteChangelog(fixtureWithOneRelease, ChangelogOptions{
		Version: "v0.2.0",
		Date:    "2026-10-01",
		Repo:    "getsyntegrity/go-specs",
		Prev:    "v0.1.9",
	})
	if err != nil {
		t.Fatalf("RewriteChangelog: unexpected error: %v", err)
	}

	want := "[v0.2.0]: https://github.com/getsyntegrity/go-specs/compare/v0.1.9...v0.2.0"
	if !strings.Contains(got, want) {
		t.Errorf("RewriteChangelog: missing %q in:\n%s", want, got)
	}
}

func TestRewriteChangelog_ReplacesStaleUnreleasedLink(t *testing.T) {
	fixture := fixtureWithOneRelease + "\n[Unreleased]: https://github.com/getsyntegrity/go-specs/compare/v0.1.0...HEAD\n"

	got, _, err := RewriteChangelog(fixture, ChangelogOptions{
		Version: "v0.2.0",
		Date:    "2026-10-01",
		Repo:    "getsyntegrity/go-specs",
	})
	if err != nil {
		t.Fatalf("RewriteChangelog: unexpected error: %v", err)
	}

	if strings.Count(got, "[Unreleased]: ") != 1 {
		t.Fatalf("RewriteChangelog: want exactly one [Unreleased] link line, got:\n%s", got)
	}
	if strings.Contains(got, "compare/v0.1.0...HEAD") {
		t.Errorf("RewriteChangelog: stale [Unreleased] link line was not replaced:\n%s", got)
	}
}

func TestRewriteChangelog_NoPriorHeadingFallsBackToReleaseTag(t *testing.T) {
	fixture := "# Changelog\n\n## [Unreleased]\n\n### Added\n\n- first thing ever\n"

	got, _, err := RewriteChangelog(fixture, ChangelogOptions{
		Version: "v0.1.0",
		Date:    "2026-09-16",
		Repo:    "getsyntegrity/go-specs",
	})
	if err != nil {
		t.Fatalf("RewriteChangelog: unexpected error: %v", err)
	}

	want := "[v0.1.0]: https://github.com/getsyntegrity/go-specs/releases/tag/v0.1.0"
	if !strings.Contains(got, want) {
		t.Errorf("RewriteChangelog: missing fallback release-tag link %q in:\n%s", want, got)
	}
}

func TestRewriteChangelog_Idempotent(t *testing.T) {
	first, _, err := RewriteChangelog(fixtureWithOneRelease, ChangelogOptions{
		Version: "v0.2.0",
		Date:    "2026-10-01",
		Repo:    "getsyntegrity/go-specs",
	})
	if err != nil {
		t.Fatalf("RewriteChangelog (first run): unexpected error: %v", err)
	}

	second, changed, err := RewriteChangelog(first, ChangelogOptions{
		Version: "v0.2.0",
		Date:    "2026-10-01",
		Repo:    "getsyntegrity/go-specs",
	})
	if err != nil {
		t.Fatalf("RewriteChangelog (second run): unexpected error: %v", err)
	}
	if changed {
		t.Errorf("RewriteChangelog (second run): changed = true, want false (idempotent no-op)")
	}
	if second != first {
		t.Errorf("RewriteChangelog (second run): output differs from first run.\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestRewriteChangelog_MissingUnreleasedHeading(t *testing.T) {
	fixture := "# Changelog\n\n## [0.1.0] - 2026-09-16\n\n### Added\n\n- the first release\n"

	_, _, err := RewriteChangelog(fixture, ChangelogOptions{
		Version: "v0.2.0",
		Date:    "2026-10-01",
		Repo:    "getsyntegrity/go-specs",
	})
	if !errors.Is(err, ErrNoUnreleasedHeading) {
		t.Fatalf("RewriteChangelog: error = %v, want ErrNoUnreleasedHeading", err)
	}
}

func TestRewriteChangelog_EmptyUnreleasedSection(t *testing.T) {
	_, _, err := RewriteChangelog(fixtureFreshUnreleased, ChangelogOptions{
		Version: "v0.2.0",
		Date:    "2026-10-01",
		Repo:    "getsyntegrity/go-specs",
	})
	if !errors.Is(err, ErrEmptyUnreleased) {
		t.Fatalf("RewriteChangelog: error = %v, want ErrEmptyUnreleased", err)
	}
}

// TestErrEmptyUnreleased_MessageIsActionable covers T1's changelog part
// (docs/investigations/odd-tasks/hotfix-release-and-charts.md): a hotfix branch always starts
// with an empty [Unreleased] right after a release, so hitting this error is the expected first
// thing a maintainer preparing a hotfix sees -- the message must say what to do about it, not
// just that something is wrong.
func TestErrEmptyUnreleased_MessageIsActionable(t *testing.T) {
	want := "add an entry under \"## [Unreleased]\" describing the fix"
	if !strings.Contains(ErrEmptyUnreleased.Error(), want) {
		t.Errorf("ErrEmptyUnreleased.Error() = %q, want it to contain %q", ErrEmptyUnreleased.Error(), want)
	}
}
