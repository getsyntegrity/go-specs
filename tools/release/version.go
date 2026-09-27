// version.go computes the next release version from Conventional Commits, per Decision 4 of
// docs/investigations/odd-tasks/native-ci-pipeline.md:
//
//   - a breaking change (`!` after type/scope, or a `BREAKING CHANGE:`/`BREAKING-CHANGE:` footer)
//     bumps minor while the last tag's major is 0, and major from 1.0.0 on;
//   - `feat` bumps patch while major is 0, and minor from 1.0.0 on;
//   - any other valid Conventional Commit type (build, chore, ci, docs, fix, perf, refactor,
//     revert, style, test) bumps patch;
//   - a merge commit, a non-Conventional-Commit subject, and the bot's own
//     `chore: update benchmark charts [skip ci]` commit are all ignored -- the first two because
//     they never match the Conventional Commit grammar below, the third by an explicit exact-text
//     exception, so the chart bot never triggers a release on its own.
//
// -commits input format: one record per commit, each the commit's full message (`git log`'s `%B`
// -- subject, blank line, body, footers) terminated by a NUL byte (`git log --format='%B%x00'
// <range>'`). NUL rather than a text delimiter because a commit body can legitimately contain any
// printable text, including blank lines and lines starting with "--" or "##", but never a NUL
// byte.
package main

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ErrNothingReleasable is returned by NextVersion when no commit in the given range is a
// recognized, non-ignored Conventional Commit -- distinct from a plain error so callers (the
// release-prep workflow) can tell "nothing to do" from "this input is broken" and exit 3 instead
// of 1.
var ErrNothingReleasable = errors.New("release: nothing releasable since the last tag")

// ErrPatchOnlyDisallowedCommit is returned by NextVersionPatchOnly when the range being released
// as a hotfix contains a breaking change or a feat commit -- Decision 1 of
// docs/investigations/odd-tasks/hotfix-release-and-charts.md: a hotfix must not change the API,
// so those commit kinds are rejected outright rather than silently downgraded to a patch bump.
// The returned error wraps this sentinel with the offending commit subjects appended, so
// errors.Is still matches it while main.go's error log line stays actionable on its own.
var ErrPatchOnlyDisallowedCommit = errors.New("release: hotfix commits must not contain a breaking change or a feat")

// commitKind classifies a recognized Conventional Commit by the bump it contributes, ordered by
// severity: kindOther < kindFeat < kindBreaking.
type commitKind int

const (
	kindOther commitKind = iota
	kindFeat
	kindBreaking
)

// strictSemver matches the strict SemVer core this repository's tags use: vMAJOR.MINOR.PATCH,
// nothing else. No pre-release or build-metadata suffix, matching release.yml's existing
// validation (kept here so -last is rejected the same way a malformed tag would be).
var strictSemver = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)$`)

// conventionalSubject matches a Conventional Commits 1.0.0 subject line: type, an optional
// (scope), an optional breaking-change `!`, then `: ` and a description. The type is captured
// unanchored to case, then checked against the allowed set below -- a subject using an unknown
// type (or garbage such as "wip" or a merge commit's "Merge pull request ...") simply fails to
// match and is ignored, exactly like a non-Conventional-Commit subject.
var conventionalSubject = regexp.MustCompile(`^([a-zA-Z]+)(\([^)]*\))?(!)?: .+`)

// breakingFooter matches a Conventional Commits breaking-change footer on its own line, either
// spelling the spec allows.
var breakingFooter = regexp.MustCompile(`(?m)^(BREAKING CHANGE|BREAKING-CHANGE): `)

// conventionalTypes is the closed set of Conventional Commit types this repository recognizes as
// releasable. "feat" is handled separately in classifyCommit because it bumps differently from
// every other member of this set.
var conventionalTypes = map[string]bool{
	"feat":     true,
	"fix":      true,
	"build":    true,
	"chore":    true,
	"ci":       true,
	"docs":     true,
	"perf":     true,
	"refactor": true,
	"revert":   true,
	"style":    true,
	"test":     true,
}

// ignoredExact holds full subject lines that are otherwise valid Conventional Commits but are
// deliberately excluded from triggering a release on their own -- currently only the benchmark
// chart bot's commit (benchmarks.yml's `bench` job), a routine, unreviewed, machine-authored
// commit that should never itself justify a new tag.
var ignoredExact = map[string]bool{
	"chore: update benchmark charts [skip ci]": true,
}

// classifyCommit reports how a single commit message (as produced by the -commits format
// documented above) affects the version bump, and whether it counts at all. valid is false for a
// merge commit, a non-Conventional-Commit subject, an unrecognized type, or an explicitly ignored
// subject -- in every valid=false case, kind is meaningless and must not be read.
func classifyCommit(msg string) (kind commitKind, valid bool) {
	lines := strings.Split(msg, "\n")
	if len(lines) == 0 {
		return kindOther, false
	}
	subject := strings.TrimSpace(lines[0])

	if ignoredExact[subject] {
		return kindOther, false
	}

	m := conventionalSubject.FindStringSubmatch(subject)
	if m == nil {
		return kindOther, false
	}

	commitType := strings.ToLower(m[1])
	if !conventionalTypes[commitType] {
		return kindOther, false
	}

	bang := m[3] == "!"
	breaking := bang || breakingFooter.MatchString(msg)

	switch {
	case breaking:
		return kindBreaking, true
	case commitType == "feat":
		return kindFeat, true
	default:
		return kindOther, true
	}
}

// parseLastTag validates and decomposes -last (strict SemVer vMAJOR.MINOR.PATCH), shared by
// NextVersion and NextVersionPatchOnly so both reject a malformed last tag identically.
func parseLastTag(lastTag string) (major, minor, patch int, err error) {
	sub := strictSemver.FindStringSubmatch(lastTag)
	if sub == nil {
		return 0, 0, 0, errors.New("release: -last must be strict SemVer vMAJOR.MINOR.PATCH, got " + strconv.Quote(lastTag))
	}
	major, err = strconv.Atoi(sub[1])
	if err != nil {
		return 0, 0, 0, err
	}
	minor, err = strconv.Atoi(sub[2])
	if err != nil {
		return 0, 0, 0, err
	}
	patch, err = strconv.Atoi(sub[3])
	if err != nil {
		return 0, 0, 0, err
	}
	return major, minor, patch, nil
}

// commitSubject returns a commit record's subject line (the first line, trimmed) -- used to name
// the offending commits in NextVersionPatchOnly's error, since the full multi-line record is not
// useful in a one-line CLI error message.
func commitSubject(record string) string {
	lines := strings.Split(record, "\n")
	return strings.TrimSpace(lines[0])
}

// NextVersion computes the next `vX.Y.Z` tag from lastTag (the most recently released tag,
// strict SemVer) and rawCommits (NUL-delimited commit records, per the format documented above).
//
// It returns ErrNothingReleasable when no commit in rawCommits is a recognized, non-ignored
// Conventional Commit -- callers map that to a distinct exit code (3) rather than a generic
// failure (1), since it is an expected, non-error outcome for a range with nothing to release.
func NextVersion(lastTag string, rawCommits string) (string, error) {
	major, minor, patch, err := parseLastTag(lastTag)
	if err != nil {
		return "", err
	}

	highest := -1 // -1 means "nothing recognized yet"; otherwise a commitKind value.
	for _, record := range strings.Split(rawCommits, "\x00") {
		record = strings.TrimSpace(record)
		if record == "" {
			continue
		}
		kind, valid := classifyCommit(record)
		if !valid {
			continue
		}
		if int(kind) > highest {
			highest = int(kind)
		}
	}

	if highest < 0 {
		return "", ErrNothingReleasable
	}

	switch commitKind(highest) {
	case kindBreaking:
		if major == 0 {
			minor++
			patch = 0
		} else {
			major++
			minor = 0
			patch = 0
		}
	case kindFeat:
		if major == 0 {
			patch++
		} else {
			minor++
			patch = 0
		}
	default: // kindOther
		patch++
	}

	return "v" + strconv.Itoa(major) + "." + strconv.Itoa(minor) + "." + strconv.Itoa(patch), nil
}

// NextVersionPatchOnly computes the next `vX.Y.Z` tag for a hotfix release: always a patch bump
// of lastTag, regardless of which recognized Conventional Commit types are present, per T1 of
// docs/investigations/odd-tasks/hotfix-release-and-charts.md.
//
// A hotfix must not change the API, so a breaking change (`!` or a BREAKING CHANGE/BREAKING-CHANGE
// footer) or a feat commit anywhere in rawCommits fails the whole call with
// ErrPatchOnlyDisallowedCommit, naming every offending commit's subject, rather than silently
// downgrading them to a patch bump the way the ordinary NextVersion rules never would either --
// those commits belong on develop, not on a hotfix branch.
//
// Like NextVersion, it returns ErrNothingReleasable when no commit in rawCommits is a recognized,
// non-ignored Conventional Commit, mapped by the CLI to the same exit code (3) as the ordinary
// path -- "nothing to release" means the same thing on a hotfix branch as it does on develop.
func NextVersionPatchOnly(lastTag string, rawCommits string) (string, error) {
	major, minor, patch, err := parseLastTag(lastTag)
	if err != nil {
		return "", err
	}

	validCount := 0
	var offending []string
	for _, record := range strings.Split(rawCommits, "\x00") {
		record = strings.TrimSpace(record)
		if record == "" {
			continue
		}
		kind, valid := classifyCommit(record)
		if !valid {
			continue
		}
		validCount++
		if kind == kindBreaking || kind == kindFeat {
			offending = append(offending, commitSubject(record))
		}
	}

	if len(offending) > 0 {
		return "", fmt.Errorf("%w: %s", ErrPatchOnlyDisallowedCommit, strings.Join(offending, "; "))
	}
	if validCount == 0 {
		return "", ErrNothingReleasable
	}

	patch++
	return "v" + strconv.Itoa(major) + "." + strconv.Itoa(minor) + "." + strconv.Itoa(patch), nil
}
