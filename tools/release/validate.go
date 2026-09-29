// validate.go is the read-only structural check for CHANGELOG.md's `## [Unreleased]` section, run
// by ci.yml's `verify` job so a duplicated or misordered category never reaches release-prep.yml
// (which would otherwise carry it verbatim into the release notes). It follows Keep a Changelog's
// category vocabulary (https://keepachangelog.com/en/1.1.0/): each recognized `### ` heading may
// appear at most once, in the canonical order below; omitted or empty categories are valid.
//
// The check is deliberately structural. It never reads entry prose, so a "**Breaking.**" item
// under `### Changed` is valid, and it never rewrites or reclassifies anything.
package main

import (
	"errors"
	"fmt"
	"strings"
)

// changelogCategories is Keep a Changelog's category vocabulary in canonical order.
var changelogCategories = []string{"Added", "Changed", "Deprecated", "Removed", "Fixed", "Security"}

// ValidateUnreleased checks that every recognized category heading in content's
// `## [Unreleased]` section occurs at most once and in canonical order. Headings of other names
// or levels, and anything inside a fenced code block, are ignored. All problems found are
// reported together in one error; a file with no `## [Unreleased]` heading yields
// ErrNoUnreleasedHeading.
func ValidateUnreleased(content string) error {
	lines := strings.Split(content, "\n")

	start := -1
	for i, l := range lines {
		if l == "## [Unreleased]" {
			start = i + 1
			break
		}
	}
	if start == -1 {
		return ErrNoUnreleasedHeading
	}

	var problems []error
	seen := map[string]bool{}
	last := -1 // canonical index of the most recent recognized heading
	fenced := false
	for _, l := range lines[start:] {
		if strings.HasPrefix(l, "```") || strings.HasPrefix(l, "~~~") {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		if strings.HasPrefix(l, "## ") {
			break
		}
		name, ok := strings.CutPrefix(l, "### ")
		if !ok {
			continue
		}
		idx := categoryIndex(strings.TrimSpace(name))
		if idx == -1 {
			continue
		}
		cat := changelogCategories[idx]
		if seen[cat] {
			problems = append(problems, fmt.Errorf("%q appears more than once", "### "+cat))
			continue
		}
		seen[cat] = true
		if idx < last {
			problems = append(problems, fmt.Errorf("%q must come before %q", "### "+cat, "### "+changelogCategories[last]))
			continue
		}
		last = idx
	}
	return errors.Join(problems...)
}

func categoryIndex(name string) int {
	for i, c := range changelogCategories {
		if c == name {
			return i
		}
	}
	return -1
}
