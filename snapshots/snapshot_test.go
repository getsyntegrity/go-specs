package snapshots

import (
	"fmt"
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
