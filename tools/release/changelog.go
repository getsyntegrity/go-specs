// changelog.go rewrites CHANGELOG.md's `## [Unreleased]` heading into a dated release heading,
// per Decision 1 of docs/investigations/odd-tasks/native-ci-pipeline.md (the changelog is updated
// inside the develop -> main release PR, before merge, not after). It follows Keep a Changelog
// (https://keepachangelog.com/en/1.1.0/): a fresh empty `[Unreleased]` section is left above the
// new release heading, and reference-style compare links for both are added or updated at the
// bottom of the file. This repository's CHANGELOG.md did not carry compare links before this
// tool existed, so there is no prior style to match; the format below follows Keep a Changelog's
// own convention directly.
package main

import (
	"errors"
	"regexp"
	"strings"
)

// ErrNoUnreleasedHeading is returned when CHANGELOG.md has no `## [Unreleased]` heading at all --
// a structural problem with the file, distinct from ErrEmptyUnreleased (a heading with nothing
// under it).
var ErrNoUnreleasedHeading = errors.New("changelog: no \"## [Unreleased]\" heading found")

// ErrEmptyUnreleased is returned when `## [Unreleased]` exists but has no content before the next
// `## ` heading (or end of file) -- there is nothing to release. Callers map this to the same
// exit code NextVersion's ErrNothingReleasable uses, so the release-prep workflow can treat
// "nothing to release" uniformly regardless of which check found it first.
//
// The message is actionable rather than just descriptive because it is the first thing a
// maintainer preparing a hotfix sees: `main`'s [Unreleased] is always empty right after a
// release (T1 of docs/investigations/odd-tasks/hotfix-release-and-charts.md), so a hotfix branch
// hits this every time until the fix's own changelog entry is added.
var ErrEmptyUnreleased = errors.New("changelog: \"## [Unreleased]\" section is empty, nothing to release -- add an entry under \"## [Unreleased]\" describing the fix, then run this again")

// ChangelogOptions configures RewriteChangelog.
type ChangelogOptions struct {
	// Version is the tag being released, e.g. "v0.2.0". Strict SemVer, with the "v" prefix --
	// the same form NextVersion returns.
	Version string
	// Date is the release date, "YYYY-MM-DD".
	Date string
	// Repo is "owner/name", used to build github.com compare/release-tag URLs.
	Repo string
	// Prev optionally overrides the previous release tag used in the new version's compare
	// link. When empty, it is derived from the next "## [<version>]" heading found below the
	// newly inserted one; when there is no earlier heading at all (this is the first release
	// recorded in the file), the version links straight to its GitHub release page instead of
	// a compare range that would have no starting point.
	Prev string
}

var (
	headingVersionRe = regexp.MustCompile(`^## \[([^\]]+)\]`)
	linkLineRe       = regexp.MustCompile(`^\[[^\]]+\]: https?://\S+$`)
)

// normalizeTag prefixes v to a bare version string (Keep a Changelog headings in this repository
// have historically omitted it, e.g. "## [0.1.0]"), so it can be used as a git ref in a compare
// URL, matching the "vX.Y.Z" tags NextVersion produces and this tool's own new headings use.
func normalizeTag(v string) string {
	if v == "" || strings.HasPrefix(v, "v") {
		return v
	}
	return "v" + v
}

// RewriteChangelog rewrites content's `## [Unreleased]` heading into a dated release heading for
// opts.Version, inserts a fresh empty `## [Unreleased]` above it, and adds or updates the
// `[Unreleased]` and `[<Version>]` compare links at the bottom of the file.
//
// changed is false, with content returned unmodified and err nil, when a heading for opts.Version
// already exists -- RewriteChangelog is idempotent, so re-running it against its own prior output
// for the same version is a safe no-op (the release-prep workflow relies on this: a retriggered
// run on an already-prepared commit produces no diff and therefore does not push again).
func RewriteChangelog(content string, opts ChangelogOptions) (string, bool, error) {
	targetPrefix := "## [" + opts.Version + "]"
	lines := strings.Split(content, "\n")

	for _, l := range lines {
		if strings.HasPrefix(l, targetPrefix) {
			return content, false, nil
		}
	}

	unreleasedIdx := -1
	for i, l := range lines {
		if l == "## [Unreleased]" {
			unreleasedIdx = i
			break
		}
	}
	if unreleasedIdx == -1 {
		return "", false, ErrNoUnreleasedHeading
	}

	// j is the index of the next "## " heading after Unreleased, or len(lines) if this is the
	// last section in the file. Computed against the original, pre-insertion line numbering,
	// since it is only used to read (never to write) the section boundaries and the heading
	// below.
	j := len(lines)
	for i := unreleasedIdx + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			j = i
			break
		}
	}

	sectionBody := strings.TrimSpace(strings.Join(lines[unreleasedIdx+1:j], "\n"))
	if sectionBody == "" {
		return "", false, ErrEmptyUnreleased
	}

	prev := opts.Prev
	if prev == "" && j < len(lines) {
		if m := headingVersionRe.FindStringSubmatch(lines[j]); m != nil {
			prev = normalizeTag(m[1])
		}
	}

	newHeadingLine := "## [" + opts.Version + "] - " + opts.Date
	newLines := make([]string, 0, len(lines)+2)
	newLines = append(newLines, lines[:unreleasedIdx+1]...)
	newLines = append(newLines, "", newHeadingLine)
	newLines = append(newLines, lines[unreleasedIdx+1:]...)

	// Peel off any trailing compare-link block so it can be rebuilt with this release's links,
	// rather than duplicated below a second time.
	end := len(newLines) - 1
	for end >= 0 && strings.TrimSpace(newLines[end]) == "" {
		end--
	}
	var existingLinks []string
	for end >= 0 && linkLineRe.MatchString(strings.TrimSpace(newLines[end])) {
		existingLinks = append([]string{strings.TrimSpace(newLines[end])}, existingLinks...)
		end--
	}
	bodyText := strings.TrimRight(strings.Join(newLines[:end+1], "\n"), "\n") + "\n"

	filteredLinks := existingLinks[:0:0]
	for _, l := range existingLinks {
		if !strings.HasPrefix(l, "[Unreleased]:") {
			filteredLinks = append(filteredLinks, l)
		}
	}

	unreleasedLink := "[Unreleased]: https://github.com/" + opts.Repo + "/compare/" + opts.Version + "...HEAD"

	var versionLink string
	if prev != "" {
		versionLink = "[" + opts.Version + "]: https://github.com/" + opts.Repo + "/compare/" + prev + "..." + opts.Version
	} else {
		// No earlier release heading to compare against -- this is the first entry the file
		// records, so there is no meaningful "since" point. Link the tag's own release page
		// instead of fabricating a compare range with no start.
		versionLink = "[" + opts.Version + "]: https://github.com/" + opts.Repo + "/releases/tag/" + opts.Version
	}

	finalLinks := append([]string{unreleasedLink, versionLink}, filteredLinks...)

	result := bodyText + "\n" + strings.Join(finalLinks, "\n") + "\n"
	return result, true, nil
}
