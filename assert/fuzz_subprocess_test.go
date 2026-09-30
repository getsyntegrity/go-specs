package assert

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Subprocess replay of the fuzz inputs.
//
// A fuzz target runs its diagnostic in the fuzzing process, protected only by the fixes that make a
// stack overflow impossible; recover cannot catch one. A regression there would not fail a test, it
// would kill the whole test binary and hide every other result. So the seed recipes (the dangerous
// shapes of fuzz_seeds_test.go), every committed testdata/fuzz corpus file and every named
// regression also run here, each in its own child process: this test binary re-executed with
// fzReplayEnv set and -test.run pointing at TestFuzzReplayChild, under a timeout. A fatal error, a
// hang or a panic in the child fails only the subtest that started it.

const (
	fzReplayEnv         = "GOSPECS_FUZZ_REPLAY" // "<target>:<hex recipe>", target "*" for all of them
	fzSubprocessTimeout = 60 * time.Second
	fzSelfTestOverflow  = "selftest-stack-overflow"
)

// fzChildSlots limits how many children run at once.
var fzChildSlots = make(chan struct{}, 4)

// TestFuzzReplayChild is the child half. It does nothing unless fzReplayEnv is set.
func TestFuzzReplayChild(t *testing.T) {
	spec := os.Getenv(fzReplayEnv)
	if spec == "" {
		t.Skip("runs only as a child of the subprocess tests")
	}
	target, encoded, _ := strings.Cut(spec, ":")
	if target == fzSelfTestOverflow {
		debug.SetMaxStack(8 << 20) // keep the deliberate overflow quick and small
		fzOverflow(0)
		return
	}
	data, err := hex.DecodeString(encoded)
	if err != nil {
		t.Fatalf("bad recipe in %s: %v", fzReplayEnv, err)
	}
	names := []string{target}
	if target == "*" {
		names = names[:0]
		for name := range fzTargets {
			names = append(names, name)
		}
		sort.Strings(names)
	}
	for _, name := range names {
		run, ok := fzTargets[name]
		if !ok {
			t.Fatalf("unknown fuzz target %q", name)
		}
		for _, p := range fzCheck(data, run) {
			t.Errorf("%s: %s", name, p)
		}
	}
}

// fzOverflow recurses without end: the fatal error only a child process can survive.
func fzOverflow(n int) int {
	var pad [128]byte
	pad[n%len(pad)] = byte(n)
	return fzOverflow(n+1) + int(pad[n%len(pad)]) //nolint:staticcheck // SA5007: the endless recursion is the point
}

// fzReplayInChild runs one recipe against a target in a child process and returns its combined
// output and how it ended. A hang ends as context.DeadlineExceeded.
func fzReplayInChild(target string, data []byte) (string, error) {
	fzChildSlots <- struct{}{}
	defer func() { <-fzChildSlots }()
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), fzSubprocessTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestFuzzReplayChild$", "-test.count=1", "-test.v")
	cmd.Env = append(os.Environ(), fzReplayEnv+"="+target+":"+hex.EncodeToString(data))
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err = cmd.Run()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	return out.String(), err
}

// fzReport fails t with the tail of the child's output.
func fzReport(t *testing.T, what string, out string, err error) {
	t.Helper()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) > 40 {
		lines = append([]string{"..."}, lines[len(lines)-40:]...)
	}
	kind := "the child failed"
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		kind = "the child did not finish within " + fzSubprocessTimeout.String() + " (a hang)"
	case strings.Contains(out, "stack overflow"):
		kind = "the child overflowed the stack"
	case strings.Contains(out, "panic:"):
		kind = "the child panicked"
	}
	t.Errorf("%s: %s: %v\n%s", what, kind, err, strings.Join(lines, "\n"))
}

func TestFuzzSeedsSubprocess(t *testing.T) {
	for _, s := range fzSeeds() {
		t.Run(s.name, func(t *testing.T) {
			t.Parallel()
			if out, err := fzReplayInChild("*", s.data); err != nil {
				fzReport(t, "seed "+s.name, out, err)
			}
		})
	}
}

// TestFuzzCorpusSubprocess replays every committed testdata/fuzz/<Target>/<file>: the inputs the
// fuzzer once found, kept as regressions. The Go tool replays them in process on plain `go test`
// too; this is the crash-proof copy.
func TestFuzzCorpusSubprocess(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "fuzz", "*", "*"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		target := filepath.Base(filepath.Dir(path))
		if _, ok := fzTargets[target]; !ok {
			t.Errorf("%s: no fuzz target named %s", path, target)
			continue
		}
		t.Run(target+"/"+filepath.Base(path), func(t *testing.T) {
			t.Parallel()
			data, err := fzReadCorpusFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if out, err := fzReplayInChild(target, data); err != nil {
				fzReport(t, path, out, err)
			}
		})
	}
}

// fzRegressions are the inputs the fuzz targets found a defect with. Each has its corpus file under
// testdata/fuzz/<target>/<name> and a named unit test in fuzz_regression_test.go that fails without
// the fix; this table replays the recipe in a child process as well.
var fzRegressions = []struct {
	name, target string
	data         []byte
}{
	{"budget-exhausted-while-rendering-map-keys", "FuzzPollMessage", []byte("B2cB000B0")},
}

func TestFuzzRegressionsSubprocess(t *testing.T) {
	for _, r := range fzRegressions {
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			committed, err := fzReadCorpusFile(filepath.Join("testdata", "fuzz", r.target, r.name))
			if err != nil {
				t.Fatalf("the regression has no committed corpus file: %v", err)
			}
			if !bytes.Equal(committed, r.data) {
				t.Fatalf("the corpus file holds %q, the table %q", committed, r.data)
			}
			if out, err := fzReplayInChild(r.target, r.data); err != nil {
				fzReport(t, r.name, out, err)
			}
		})
	}
}

// TestFuzzSubprocessContainsAStackOverflow proves the isolation works: a child that overflows its
// stack fails its own run, with the runtime's fatal error in its output, and this test carries on.
func TestFuzzSubprocessContainsAStackOverflow(t *testing.T) {
	out, err := fzReplayInChild(fzSelfTestOverflow, nil)
	if err == nil {
		t.Fatalf("a child that overflows its stack reported success:\n%s", out)
	}
	if !strings.Contains(out, "stack overflow") {
		t.Fatalf("the child failed, but not with a stack overflow (%v):\n%s", err, out)
	}
}

// fzReadCorpusFile reads a `go test fuzz v1` corpus file holding one []byte.
func fzReadCorpusFile(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 || lines[0] != "go test fuzz v1" {
		return nil, fmt.Errorf("%s: not a single-value go test fuzz v1 file", path)
	}
	body, ok := strings.CutPrefix(lines[1], "[]byte(")
	body, ok2 := strings.CutSuffix(body, ")")
	if !ok || !ok2 {
		return nil, fmt.Errorf("%s: the value is not a []byte", path)
	}
	s, err := strconv.Unquote(body)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return []byte(s), nil
}

func TestFuzzReadCorpusFile(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	got, err := fzReadCorpusFile(write("ok", "go test fuzz v1\n[]byte(\"\\x01\\xffab\")\n"))
	if err != nil || !bytes.Equal(got, []byte{1, 0xff, 'a', 'b'}) {
		t.Fatalf("got %x, %v", got, err)
	}
	for name, content := range map[string]string{
		"header":  "go test fuzz v0\n[]byte(\"a\")\n",
		"type":    "go test fuzz v1\nstring(\"a\")\n",
		"two":     "go test fuzz v1\n[]byte(\"a\")\nint(1)\n",
		"quoting": "go test fuzz v1\n[]byte(a)\n",
	} {
		if _, err := fzReadCorpusFile(write(name, content)); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}
