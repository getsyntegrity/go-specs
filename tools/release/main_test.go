package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun_NextVersion_Success(t *testing.T) {
	commitsPath := writeTempFile(t, commits("feat: add widget"))

	var stdout, stderr bytes.Buffer
	code := run([]string{"next-version", "-last", "v0.1.0", "-commits", commitsPath}, strings.NewReader(""), &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run(next-version) exit = %d, want 0; stderr:\n%s", code, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != "v0.1.1" {
		t.Errorf("run(next-version) stdout = %q, want %q", got, "v0.1.1")
	}
}

func TestRun_NextVersion_StdinInput(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"next-version", "-last", "v0.1.0", "-commits", "-"}, strings.NewReader(commits("fix: correct bug")), &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run(next-version) exit = %d, want 0; stderr:\n%s", code, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != "v0.1.1" {
		t.Errorf("run(next-version) stdout = %q, want %q", got, "v0.1.1")
	}
}

func TestRun_NextVersion_NothingReleasableExitsThree(t *testing.T) {
	commitsPath := writeTempFile(t, commits("chore: update benchmark charts [skip ci]"))

	var stdout, stderr bytes.Buffer
	code := run([]string{"next-version", "-last", "v0.1.0", "-commits", commitsPath}, strings.NewReader(""), &stdout, &stderr)

	if code != 3 {
		t.Fatalf("run(next-version) exit = %d, want 3; stderr:\n%s", code, stderr.String())
	}
}

func TestRun_NextVersion_MissingFlagsExitOne(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"next-version"}, strings.NewReader(""), &stdout, &stderr)
	if code != 1 {
		t.Fatalf("run(next-version) with no flags exit = %d, want 1", code)
	}
}

func TestRun_NextVersion_PatchOnly_Success(t *testing.T) {
	commitsPath := writeTempFile(t, commits("chore: tidy go.mod"))

	var stdout, stderr bytes.Buffer
	code := run([]string{"next-version", "-last", "v0.1.2", "-commits", commitsPath, "-patch-only"}, strings.NewReader(""), &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run(next-version -patch-only) exit = %d, want 0; stderr:\n%s", code, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != "v0.1.3" {
		t.Errorf("run(next-version -patch-only) stdout = %q, want %q", got, "v0.1.3")
	}
}

func TestRun_NextVersion_PatchOnly_BreakingOrFeatExitsOneNamingSubjects(t *testing.T) {
	commitsPath := writeTempFile(t, commits("fix: correct off-by-one", "feat!: drop legacy API"))

	var stdout, stderr bytes.Buffer
	code := run([]string{"next-version", "-last", "v0.1.2", "-commits", commitsPath, "-patch-only"}, strings.NewReader(""), &stdout, &stderr)

	if code != 1 {
		t.Fatalf("run(next-version -patch-only) exit = %d, want 1; stderr:\n%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "feat!: drop legacy API") {
		t.Errorf("run(next-version -patch-only) stderr = %q, want it to name the offending commit subject", stderr.String())
	}
}

func TestRun_NextVersion_PatchOnly_NothingReleasableExitsThree(t *testing.T) {
	commitsPath := writeTempFile(t, commits("chore: update benchmark charts [skip ci]"))

	var stdout, stderr bytes.Buffer
	code := run([]string{"next-version", "-last", "v0.1.2", "-commits", commitsPath, "-patch-only"}, strings.NewReader(""), &stdout, &stderr)

	if code != 3 {
		t.Fatalf("run(next-version -patch-only) exit = %d, want 3; stderr:\n%s", code, stderr.String())
	}
}

func TestRun_UnknownSubcommandExitsOne(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"bogus"}, strings.NewReader(""), &stdout, &stderr)
	if code != 1 {
		t.Fatalf("run(bogus) exit = %d, want 1", code)
	}
}

func TestRun_Changelog_Success(t *testing.T) {
	changelogPath := writeTempFile(t, fixtureWithOneRelease)

	var stdout, stderr bytes.Buffer
	code := run([]string{"changelog", "-version", "v0.2.0", "-date", "2026-10-01", "-file", changelogPath}, strings.NewReader(""), &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run(changelog) exit = %d, want 0; stderr:\n%s", code, stderr.String())
	}

	got, err := os.ReadFile(changelogPath)
	if err != nil {
		t.Fatalf("reading rewritten changelog: %v", err)
	}
	if !strings.Contains(string(got), "## [v0.2.0] - 2026-10-01") {
		t.Errorf("rewritten changelog missing new heading:\n%s", got)
	}
}

func TestRun_Changelog_IdempotentSecondRunExitsZero(t *testing.T) {
	changelogPath := writeTempFile(t, fixtureWithOneRelease)

	var stdout, stderr bytes.Buffer
	if code := run([]string{"changelog", "-version", "v0.2.0", "-date", "2026-10-01", "-file", changelogPath}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("first run exit = %d, want 0; stderr:\n%s", code, stderr.String())
	}
	firstContent, err := os.ReadFile(changelogPath)
	if err != nil {
		t.Fatalf("reading rewritten changelog: %v", err)
	}

	stdout.Reset()
	stderr.Reset()
	code := run([]string{"changelog", "-version", "v0.2.0", "-date", "2026-10-01", "-file", changelogPath}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("second run exit = %d, want 0; stderr:\n%s", code, stderr.String())
	}

	secondContent, err := os.ReadFile(changelogPath)
	if err != nil {
		t.Fatalf("reading changelog after second run: %v", err)
	}
	if string(firstContent) != string(secondContent) {
		t.Errorf("second run changed an already-prepared changelog.\nfirst:\n%s\nsecond:\n%s", firstContent, secondContent)
	}
}

func TestRun_Changelog_EmptyUnreleasedExitsThree(t *testing.T) {
	changelogPath := writeTempFile(t, fixtureFreshUnreleased)

	var stdout, stderr bytes.Buffer
	code := run([]string{"changelog", "-version", "v0.2.0", "-date", "2026-10-01", "-file", changelogPath}, strings.NewReader(""), &stdout, &stderr)

	if code != 3 {
		t.Fatalf("run(changelog) exit = %d, want 3; stderr:\n%s", code, stderr.String())
	}
}

func TestRun_LatestHeading_Success(t *testing.T) {
	changelogPath := writeTempFile(t, fixtureWithOneRelease)

	var stdout, stderr bytes.Buffer
	code := run([]string{"latest-heading", "-file", changelogPath}, strings.NewReader(""), &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run(latest-heading) exit = %d, want 0; stderr:\n%s", code, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != "v0.1.0" {
		t.Errorf("run(latest-heading) stdout = %q, want %q", got, "v0.1.0")
	}
}

func TestRun_Notes_Success(t *testing.T) {
	changelogPath := writeTempFile(t, fixtureWithOneRelease)

	var stdout, stderr bytes.Buffer
	code := run([]string{"notes", "-version", "v0.1.0", "-file", changelogPath}, strings.NewReader(""), &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run(notes) exit = %d, want 0; stderr:\n%s", code, stderr.String())
	}
	want := "### Added\n\n- the first release"
	if got := strings.TrimSpace(stdout.String()); got != want {
		t.Errorf("run(notes) stdout = %q, want %q", got, want)
	}
}

func TestRun_Notes_MissingVersionFlagExitsOne(t *testing.T) {
	changelogPath := writeTempFile(t, fixtureWithOneRelease)

	var stdout, stderr bytes.Buffer
	code := run([]string{"notes", "-file", changelogPath}, strings.NewReader(""), &stdout, &stderr)
	if code != 1 {
		t.Fatalf("run(notes) with no -version exit = %d, want 1", code)
	}
}

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "input.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writeTempFile: %v", err)
	}
	return path
}
