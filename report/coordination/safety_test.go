package coordination

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// decoyMarker writes a marker that WOULD verify successfully, at a path outside the run
// directory. If any symlink is followed, verification succeeds and the test fails — which is what
// makes these tests prove non-following rather than merely prove "an error happened".
func decoyMarker(t *testing.T, dir string, id RunID, tok RunToken) string {
	t.Helper()
	hash, err := HashRunToken(tok)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "decoy.json")
	body := `{"schemaVersion":"` + markerSchemaVersion + `","runId":"` + string(id) + `","runTokenHash":"` + hash + `"}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestVerifyRunOwnershipRefusesToFollowASymlinkedMarker(t *testing.T) {
	// Claim E1. <base>/<run-id>/run.json is a predictable path. A pre-planted symlink must cause
	// a failure, not a read of an attacker-chosen file that happens to verify.
	if runtime.GOOS == "windows" {
		t.Skip("creating a symlink on Windows needs a privilege ordinary CI accounts do not hold")
	}
	base := secureTempDir(t)
	elsewhere := secureTempDir(t)
	mustInitRun(t, base, "run-1", validToken)

	path := markerPath(base, "run-1")
	decoy := decoyMarker(t, elsewhere, "run-1", validToken)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(decoy, path); err != nil {
		t.Fatal(err)
	}

	if _, err := VerifyRunOwnership(base, "run-1", validToken); err == nil {
		t.Fatal("verification followed a symlinked run.json and succeeded; O_NOFOLLOW is not being applied on the read side")
	}
}

func TestInitializeRunRefusesToWriteThroughASymlinkedMarker(t *testing.T) {
	// The write side of claim E1: a pre-planted symlink must not redirect the marker write to an
	// attacker-chosen destination.
	if runtime.GOOS == "windows" {
		t.Skip("creating a symlink on Windows needs a privilege ordinary CI accounts do not hold")
	}
	base := secureTempDir(t)
	elsewhere := secureTempDir(t)

	dir := runDir(base, "run-1")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(elsewhere, "victim")
	if err := os.WriteFile(victim, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, markerPath(base, "run-1")); err != nil {
		t.Fatal(err)
	}

	if _, err := InitializeRun(context.Background(), InitializeRunOptions{
		RunID: "run-1", Token: validToken, BaseDir: base,
	}); err == nil {
		t.Fatal("InitializeRun wrote through a symlinked run.json")
	}
	got, err := os.ReadFile(victim)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "original" {
		t.Fatalf("the symlink target was modified: %q", got)
	}
}

func TestReportingRefusesAForeignWritableAncestor(t *testing.T) {
	// Claim E3. If any ancestor is writable by someone else, another user can swap a directory
	// component and every downstream check — exclusive creation, create-no-replace, O_NOFOLLOW —
	// is defending a path that is no longer the path it thinks it is.
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mode bits do not carry the same meaning on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses the permission semantics this test asserts")
	}
	parent := secureTempDir(t)
	base := filepath.Join(parent, "runs")
	if err := os.Mkdir(base, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(base, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0o777); err != nil {
		t.Fatal(err)
	}

	_, err := InitializeRun(context.Background(), InitializeRunOptions{
		RunID: "run-1", Token: validToken, BaseDir: base,
	})
	if err == nil {
		t.Fatal("InitializeRun proceeded under a world-writable ancestor")
	}
	msg := err.Error()
	if !strings.Contains(msg, parent) {
		t.Fatalf("error %q does not name the offending directory", msg)
	}
	if !strings.Contains(msg, "0777") {
		t.Fatalf("error %q does not state the offending mode", msg)
	}

	// The producer side performs the same startup check, not just the preflight step.
	if _, err := VerifyRunOwnership(base, "run-1", validToken); err == nil {
		t.Fatal("the producer path skipped the ancestor check")
	}
}

func TestStickyAncestorsAreAccepted(t *testing.T) {
	// /tmp is 1777. On a sticky directory only the owner of an entry may remove or rename it,
	// which is the property the check actually needs — without this exemption every temp-backed
	// CI scratch directory would be refused.
	if runtime.GOOS == "windows" {
		t.Skip("POSIX sticky semantics do not apply on Windows")
	}
	base := secureTempDir(t)
	if err := ensureSafeBaseDir(base); err != nil {
		t.Fatalf("a directory under sticky /tmp was refused: %v", err)
	}
}

func TestPreExistingRunDirectoryWithAWiderModeIsRefused(t *testing.T) {
	// Claim E2. A directory that already exists was not created by this implementation, so its
	// mode is evidence about who else can reach it. Quietly tightening it would hide the fact
	// that something else created the path first.
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mode bits do not carry the same meaning on Windows")
	}
	base := secureTempDir(t)
	dir := runDir(base, "run-1")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}

	_, err := InitializeRun(context.Background(), InitializeRunOptions{
		RunID: "run-1", Token: validToken, BaseDir: base,
	})
	if err == nil {
		t.Fatal("InitializeRun accepted a pre-existing world-writable run directory")
	}
	if !strings.Contains(err.Error(), "0777") {
		t.Fatalf("error %q does not state the offending mode", err)
	}
	if got := modeOf(t, dir); got != 0o777 {
		t.Fatalf("the refused directory was modified to %04o; refusal must not tighten it silently", got)
	}
}

func TestCreatedDirectoriesGetModeSevenHundredRegardlessOfUmask(t *testing.T) {
	// umask does not apply to a subsequent Chmod, which is what actually guarantees the mode.
	if runtime.GOOS == "windows" {
		t.Skip("POSIX umask does not apply on Windows")
	}
	base := secureTempDir(t)
	dir := filepath.Join(base, "nested", "runs")
	if err := mkdirSecure(dir); err != nil {
		t.Fatalf("mkdirSecure: %v", err)
	}
	if got := modeOf(t, dir); got != 0o700 {
		t.Fatalf("created directory mode = %04o, want 0700", got)
	}
}

func TestAForeignWritableAncestorIsFoundThroughASymlink(t *testing.T) {
	// filepath.Abs is purely lexical, so walking the lexical parents of /a/b/link/runs never
	// inspects what link actually points at. If that target is world-writable the entire ancestor
	// check is defeated by one symlink — the cheapest possible bypass of the rule.
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mode bits do not carry the same meaning on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses the permission semantics this test asserts")
	}

	exposed := secureTempDir(t)
	if err := os.Chmod(exposed, 0o777); err != nil {
		t.Fatal(err)
	}
	container := secureTempDir(t)
	link := filepath.Join(container, "link")
	if err := os.Symlink(exposed, link); err != nil {
		t.Fatal(err)
	}

	base := filepath.Join(link, "runs")
	if err := os.Mkdir(base, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(base, 0o700); err != nil {
		t.Fatal(err)
	}

	err := ensureSafeBaseDir(base)
	if err == nil {
		t.Fatal("a world-writable ancestor reached through a symlink was accepted")
	}
	if !strings.Contains(err.Error(), "0777") {
		t.Fatalf("error %q does not state the offending mode", err)
	}
}

func TestInitializeRunLeavesNoPartialOrTemporaryMarker(t *testing.T) {
	// The marker is published atomically rather than written in place under O_CREATE|O_EXCL,
	// because an exclusive create followed by a failed write leaves an EMPTY marker that is
	// indistinguishable from a real one: every retry of the run id then fails as "already
	// initialized" and every producer fails as marker-unreadable, so one transient ENOSPC would
	// brick the run id permanently.
	base := secureTempDir(t)
	own := mustInitRun(t, base, "run-1", validToken)

	entries, err := os.ReadDir(filepath.Dir(own.MarkerPath))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Fatalf("a temporary marker survived: %s", e.Name())
		}
	}

	info, err := os.Stat(own.MarkerPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Fatal("the published marker is empty")
	}
}
