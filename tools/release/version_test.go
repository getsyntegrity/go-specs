package main

import (
	"errors"
	"strings"
	"testing"
)

// commitRecord builds one NUL-terminated commit record the way
// `git log --format='%B%x00'` does: the full commit message (subject, blank
// line, body/footers) followed by a NUL byte. See doc.go for the format
// contract next-version's -commits input follows.
func commitRecord(msg string) string {
	return msg + "\x00"
}

func commits(msgs ...string) string {
	var b strings.Builder
	for _, m := range msgs {
		b.WriteString(commitRecord(m))
	}
	return b.String()
}

func TestNextVersion_BumpRules(t *testing.T) {
	tests := []struct {
		name    string
		lastTag string
		commits string
		want    string
	}{
		// --- pre-1.0 (major == 0): feat bumps patch, breaking bumps minor ---
		{
			name:    "pre-1.0 feat bumps patch",
			lastTag: "v0.1.0",
			commits: commits("feat: add widget"),
			want:    "v0.1.1",
		},
		{
			name:    "pre-1.0 fix bumps patch",
			lastTag: "v0.1.0",
			commits: commits("fix: correct off-by-one"),
			want:    "v0.1.1",
		},
		{
			name:    "pre-1.0 bang breaking bumps minor",
			lastTag: "v0.1.0",
			commits: commits("feat!: drop legacy API"),
			want:    "v0.2.0",
		},
		{
			name:    "pre-1.0 scoped bang breaking bumps minor",
			lastTag: "v0.1.0",
			commits: commits("feat(specs)!: drop legacy API"),
			want:    "v0.2.0",
		},
		{
			name:    "pre-1.0 BREAKING CHANGE footer bumps minor",
			lastTag: "v0.1.0",
			commits: commits("fix: patch a thing\n\nBREAKING CHANGE: removes the old field"),
			want:    "v0.2.0",
		},
		{
			name:    "pre-1.0 BREAKING-CHANGE footer bumps minor",
			lastTag: "v0.1.0",
			commits: commits("refactor: reshape internals\n\nBREAKING-CHANGE: removes the old field"),
			want:    "v0.2.0",
		},
		// --- post-1.0 (major > 0): feat bumps minor, breaking bumps major ---
		{
			name:    "post-1.0 feat bumps minor",
			lastTag: "v1.4.2",
			commits: commits("feat: add widget"),
			want:    "v1.5.0",
		},
		{
			name:    "post-1.0 fix bumps patch",
			lastTag: "v1.4.2",
			commits: commits("fix: correct off-by-one"),
			want:    "v1.4.3",
		},
		{
			name:    "post-1.0 bang breaking bumps major",
			lastTag: "v1.4.2",
			commits: commits("feat!: drop legacy API"),
			want:    "v2.0.0",
		},
		{
			name:    "post-1.0 BREAKING CHANGE footer bumps major",
			lastTag: "v1.4.2",
			commits: commits("fix: patch a thing\n\nBREAKING CHANGE: removes the old field"),
			want:    "v2.0.0",
		},
		// --- any other valid Conventional Commit type bumps patch ---
		{
			name:    "build bumps patch",
			lastTag: "v0.3.0",
			commits: commits("build: bump linker flags"),
			want:    "v0.3.1",
		},
		{
			name:    "chore bumps patch",
			lastTag: "v0.3.0",
			commits: commits("chore: tidy go.mod"),
			want:    "v0.3.1",
		},
		{
			name:    "ci bumps patch",
			lastTag: "v0.3.0",
			commits: commits("ci: adjust runner"),
			want:    "v0.3.1",
		},
		{
			name:    "docs bumps patch",
			lastTag: "v0.3.0",
			commits: commits("docs: fix typo"),
			want:    "v0.3.1",
		},
		{
			name:    "perf bumps patch",
			lastTag: "v0.3.0",
			commits: commits("perf: avoid an allocation"),
			want:    "v0.3.1",
		},
		{
			name:    "refactor bumps patch",
			lastTag: "v0.3.0",
			commits: commits("refactor: extract helper"),
			want:    "v0.3.1",
		},
		{
			name:    "revert bumps patch",
			lastTag: "v0.3.0",
			commits: commits("revert: undo prior commit"),
			want:    "v0.3.1",
		},
		{
			name:    "style bumps patch",
			lastTag: "v0.3.0",
			commits: commits("style: gofmt"),
			want:    "v0.3.1",
		},
		{
			name:    "test bumps patch",
			lastTag: "v0.3.0",
			commits: commits("test: add table case"),
			want:    "v0.3.1",
		},
		// --- ignored input: merges, non-conventional subjects, the bot chart commit ---
		{
			name:    "merge commit ignored, real feat still counted",
			lastTag: "v0.1.0",
			commits: commits("Merge pull request #42 from getsyntegrity/feature-x", "feat: add widget"),
			want:    "v0.1.1",
		},
		{
			name:    "non-conventional subject ignored, real fix still counted",
			lastTag: "v0.1.0",
			commits: commits("wip", "fix: correct off-by-one"),
			want:    "v0.1.1",
		},
		{
			name:    "bot chart commit ignored, real feat still counted",
			lastTag: "v0.1.0",
			commits: commits("chore: update benchmark charts [skip ci]", "feat: add widget"),
			want:    "v0.1.1",
		},
		// --- highest bump wins across multiple commits ---
		{
			name:    "breaking outranks feat and fix in the same range",
			lastTag: "v1.0.0",
			commits: commits("fix: correct off-by-one", "feat: add widget", "feat!: drop legacy API"),
			want:    "v2.0.0",
		},
		{
			name:    "feat outranks fix in the same range",
			lastTag: "v1.0.0",
			commits: commits("fix: correct off-by-one", "feat: add widget"),
			want:    "v1.1.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NextVersion(tt.lastTag, tt.commits)
			if err != nil {
				t.Fatalf("NextVersion(%q, ...) unexpected error: %v", tt.lastTag, err)
			}
			if got != tt.want {
				t.Errorf("NextVersion(%q, ...) = %q, want %q", tt.lastTag, got, tt.want)
			}
		})
	}
}

func TestNextVersion_NothingReleasable(t *testing.T) {
	tests := []struct {
		name    string
		commits string
	}{
		{"empty input", ""},
		{"only merge commits", commits("Merge pull request #1 from x/y", "Merge branch 'develop' into main")},
		{"only non-conventional subjects", commits("wip", "oops", "quick fix")},
		{"only the bot chart commit", commits("chore: update benchmark charts [skip ci]")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NextVersion("v0.1.0", tt.commits)
			if !errors.Is(err, ErrNothingReleasable) {
				t.Fatalf("NextVersion(...) error = %v, want ErrNothingReleasable", err)
			}
		})
	}
}

func TestNextVersion_StrictSemverLastTag(t *testing.T) {
	tests := []struct {
		name    string
		lastTag string
	}{
		{"missing v prefix", "0.1.0"},
		{"missing patch", "v0.1"},
		{"pre-release suffix", "v0.1.0-rc.1"},
		{"build metadata suffix", "v0.1.0+build.5"},
		{"non-numeric", "vX.Y.Z"},
		{"empty", ""},
		{"trailing garbage", "v0.1.0 "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NextVersion(tt.lastTag, commits("feat: add widget"))
			if err == nil {
				t.Fatalf("NextVersion(%q, ...) expected an error, got none", tt.lastTag)
			}
			if errors.Is(err, ErrNothingReleasable) {
				t.Fatalf("NextVersion(%q, ...) = ErrNothingReleasable, want a strict semver validation error", tt.lastTag)
			}
		})
	}
}

func TestClassifyCommit(t *testing.T) {
	tests := []struct {
		name      string
		msg       string
		wantKind  commitKind
		wantValid bool
	}{
		{"feat", "feat: add widget", kindFeat, true},
		{"fix", "fix: correct bug", kindOther, true},
		{"scoped fix", "fix(specs): correct bug", kindOther, true},
		{"bang breaking", "feat!: drop API", kindBreaking, true},
		{"scoped bang breaking", "fix(specs)!: drop API", kindBreaking, true},
		{"footer breaking", "fix: correct bug\n\nBREAKING CHANGE: drops a field", kindBreaking, true},
		{"footer breaking hyphen", "fix: correct bug\n\nBREAKING-CHANGE: drops a field", kindBreaking, true},
		{"merge commit", "Merge pull request #1 from x/y", kindOther, false},
		{"free text", "wip", kindOther, false},
		{"unknown type", "oops: not a real type", kindOther, false},
		{"bot chart commit", "chore: update benchmark charts [skip ci]", kindOther, false},
		{"chore is other", "chore: tidy go.mod", kindOther, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, valid := classifyCommit(tt.msg)
			if valid != tt.wantValid {
				t.Fatalf("classifyCommit(%q) valid = %v, want %v", tt.msg, valid, tt.wantValid)
			}
			if valid && kind != tt.wantKind {
				t.Errorf("classifyCommit(%q) kind = %v, want %v", tt.msg, kind, tt.wantKind)
			}
		})
	}
}
