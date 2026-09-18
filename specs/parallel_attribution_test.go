package specs

import (
	"bufio"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// This file pins #108's fix: an ItParallel failure's recorded message embeds the user's own
// assertion file:line (see parallelCallerLocation and failureRecord in scheduler.go). It runs the
// fixture in testdata/parallel_attribution as a real `go test` subprocess, for the same reason
// attribution_test.go does: the thing under test is what `go test` actually prints.
//
// KNOWN LIMITATION, deliberately asserted here rather than hidden: unlike the sequential path (see
// attribution_test.go), Go's own primary-decorated location — the line immediately after
// "--- FAIL: TestXxx" — still names an internal go-specs frame, never the fixture file. By the time
// reportFailures's tb.Fatalf runs, the worker goroutine that ran the user's assertion has already
// exited, so there is no live frame left for tb.Helper() to mark transparent (see
// parallelCallerLocation's doc comment). The user's file:line is instead embedded as text inside the
// Fatalf message itself, the only place a plain `go test` run can show it at all.

const (
	parallelAttributionFixtureDir  = "testdata/parallel_attribution"
	parallelAttributionFixtureFile = "parallel_attribution_test.go"
)

var (
	parallelAttributionWantRe     = regexp.MustCompile(`// want:(\w+)`)
	parallelAttributionFailRe     = regexp.MustCompile(`^--- FAIL: (Test\w+)`)
	parallelAttributionLocRe      = regexp.MustCompile(`^\s+([\w./-]+\.go):(\d+): `)
	parallelAttributionEmbeddedRe = regexp.MustCompile(regexp.QuoteMeta(parallelAttributionFixtureFile) + `:(\d+):`)
)

// TestParallelAssertionSourceAttribution runs the fixture package with a real `go test` and proves
// that an ItParallel assertion failure's message embeds the user's own file and line, while Go's own
// primary-decorated location still names an internal go-specs frame (the known limitation above).
func TestParallelAssertionSourceAttribution(t *testing.T) {
	output := runParallelAttributionFixture(t)

	t.Run("embeds each assertion's own call site in the failure message", func(t *testing.T) {
		assertEmbeddedAtWantedLines(t, output)
	})
	t.Run("Go's own primary location still names an internal frame", func(t *testing.T) {
		assertParallelPrimaryLocationIsInternal(t, output)
	})
}

// assertEmbeddedAtWantedLines checks each `// want:Key` marker in the fixture against the file:line
// embedded in the message reported for Test<Key>.
func assertEmbeddedAtWantedLines(t *testing.T, output string) {
	t.Helper()

	want := parseParallelAttributionWants(t)
	if len(want) == 0 {
		t.Fatalf("fixture %s declares no `// want:` markers", parallelAttributionFixtureFile)
	}
	got := parseParallelAttributionEmbedded(t, output)

	for key, wantLine := range want {
		testName := "Test" + key
		gotLine, ok := got[testName]
		if !ok {
			t.Errorf("%s: no embedded location found in its failure message\nfull output:\n%s",
				testName, output)
			continue
		}
		if gotLine != wantLine {
			t.Errorf("%s: message embedded %s:%d, want %s:%d",
				testName, parallelAttributionFixtureFile, gotLine, parallelAttributionFixtureFile, wantLine)
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

// assertParallelPrimaryLocationIsInternal documents and pins the known limitation: Go's own
// primary-decorated location (unlike the sequential path) never names the fixture file, since the
// worker goroutine that ran the user's assertion is already gone by the time reportFailures calls
// tb.Fatalf. If this ever starts naming the fixture file, the limitation this file documents no
// longer holds and the comment above needs updating, not just this assertion.
func assertParallelPrimaryLocationIsInternal(t *testing.T, output string) {
	t.Helper()

	scanner := bufio.NewScanner(strings.NewReader(output))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	current := ""
	seen := make(map[string]bool)
	for scanner.Scan() {
		line := scanner.Text()
		if m := parallelAttributionFailRe.FindStringSubmatch(line); m != nil {
			current = m[1]
			continue
		}
		m := parallelAttributionLocRe.FindStringSubmatch(line)
		if m == nil || current == "" || seen[current] {
			continue
		}
		seen[current] = true
		if file := filepath.Base(m[1]); file == parallelAttributionFixtureFile {
			t.Errorf("%s: primary location unexpectedly named the fixture file %s — the known limitation "+
				"this file documents no longer holds: %q", current, file, strings.TrimSpace(line))
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanning go test output: %v", err)
	}
	if len(seen) == 0 {
		t.Fatalf("no primary failure location found in output:\n%s", output)
	}
}

// parseParallelAttributionWants maps each `// want:Key` marker in the fixture to the 1-based line it
// sits on, so the expectations track the fixture as it is edited instead of hardcoding line numbers.
func parseParallelAttributionWants(t *testing.T) map[string]int {
	t.Helper()

	path := filepath.Join(parallelAttributionFixtureDir, parallelAttributionFixtureFile)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", path, err)
	}

	want := make(map[string]int)
	for i, line := range strings.Split(string(data), "\n") {
		m := parallelAttributionWantRe.FindStringSubmatch(line)
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

// runParallelAttributionFixture runs `go test` over the fixture package and returns its combined
// output. The fixture is expected to fail, so a non-zero exit status is the success case here.
func runParallelAttributionFixture(t *testing.T) string {
	t.Helper()

	goBin := findGoBinary(t)

	cmd := exec.Command(goBin, "test", "-count=1", "./"+parallelAttributionFixtureDir)
	out, err := cmd.CombinedOutput()
	output := string(out)
	if err == nil {
		t.Fatalf("expected the parallel attribution fixture to fail, but it passed:\n%s", output)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("running go test on the parallel attribution fixture: %v\n%s", err, output)
	}
	return output
}

// parseParallelAttributionEmbedded maps each top-level failing test to the line number embedded in
// its failure message via parallelAttributionEmbeddedRe (the fixture's own basename, since File comes
// straight from runtime.CallersFrames rather than Go's own trimmed decoration — see
// parallelCallerLocation).
func parseParallelAttributionEmbedded(t *testing.T, output string) map[string]int {
	t.Helper()

	got := make(map[string]int)
	current := ""
	scanner := bufio.NewScanner(strings.NewReader(output))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if m := parallelAttributionFailRe.FindStringSubmatch(line); m != nil {
			current = m[1]
			continue
		}
		if current == "" {
			continue
		}
		m := parallelAttributionEmbeddedRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if _, seen := got[current]; seen {
			continue
		}
		lineNo, err := strconv.Atoi(m[1])
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
