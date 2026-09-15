package specs

import (
	"bufio"
	"errors"
	"go/build"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// This file pins the source-attribution contract: a failing built-in assertion must report the
// user's assertion line, never a go-specs frame.
//
// It deliberately uses the standard library rather than the go-specs DSL. The thing under test is
// what `go test` prints for a go-specs failure, so the proof has to come from a real `go test`
// subprocess over a real *testing.T — a fake backend cannot observe testing.T.Helper's frame
// bookkeeping at all, and that bookkeeping is the entire mechanism. Asserting on the raw subprocess
// output also keeps the proof independent of the assertion API it is proving.
//
// The fixture lives in testdata/attribution so `go test ./...` never runs it directly; every spec
// in it fails on purpose.

const (
	attributionFixtureDir  = "testdata/attribution"
	attributionFixtureFile = "attribution_test.go"
	// updateSnapshotsEnv mirrors snapshots.UpdateSnapshotsEnv; the specs package does not import
	// that constant and this test only needs to blank it out for the subprocess.
	updateSnapshotsEnv = "GO_SPECS_UPDATE_SNAPSHOTS"
)

// goSpecsInternalFiles are files that must never appear as a failure location. testing_backend.go
// and context.go are the two frames the pre-fix code actually reported.
var goSpecsInternalFiles = []string{
	"testing_backend.go",
	"context.go",
	"matcher.go",
	"snapshot.go",
	"runner.go",
	"execution_plan.go",
}

var (
	attributionWantRe = regexp.MustCompile(`// want:(\w+)`)
	attributionFailRe = regexp.MustCompile(`^--- FAIL: (Test\w+)`)
	attributionLocRe  = regexp.MustCompile(`^\s+([\w./-]+\.go):(\d+): `)
)

// TestAssertionSourceAttribution runs the fixture package with a real `go test` and proves that each
// built-in assertion surface reports the user's own file and line.
func TestAssertionSourceAttribution(t *testing.T) {
	output := runAttributionFixture(t)

	t.Run("attributes every assertion to its own call site", func(t *testing.T) {
		assertAttributedToWantedLines(t, output)
	})
	t.Run("never names a go-specs internal frame", func(t *testing.T) {
		assertNoInternalFrames(t, output)
	})
}

// assertAttributedToWantedLines checks each `// want:Key` marker in the fixture against the location
// go test reported for Test<Key>.
func assertAttributedToWantedLines(t *testing.T, output string) {
	t.Helper()

	want := parseAttributionWants(t)
	if len(want) == 0 {
		t.Fatalf("fixture %s declares no `// want:` markers", attributionFixtureFile)
	}
	got := parseAttributionFailures(t, output)

	for key, wantLine := range want {
		testName := "Test" + key
		gotLine, ok := got[testName]
		if !ok {
			t.Errorf("%s: no failure location reported in %s\nfull output:\n%s",
				testName, attributionFixtureFile, output)
			continue
		}
		if gotLine != wantLine {
			t.Errorf("%s: failure attributed to %s:%d, want %s:%d",
				testName, attributionFixtureFile, gotLine, attributionFixtureFile, wantLine)
		}
	}

	for testName := range got {
		key := strings.TrimPrefix(testName, "Test")
		if _, ok := want[key]; !ok {
			t.Errorf("%s failed but declares no `// want:%s` marker; every failing fixture spec must pin its expected line",
				testName, key)
		}
	}
}

// assertNoInternalFrames is the negative half of the contract: no line of the fixture's failure
// output may use a go-specs source file as its location. Checking only that the expected lines
// appear would still pass if a second, internal location were reported alongside.
func assertNoInternalFrames(t *testing.T, output string) {
	t.Helper()

	scanner := bufio.NewScanner(strings.NewReader(output))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		m := attributionLocRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		file := filepath.Base(m[1])
		for _, internal := range goSpecsInternalFiles {
			if file == internal {
				t.Errorf("failure attributed to go-specs internal frame %s: %q", internal, strings.TrimSpace(line))
			}
		}
		if file != attributionFixtureFile {
			t.Errorf("failure attributed to unexpected file %s, want %s: %q",
				file, attributionFixtureFile, strings.TrimSpace(line))
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanning go test output: %v", err)
	}
}

// parseAttributionWants maps each `// want:Key` marker in the fixture to the 1-based line it sits
// on, so the expectations track the fixture as it is edited instead of hardcoding line numbers.
func parseAttributionWants(t *testing.T) map[string]int {
	t.Helper()

	path := filepath.Join(attributionFixtureDir, attributionFixtureFile)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", path, err)
	}

	want := make(map[string]int)
	for i, line := range strings.Split(string(data), "\n") {
		m := attributionWantRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if prev, dup := want[m[1]]; dup {
			t.Fatalf("fixture declares `// want:%s` twice (lines %d and %d); keys must be unique", m[1], prev, i+1)
		}
		want[m[1]] = i + 1
	}
	return want
}

// runAttributionFixture runs `go test` over the fixture package and returns its combined output. The
// fixture is expected to fail, so a non-zero exit status is the success case here.
func runAttributionFixture(t *testing.T) string {
	t.Helper()

	goBin := findGoBinary(t)

	cmd := exec.Command(goBin, "test", "-count=1", "./"+attributionFixtureDir)
	// A set GO_SPECS_UPDATE_SNAPSHOTS would rewrite the fixture's stored snapshot and turn the
	// snapshot spec green, silently dropping it from the proof.
	cmd.Env = append(os.Environ(), updateSnapshotsEnv+"=")
	out, err := cmd.CombinedOutput()
	output := string(out)
	if err == nil {
		t.Fatalf("expected the attribution fixture to fail, but it passed:\n%s", output)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("running go test on the attribution fixture: %v\n%s", err, output)
	}
	return output
}

// parseAttributionFailures maps each top-level failing test to the first fixture line reported as
// its failure location. Top-level `--- FAIL:` lines start at column zero while subtest ones are
// indented, so the scan attributes every location to its enclosing top-level test.
func parseAttributionFailures(t *testing.T, output string) map[string]int {
	t.Helper()

	got := make(map[string]int)
	current := ""
	scanner := bufio.NewScanner(strings.NewReader(output))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if m := attributionFailRe.FindStringSubmatch(line); m != nil {
			current = m[1]
			continue
		}
		m := attributionLocRe.FindStringSubmatch(line)
		if m == nil || current == "" {
			continue
		}
		if filepath.Base(m[1]) != attributionFixtureFile {
			continue
		}
		if _, seen := got[current]; seen {
			continue
		}
		lineNo, err := strconv.Atoi(m[2])
		if err != nil {
			t.Fatalf("parsing line number from %q: %v", line, err)
		}
		got[current] = lineNo
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanning go test output: %v", err)
	}
	return got
}

// findGoBinary locates the go command: PATH first, then GOROOT, which covers a toolchain invoked by
// absolute path with no matching PATH entry.
func findGoBinary(t *testing.T) string {
	t.Helper()

	if path, err := exec.LookPath("go"); err == nil {
		return path
	}
	if root := build.Default.GOROOT; root != "" {
		candidate := filepath.Join(root, "bin", "go")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	t.Skip("go command not found on PATH or in GOROOT; cannot run the attribution fixture")
	return ""
}
