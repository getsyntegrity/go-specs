package snapshots

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

type fakeBackend struct {
	mu   sync.Mutex
	msgs []string
}

func (f *fakeBackend) Fatalf(format string, args ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.msgs = append(f.msgs, fmt.Sprintf(format, args...))
}

func (f *fakeBackend) failures() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.msgs))
	copy(out, f.msgs)
	return out
}

// TestRunFromFile_ConcurrentUpdatesDoNotClobberEachOther verifies #27's fix: many goroutines
// snapshotting under distinct names into the same file, concurrently, in update mode, must not
// lose any writes to a load-mutate-save race. Before the per-file mutex, each goroutine's Save
// could overwrite another's just-written key because both started from the same on-disk read.
func TestRunFromFile_ConcurrentUpdatesDoNotClobberEachOther(t *testing.T) {
	t.Setenv(UpdateSnapshotsEnv, "1")
	dir := t.TempDir()
	callerFile := filepath.Join(dir, "concurrent_test.go")

	const n = 50
	backend := &fakeBackend{}
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			RunFromFile(backend, callerFile, fmt.Sprintf("key-%d", i), i)
		}(i)
	}
	wg.Wait()

	if msgs := backend.failures(); len(msgs) > 0 {
		t.Fatalf("expected no failures, got: %v", msgs)
	}

	snapshotPath := filepath.Join(dir, "__snapshots__", "concurrent_test.snap.json")
	data, err := Load(snapshotPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(data) != n {
		t.Fatalf("expected %d keys, got %d (lost writes to a load-mutate-save race)", n, len(data))
	}
	for i := 0; i < n; i++ {
		key := fmt.Sprintf("key-%d", i)
		if _, ok := data[key]; !ok {
			t.Errorf("missing key %q", key)
		}
	}
}

// TestRunFromFile_ConcurrentUpdatesToSameKeyDoNotCorruptFile verifies many goroutines racing to
// update the very same key (not just distinct keys) still leave a well-formed, readable file: the
// per-file mutex fully serializes each Load-mutate-Save cycle, so writes interleave cleanly instead
// of tearing mid-write.
func TestRunFromFile_ConcurrentUpdatesToSameKeyDoNotCorruptFile(t *testing.T) {
	t.Setenv(UpdateSnapshotsEnv, "1")
	dir := t.TempDir()
	callerFile := filepath.Join(dir, "samekey_test.go")

	const n = 50
	backend := &fakeBackend{}
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			RunFromFile(backend, callerFile, "shared", i)
		}(i)
	}
	wg.Wait()

	if msgs := backend.failures(); len(msgs) > 0 {
		t.Fatalf("expected no failures, got: %v", msgs)
	}

	snapshotPath := filepath.Join(dir, "__snapshots__", "samekey_test.snap.json")
	data, err := Load(snapshotPath)
	if err != nil {
		t.Fatalf("Load: %v (file corrupted by an unserialized write)", err)
	}
	if _, ok := data["shared"]; !ok {
		t.Fatal("expected key \"shared\" to be present")
	}
}

func TestRunFromFile_MissingKeyFails(t *testing.T) {
	dir := t.TempDir()
	callerFile := filepath.Join(dir, "missing_test.go")
	backend := &fakeBackend{}
	RunFromFile(backend, callerFile, "nonexistent", 42)
	msgs := backend.failures()
	if len(msgs) != 1 {
		t.Fatalf("expected exactly one failure, got %v", msgs)
	}
}

// swapRename replaces the rename seam for the duration of a test and restores it afterwards.
func swapRename(t *testing.T, fn func(oldpath, newpath string) error) {
	t.Helper()
	prev := renameSnapshot
	renameSnapshot = fn
	t.Cleanup(func() { renameSnapshot = prev })
}

// TestSave_ReplacesExistingContentWithoutTruncating verifies #152: Save must land the replacement
// through a rename, so the destination is never observed half-written. The observable contract here
// is that a successful Save fully replaces the previous content and keeps the intended file mode.
func TestSave_ReplacesExistingContentWithoutTruncating(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "atomic.snap.json")

	if err := Save(path, map[string]json.RawMessage{"first": json.RawMessage(`1`)}); err != nil {
		t.Fatalf("first Save: %v", err)
	}
	if err := Save(path, map[string]json.RawMessage{"second": json.RawMessage(`2`)}); err != nil {
		t.Fatalf("second Save: %v", err)
	}

	data, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := data["first"]; ok {
		t.Error("expected the replacement to drop the previous content")
	}
	if _, ok := data["second"]; !ok {
		t.Error("expected the replacement content to be present")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0644 {
		t.Errorf("expected mode 0644, got %o", got)
	}
}

// TestSave_WritesTemporaryFileNextToDestination verifies the temporary file shares the destination's
// directory. A rename across filesystems is not atomic, so the staging file must never land in
// os.TempDir() or anywhere else that could be a different mount.
func TestSave_WritesTemporaryFileNextToDestination(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sibling.snap.json")

	var tempPath string
	swapRename(t, func(oldpath, newpath string) error {
		tempPath = oldpath
		return os.Rename(oldpath, newpath)
	})

	if err := Save(path, map[string]json.RawMessage{"k": json.RawMessage(`1`)}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if tempPath == "" {
		t.Fatal("expected Save to stage the content through a temporary file")
	}
	if got, want := filepath.Dir(tempPath), filepath.Dir(path); got != want {
		t.Errorf("expected the temporary file in %q, got %q", want, got)
	}
	if tempPath == path {
		t.Error("expected the temporary file to be distinct from the destination")
	}
}

// TestSave_FailureBeforeReplacementPreservesDestination verifies that a failure at the final
// replacement leaves the previous snapshot readable and unchanged, and removes the staging file.
// This is the whole point of #152: an interrupted Save must never cost the caller their snapshot.
func TestSave_FailureBeforeReplacementPreservesDestination(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "preserved.snap.json")

	if err := Save(path, map[string]json.RawMessage{"original": json.RawMessage(`1`)}); err != nil {
		t.Fatalf("seed Save: %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	boom := errors.New("simulated replacement failure")
	swapRename(t, func(oldpath, newpath string) error { return boom })

	err = Save(path, map[string]json.RawMessage{"replacement": json.RawMessage(`2`)})
	if !errors.Is(err, boom) {
		t.Fatalf("expected the replacement failure to surface, got %v", err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile after failed Save: %v", err)
	}
	if string(after) != string(before) {
		t.Errorf("expected the previous content intact, got %q", string(after))
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(path) {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("expected only the destination to remain, got %v", names)
	}
}
