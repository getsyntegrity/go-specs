package coordination

import (
	"os"
	"testing"
)

// secureTempDir returns a temporary directory that satisfies contract v1.2.6 §10 rule 2.
//
// t.TempDir() is not usable as-is: it creates its numbered subdirectory with os.Mkdir(…, 0777),
// so under the common umask 002 the result is 0775 — group-writable, which ensureSafeBaseDir
// refuses by design. Tightening it here keeps the tests exercising the code path that matters
// instead of the refusal, and the refusal itself is covered explicitly in safety_test.go.
func secureTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatalf("chmod %s: %v", dir, err)
	}
	return dir
}
