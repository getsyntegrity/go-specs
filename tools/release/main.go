// Command release computes the next release version from Conventional Commits and rewrites
// CHANGELOG.md accordingly, for the release-prep workflow
// (.github/workflows/release-prep.yml). The bump rules and changelog format are documented in
// version.go and changelog.go; this file is only the CLI wiring and exit-code contract.
//
// Exit codes:
//
//	0  success
//	1  error (bad flags, malformed input, an I/O failure)
//	3  "nothing to release" -- no releasable commit (next-version), or an empty
//	   [Unreleased] section (changelog). A distinct code so the release-prep workflow
//	   can tell "there's genuinely nothing to do" apart from "this failed" and stop
//	   green either way.
//
// Usage:
//
//	go run ./tools/release next-version -last v0.1.0 -commits commits.txt
//	go run ./tools/release next-version -last v0.1.0 -commits -   # read from stdin
//	go run ./tools/release next-version -last v0.1.2 -commits commits.txt -patch-only   # hotfix
//	go run ./tools/release changelog -version v0.2.0 -date 2026-10-01 -file CHANGELOG.md
//	go run ./tools/release latest-heading -file CHANGELOG.md
//	go run ./tools/release notes -version v0.2.0 -file CHANGELOG.md
//	go run ./tools/release validate -file CHANGELOG.md   # read-only structure check of [Unreleased]
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// logf writes a diagnostic or result line to w (stdout or stderr) and discards the write's own
// error deliberately: a CLI's own status/output stream failing (a closed pipe, a full disk on the
// other end) is not this tool's failure to report, and there is nowhere further to report it to.
// The process's exit code, returned by every caller regardless, is the real signal.
func logf(w io.Writer, format string, a ...any) {
	_, _ = fmt.Fprintf(w, format, a...)
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		logf(stderr, "release: expected a subcommand (next-version, changelog, latest-heading, notes, validate)\n")
		return 1
	}

	switch args[0] {
	case "next-version":
		return runNextVersion(args[1:], stdin, stdout, stderr)
	case "changelog":
		return runChangelog(args[1:], stdout, stderr)
	case "latest-heading":
		return runLatestHeading(args[1:], stdout, stderr)
	case "notes":
		return runNotes(args[1:], stdout, stderr)
	case "validate":
		return runValidate(args[1:], stdout, stderr)
	default:
		logf(stderr, "release: unknown subcommand %q (expected next-version, changelog, latest-heading, notes)\n", args[0])
		return 1
	}
}

func runNextVersion(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("next-version", flag.ContinueOnError)
	fs.SetOutput(stderr)
	last := fs.String("last", "", "last released tag, strict SemVer (e.g. v1.2.3)")
	commitsPath := fs.String("commits", "", `commit records file, or "-" for stdin (NUL-delimited "git log --format='%B%x00'" output)`)
	patchOnly := fs.Bool("patch-only", false, "hotfix mode: always bump patch, and fail if the range contains a breaking change or a feat commit")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	if *last == "" {
		logf(stderr, "release next-version: -last is required\n")
		return 1
	}
	if *commitsPath == "" {
		logf(stderr, "release next-version: -commits is required\n")
		return 1
	}

	raw, err := readInput(*commitsPath, stdin)
	if err != nil {
		logf(stderr, "release next-version: %v\n", err)
		return 1
	}

	var next string
	if *patchOnly {
		next, err = NextVersionPatchOnly(*last, raw)
	} else {
		next, err = NextVersion(*last, raw)
	}
	switch {
	case err == nil:
		logf(stdout, "%s\n", next)
		return 0
	case errors.Is(err, ErrNothingReleasable):
		logf(stderr, "release next-version: nothing to release since %s\n", *last)
		return 3
	default:
		logf(stderr, "release next-version: %v\n", err)
		return 1
	}
}

func runChangelog(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("changelog", flag.ContinueOnError)
	fs.SetOutput(stderr)
	version := fs.String("version", "", "version being released, e.g. v0.2.0")
	date := fs.String("date", "", "release date, YYYY-MM-DD")
	file := fs.String("file", "CHANGELOG.md", "changelog file to rewrite in place")
	repo := fs.String("repo", "getsyntegrity/go-specs", "owner/name, used to build compare/release-tag links")
	prev := fs.String("prev", "", "previous release tag override for the new compare link (derived from the file when omitted)")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	if *version == "" {
		logf(stderr, "release changelog: -version is required\n")
		return 1
	}
	if *date == "" {
		logf(stderr, "release changelog: -date is required\n")
		return 1
	}

	content, err := os.ReadFile(*file)
	if err != nil {
		logf(stderr, "release changelog: %v\n", err)
		return 1
	}

	result, changed, err := RewriteChangelog(string(content), ChangelogOptions{
		Version: *version,
		Date:    *date,
		Repo:    *repo,
		Prev:    *prev,
	})
	switch {
	case err == nil:
		// fall through
	case errors.Is(err, ErrEmptyUnreleased):
		logf(stderr, "release changelog: %v\n", err)
		return 3
	default:
		logf(stderr, "release changelog: %v\n", err)
		return 1
	}

	if !changed {
		logf(stdout, "release changelog: %s already has a heading for %s, nothing to do\n", *file, *version)
		return 0
	}

	if err := os.WriteFile(*file, []byte(result), 0o644); err != nil {
		logf(stderr, "release changelog: %v\n", err)
		return 1
	}

	logf(stdout, "release changelog: prepared %s in %s\n", *version, *file)
	return 0
}

func runLatestHeading(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("latest-heading", flag.ContinueOnError)
	fs.SetOutput(stderr)
	file := fs.String("file", "CHANGELOG.md", "changelog file to read")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	content, err := os.ReadFile(*file)
	if err != nil {
		logf(stderr, "release latest-heading: %v\n", err)
		return 1
	}

	version, err := LatestHeadingVersion(string(content))
	if err != nil {
		logf(stderr, "release latest-heading: %v\n", err)
		return 1
	}

	logf(stdout, "%s\n", version)
	return 0
}

func runNotes(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("notes", flag.ContinueOnError)
	fs.SetOutput(stderr)
	version := fs.String("version", "", "version whose section to extract, e.g. v0.2.0")
	file := fs.String("file", "CHANGELOG.md", "changelog file to read")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	if *version == "" {
		logf(stderr, "release notes: -version is required\n")
		return 1
	}

	content, err := os.ReadFile(*file)
	if err != nil {
		logf(stderr, "release notes: %v\n", err)
		return 1
	}

	notes, err := ReleaseNotes(string(content), *version)
	if err != nil {
		logf(stderr, "release notes: %v\n", err)
		return 1
	}

	logf(stdout, "%s\n", notes)
	return 0
}

func runValidate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	file := fs.String("file", "CHANGELOG.md", "changelog file to check (never modified)")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	content, err := os.ReadFile(*file)
	if err != nil {
		logf(stderr, "release validate: %v\n", err)
		return 1
	}

	if err := ValidateUnreleased(string(content)); err != nil {
		logf(stderr, "release validate: %s: %v\n", *file, err)
		return 1
	}

	logf(stdout, "release validate: %s [Unreleased] structure is valid\n", *file)
	return 0
}

// readInput reads the -commits argument: "-" means stdin, anything else is a file path.
func readInput(path string, stdin io.Reader) (string, error) {
	if path == "-" {
		b, err := io.ReadAll(stdin)
		return string(b), err
	}
	b, err := os.ReadFile(path)
	return string(b), err
}
