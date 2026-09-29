package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "changelog", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(b)
}

func TestValidateUnreleased_Valid(t *testing.T) {
	// valid_breaking_under_changed.md holds a "**Breaking.**" entry under "### Changed": the check
	// is structural only, so it must neither reject that entry nor infer anything from its prose.
	for _, name := range []string{
		"valid_full.md",
		"valid_partial.md",
		"valid_empty_sections.md",
		"valid_none.md",
		"valid_breaking_under_changed.md",
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateUnreleased(readFixture(t, name)); err != nil {
				t.Errorf("ValidateUnreleased(%s) = %v, want nil", name, err)
			}
		})
	}
}

func TestValidateUnreleased_Invalid(t *testing.T) {
	tests := []struct {
		fixture string
		want    string
		is      error
	}{
		{fixture: "invalid_duplicate_fixed.md", want: `"### Fixed" appears more than once`},
		{fixture: "invalid_misordered.md", want: `"### Removed" must come before "### Fixed"`},
		{fixture: "invalid_no_unreleased.md", is: ErrNoUnreleasedHeading},
	}
	for _, tc := range tests {
		t.Run(tc.fixture, func(t *testing.T) {
			err := ValidateUnreleased(readFixture(t, tc.fixture))
			if err == nil {
				t.Fatalf("ValidateUnreleased(%s) = nil, want an error", tc.fixture)
			}
			if tc.is != nil && !errors.Is(err, tc.is) {
				t.Errorf("error = %v, want %v", err, tc.is)
			}
			if tc.want != "" && !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestValidateUnreleased_IgnoresOtherHeadingsAndFences(t *testing.T) {
	content := "## [Unreleased]\n\n### Added\n\n```\n### Fixed\n### Fixed\n```\n\n#### Fixed\n\n### Notes\n\n### Fixed\n\n## [v0.1.0] - 2026-01-01\n\n### Fixed\n\n### Fixed\n"
	if err := ValidateUnreleased(content); err != nil {
		t.Errorf("ValidateUnreleased = %v, want nil", err)
	}
}

func TestValidateUnreleased_ReportsEveryProblem(t *testing.T) {
	content := "## [Unreleased]\n\n### Fixed\n\n### Fixed\n\n### Added\n"
	err := ValidateUnreleased(content)
	if err == nil {
		t.Fatal("ValidateUnreleased = nil, want an error")
	}
	for _, want := range []string{`"### Fixed" appears more than once`, `"### Added" must come before "### Fixed"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to contain %q", err, want)
		}
	}
}

func TestRunValidate(t *testing.T) {
	dir := t.TempDir()
	for name, want := range map[string]int{
		"valid_full.md":              0,
		"invalid_duplicate_fixed.md": 1,
		"invalid_no_unreleased.md":   1,
	} {
		path := filepath.Join(dir, name)
		content := readFixture(t, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		if got := run([]string{"validate", "-file", path}, strings.NewReader(""), &stdout, &stderr); got != want {
			t.Errorf("validate %s: exit %d, want %d (stderr: %s)", name, got, want, stderr.String())
		}
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != content {
			t.Errorf("validate %s modified the file", name)
		}
	}
}

func TestRunValidate_MissingFile(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if got := run([]string{"validate", "-file", filepath.Join(t.TempDir(), "nope.md")}, strings.NewReader(""), &stdout, &stderr); got != 1 {
		t.Errorf("exit %d, want 1", got)
	}
}

// TestReleasePrepRoundTrip runs the real release-prep sequence (changelog, latest-heading, notes)
// against a temporary copy of a fixture holding every recognized category, so the last category
// ("### Security") must survive extraction and no pre-existing entry or release history may be
// lost or rewritten.
func TestReleasePrepRoundTrip(t *testing.T) {
	for _, name := range []string{"valid_full.md", "valid_breaking_under_changed.md"} {
		t.Run(name, func(t *testing.T) {
			original := readFixture(t, name)
			path := filepath.Join(t.TempDir(), "CHANGELOG.md")
			if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
				t.Fatal(err)
			}

			var stdout, stderr bytes.Buffer
			args := []string{"changelog", "-version", "v0.3.0", "-date", "2026-10-01", "-file", path}
			if got := run(args, strings.NewReader(""), &stdout, &stderr); got != 0 {
				t.Fatalf("changelog: exit %d, stderr: %s", got, stderr.String())
			}
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			rewritten := string(b)

			// Every non-blank line of the original survives, except the [Unreleased] compare link, which
			// the tool deliberately re-points at the new tag.
			for _, l := range strings.Split(original, "\n") {
				if strings.TrimSpace(l) != "" && !strings.HasPrefix(l, "[Unreleased]:") && !strings.Contains(rewritten, l+"\n") {
					t.Errorf("pre-existing line lost after changelog: %q", l)
				}
			}
			if !strings.Contains(rewritten, "## [v0.2.0] - 2026-09-27") {
				t.Errorf("release history lost:\n%s", rewritten)
			}

			stdout.Reset()
			if got := run([]string{"latest-heading", "-file", path}, strings.NewReader(""), &stdout, &stderr); got != 0 || strings.TrimSpace(stdout.String()) != "v0.3.0" {
				t.Errorf("latest-heading: exit %d, out %q, want v0.3.0", got, stdout.String())
			}

			stdout.Reset()
			if got := run([]string{"notes", "-version", "v0.3.0", "-file", path}, strings.NewReader(""), &stdout, &stderr); got != 0 {
				t.Fatalf("notes: exit %d, stderr: %s", got, stderr.String())
			}
			notes := stdout.String()
			unreleased := original[strings.Index(original, "## [Unreleased]")+len("## [Unreleased]"):]
			unreleased = unreleased[:strings.Index(unreleased, "## [v0.2.0]")]
			for _, l := range strings.Split(unreleased, "\n") {
				if strings.TrimSpace(l) != "" && !strings.Contains(notes, l) {
					t.Errorf("notes missing line %q:\n%s", l, notes)
				}
			}
			if strings.Contains(notes, "earlier feature") {
				t.Errorf("notes leaked the previous release:\n%s", notes)
			}
		})
	}
}
