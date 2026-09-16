package report

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// compareGolden compares actual against testdata/golden/<name>. Run with UPDATE_GOLDEN=1 to
// (re)write the fixture from the current renderer output after a deliberate format change.
func compareGolden(t *testing.T, name string, actual []byte) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name)
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create golden dir: %v", err)
		}
		if err := os.WriteFile(path, actual, 0o644); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run `UPDATE_GOLDEN=1 go test ./report/...` to create it)", path, err)
	}
	if !bytes.Equal(want, actual) {
		t.Fatalf("golden mismatch for %s\n--- want ---\n%s\n--- got ---\n%s", path, want, actual)
	}
}
