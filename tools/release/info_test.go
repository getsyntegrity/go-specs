package main

import (
	"errors"
	"testing"
)

func TestLatestHeadingVersion(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "version heading below Unreleased, no v prefix in file",
			content: fixtureWithOneRelease,
			want:    "v0.1.0",
		},
		{
			name:    "version heading already carries v prefix",
			content: "## [Unreleased]\n\n## [v0.2.0] - 2026-10-01\n\n### Added\n\n- x\n",
			want:    "v0.2.0",
		},
		{
			name:    "no Unreleased heading at all, falls back to the first release heading",
			content: "# Changelog\n\n## [0.1.0] - 2026-09-16\n\n### Added\n\n- x\n",
			want:    "v0.1.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := LatestHeadingVersion(tt.content)
			if err != nil {
				t.Fatalf("LatestHeadingVersion: unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("LatestHeadingVersion = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLatestHeadingVersion_NoHeadingAtAll(t *testing.T) {
	_, err := LatestHeadingVersion("# Changelog\n\nNothing here yet.\n")
	if !errors.Is(err, ErrNoReleaseHeading) {
		t.Fatalf("LatestHeadingVersion error = %v, want ErrNoReleaseHeading", err)
	}
}

func TestReleaseNotes(t *testing.T) {
	content := "# Changelog\n\n## [Unreleased]\n\n## [v0.2.0] - 2026-10-01\n\n### Added\n\n- something new\n\n## [0.1.0] - 2026-09-16\n\n### Added\n\n- the first release\n"

	got, err := ReleaseNotes(content, "v0.2.0")
	if err != nil {
		t.Fatalf("ReleaseNotes: unexpected error: %v", err)
	}
	want := "### Added\n\n- something new"
	if got != want {
		t.Errorf("ReleaseNotes = %q, want %q", got, want)
	}
}

func TestReleaseNotes_LastSectionInFile(t *testing.T) {
	content := "# Changelog\n\n## [Unreleased]\n\n## [v0.1.0] - 2026-09-16\n\n### Added\n\n- the first release\n"

	got, err := ReleaseNotes(content, "v0.1.0")
	if err != nil {
		t.Fatalf("ReleaseNotes: unexpected error: %v", err)
	}
	want := "### Added\n\n- the first release"
	if got != want {
		t.Errorf("ReleaseNotes = %q, want %q", got, want)
	}
}

func TestReleaseNotes_MissingHeading(t *testing.T) {
	_, err := ReleaseNotes(fixtureWithOneRelease, "v9.9.9")
	if !errors.Is(err, ErrNoReleaseHeading) {
		t.Fatalf("ReleaseNotes error = %v, want ErrNoReleaseHeading", err)
	}
}
