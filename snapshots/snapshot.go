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
// update. The lock also protects concurrent reads from observing a partially-written file mid-Save.
var fileLocks sync.Map // map[string]*sync.Mutex

func lockFor(path string) *sync.Mutex {
	key := path
	if abs, err := filepath.Abs(path); err == nil {
		key = abs
	}
	v, _ := fileLocks.LoadOrStore(key, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// RunFromFile compares value to the stored snapshot for name, or creates/updates it.
// callerFile is the path to the test file (e.g. from runtime.Caller(1) in Context.Snapshot).
// A backend that exposes Helper() is marked unconditionally, not only on failure: RunFromFile decides
// the verdict itself and reports it here, so the mark has to precede the comparison.
func RunFromFile(backend Backend, callerFile string, name string, value any) {
	if h, ok := backend.(helperBackend); ok {
		h.Helper()
	}
	if name == "" {
		backend.Fatalf("snapshot name cannot be empty")
		return
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
		return
	}

	mu := lockFor(snapshotPath)
	mu.Lock()
	defer mu.Unlock()

	update := os.Getenv(UpdateSnapshotsEnv) == "1"
	data, err := Load(snapshotPath)
	if err != nil && !os.IsNotExist(err) {
		backend.Fatalf("snapshot: load %s: %v", snapshotPath, err)
		return
	}
	if data == nil {
		data = make(map[string]json.RawMessage)
	}

	if update {
		data[name] = newBytes
		if err := os.MkdirAll(snapshotDir, 0755); err != nil {
			backend.Fatalf("snapshot: mkdir: %v", err)
			return
		}
		if err := Save(snapshotPath, data); err != nil {
			backend.Fatalf("snapshot: save: %v", err)
			return
		}
		return
	}

	existing, ok := data[name]
	if !ok {
		backend.Fatalf("snapshot %q missing; run with %s=1 to create", name, UpdateSnapshotsEnv)
		return
	}

	var existingVal, newVal any
	if err := json.Unmarshal(existing, &existingVal); err != nil {
		backend.Fatalf("snapshot: unmarshal existing: %v", err)
		return
	}
	if err := json.Unmarshal(newBytes, &newVal); err != nil {
		backend.Fatalf("snapshot: unmarshal new: %v", err)
		return
	}
	if !reflect.DeepEqual(existingVal, newVal) {
		backend.Fatalf("snapshot %q mismatch:\nexpected (snapshot): %s\ngot: %s", name, string(existing), string(newBytes))
		return
	}
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

// Save writes a snapshot file with deterministic key order.
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
	return os.WriteFile(path, out, 0644)
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
