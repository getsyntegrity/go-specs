// info.go reads facts back out of an already-prepared CHANGELOG.md, for release.yml (which runs
// after a develop -> main PR merges, once release-prep.yml has already rewritten the file): the
// version it should tag, and that version's own section as GoReleaser release notes.
package main

import (
	"errors"
	"strings"
)

// ErrNoReleaseHeading is returned by LatestHeadingVersion when the file has no released
// "## [<version>]" heading at all, and by ReleaseNotes when the requested version has none.
var ErrNoReleaseHeading = errors.New("changelog: no released \"## [<version>]\" heading found")

// LatestHeadingVersion returns the most recently released version recorded in content, following
// this repository's CHANGELOG.md layout: the first "## [<version>]" heading below
// "## [Unreleased]" (the section release-prep.yml's RewriteChangelog most recently renamed), or,
// for a file with no Unreleased heading, the first "## [<version>]" heading in the file. The
// result is normalized to the "vX.Y.Z" tag form, matching NextVersion's output, even when the
// heading itself omits the "v" (as this repository's own pre-existing "## [0.1.0]" heading does).
func LatestHeadingVersion(content string) (string, error) {
	lines := strings.Split(content, "\n")

	start := 0
	for i, l := range lines {
		if l == "## [Unreleased]" {
			start = i + 1
			break
		}
	}

	for i := start; i < len(lines); i++ {
		if m := headingVersionRe.FindStringSubmatch(lines[i]); m != nil {
			return normalizeTag(m[1]), nil
		}
	}
	return "", ErrNoReleaseHeading
}

// ReleaseNotes returns the trimmed body of version's own CHANGELOG.md section -- everything
// between its "## [<version>]" heading and the next "## " heading, or end of file -- for use as
// GoReleaser's --release-notes input, so a GitHub Release's notes are this repository's own
// hand-written changelog entry rather than GoReleaser's generated git-log changelog.
func ReleaseNotes(content, version string) (string, error) {
	lines := strings.Split(content, "\n")
	// Match either spelling of the heading: this tool's own writes always carry the "v" prefix
	// (changelog.go), but a heading predating this tool -- "## [0.1.0]", already in this
	// repository -- may not.
	prefixes := []string{"## [" + version + "]", "## [" + strings.TrimPrefix(version, "v") + "]"}

	idx := -1
	for i, l := range lines {
		for _, prefix := range prefixes {
			if strings.HasPrefix(l, prefix) {
				idx = i
				break
			}
		}
		if idx != -1 {
			break
		}
	}
	if idx == -1 {
		return "", ErrNoReleaseHeading
	}

	end := len(lines)
	for i := idx + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			end = i
			break
		}
	}

	return strings.TrimSpace(strings.Join(lines[idx+1:end], "\n")), nil
}
