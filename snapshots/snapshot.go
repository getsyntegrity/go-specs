package snapshots

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"sync"
)

const UpdateSnapshotsEnv = "GO_SPECS_UPDATE_SNAPSHOTS"

// Backend is used to report failures (e.g. testing.TB).
type Backend interface {
	Fatalf(format string, args ...any)
}

// helperBackend is the optional Helper() half of testing.TB. RunFromFile marks itself through it so
// snapshot mismatches are attributed to the caller's ctx.Snapshot line, not to snapshot.go.
type helperBackend interface {
	Helper()
}

// fileLocks serializes RunFromFile's load-mutate-save cycle per snapshot file, keyed by absolute
// path. Without it, two specs sharing a snapshot file (e.g. parallel specs in the same test file,
// each snapshotting under a different name) race: both Load the same on-disk contents, each mutates
// its own key in its own in-memory copy, and whichever Save runs last silently clobbers the other's
// update. Its scope is this process only: it does not coordinate independent `go test` package
// processes, and it is not what makes a write crash-safe. Save handles that on its own by replacing
// the destination atomically.
var fileLocks sync.Map // map[string]*sync.Mutex

func lockFor(path string) *sync.Mutex {
	key := path
	if abs, err := filepath.Abs(path); err == nil {
		key = abs
	}
	v, _ := fileLocks.LoadOrStore(key, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// RunFromFile compares value to the stored snapshot for name, or creates/updates it. It reports any
// failure to backend itself and also returns whether the comparison passed, so a caller that tracks
// its own pass/fail state (e.g. specs.Context) can fold the verdict in without re-deciding it.
// callerFile is the path to the test file (e.g. from runtime.Caller(1) in Context.Snapshot).
// A backend that exposes Helper() is marked unconditionally, not only on failure: RunFromFile decides
// the verdict itself and reports it here, so the mark has to precede the comparison.
func RunFromFile(backend Backend, callerFile string, name string, value any) bool {
	if h, ok := backend.(helperBackend); ok {
		h.Helper()
	}
	if name == "" {
		backend.Fatalf("snapshot name cannot be empty")
		return false
	}
	dir := filepath.Dir(callerFile)
	base := filepath.Base(callerFile)
	ext := filepath.Ext(base)
	namePart := base
	if len(ext) > 0 && len(base) > len(ext) {
		namePart = base[:len(base)-len(ext)]
	}
	snapshotDir := filepath.Join(dir, "__snapshots__")
	snapshotPath := filepath.Join(snapshotDir, namePart+".snap.json")

	newBytes, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		backend.Fatalf("snapshot: marshal: %v", err)
		return false
	}

	mu := lockFor(snapshotPath)
	mu.Lock()
	defer mu.Unlock()

	update := os.Getenv(UpdateSnapshotsEnv) == "1"
	data, err := Load(snapshotPath)
	if err != nil && !os.IsNotExist(err) {
		backend.Fatalf("snapshot: load %s: %v", snapshotPath, err)
		return false
	}
	if data == nil {
		data = make(map[string]json.RawMessage)
	}

	if update {
		data[name] = newBytes
		if err := os.MkdirAll(snapshotDir, 0755); err != nil {
			backend.Fatalf("snapshot: mkdir: %v", err)
			return false
		}
		if err := Save(snapshotPath, data); err != nil {
			backend.Fatalf("snapshot: save: %v", err)
			return false
		}
		return true
	}

	existing, ok := data[name]
	if !ok {
		backend.Fatalf("snapshot %q missing; run with %s=1 to create", name, UpdateSnapshotsEnv)
		return false
	}

	var existingVal, newVal any
	if err := json.Unmarshal(existing, &existingVal); err != nil {
		backend.Fatalf("snapshot: unmarshal existing: %v", err)
		return false
	}
	if err := json.Unmarshal(newBytes, &newVal); err != nil {
		backend.Fatalf("snapshot: unmarshal new: %v", err)
		return false
	}
	if !reflect.DeepEqual(existingVal, newVal) {
		backend.Fatalf("snapshot %q mismatch:\nexpected (snapshot): %s\ngot: %s", name, string(existing), string(newBytes))
		return false
	}
	return true
}

// Load reads a snapshot file.
func Load(path string) (map[string]json.RawMessage, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var data map[string]json.RawMessage
	if err := json.Unmarshal(b, &data); err != nil {
		return nil, fmt.Errorf("parse snapshot file: %w", err)
	}
	return data, nil
}

// renameSnapshot is Save's final replacement step, indirected so tests can simulate a failure that
// must leave the previous snapshot intact.
var renameSnapshot = os.Rename

// snapshotFileMode is the mode a snapshot file is published with, regardless of the restrictive mode
// the staging file is created under.
const snapshotFileMode os.FileMode = 0644

// Save writes a snapshot file with deterministic key order. The rendered content is staged in a
// temporary file and swapped over the destination with a single rename, so an interrupted or failing
// Save never leaves a truncated snapshot behind: a reader sees either the previous file or the
// complete new one, never a half-written mix.
func Save(path string, data map[string]json.RawMessage) error {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := []byte("{\n")
	for i, k := range keys {
		raw := data[k]
		indented := indentJSON(raw, "  ")
		keyQuoted, _ := json.Marshal(k)
		if i > 0 {
			out = append(out, ",\n"...)
		}
		out = append(out, "  "...)
		out = append(out, keyQuoted...)
		out = append(out, ": "...)
		out = append(out, indented...)
	}
	out = append(out, "\n}\n"...)
	return writeAtomic(path, out)
}

// writeAtomic publishes content at path through a temporary file in the same directory. Staging
// beside the destination is what keeps the final rename atomic: a rename across filesystems is not,
// so a shared temp directory would silently give up the guarantee. Every failure path removes the
// staging file and leaves the destination untouched.
func writeAtomic(path string, out []byte) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp snapshot file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		if err != nil {
			// Both are best-effort: the write already failed, and a stale temp file is the only
			// thing left to clean up.
			_ = tmp.Close()
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err = tmp.Write(out); err != nil {
		return fmt.Errorf("write temp snapshot file: %w", err)
	}
	// Flush to disk before the rename, so a crash right after the rename cannot expose a destination
	// that points at unwritten content.
	if err = tmp.Sync(); err != nil {
		return fmt.Errorf("sync temp snapshot file: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("close temp snapshot file: %w", err)
	}
	// CreateTemp opens at 0600; publish under the mode snapshot files are expected to carry.
	if err = os.Chmod(tmpPath, snapshotFileMode); err != nil {
		return fmt.Errorf("chmod temp snapshot file: %w", err)
	}
	if err = renameSnapshot(tmpPath, path); err != nil {
		return fmt.Errorf("replace snapshot file: %w", err)
	}
	return nil
}

func indentJSON(raw []byte, indent string) []byte {
	if len(raw) == 0 {
		return raw
	}
	var buf []byte
	buf = append(buf, indent...)
	for _, b := range raw {
		buf = append(buf, b)
		if b == '\n' {
			buf = append(buf, indent...)
		}
	}
	return buf
}
