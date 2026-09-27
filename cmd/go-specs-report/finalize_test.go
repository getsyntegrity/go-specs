package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getsyntegrity/go-specs/report/coordination"
)

// finalize_test.go pins T3 of issue #146: the `finalize` verb, which wraps coordination.Finalize +
// coordination.ExitCode without duplicating ownership, merge or render logic (contract v1.2.9 §7,
// §8's finalize-side ownership row).

const (
	producerA = "example.com/m/a"
	producerB = "example.com/m/b"
)

func finalizeTargets(t *testing.T, dir string) (jsonPath, xmlPath, txtPath, htmlPath string) {
	t.Helper()
	return filepath.Join(dir, "report.json"),
		filepath.Join(dir, "report.xml"),
		filepath.Join(dir, "report.txt"),
		filepath.Join(dir, "report.html")
}

func TestFinalizeExitsZeroWritesEveryTargetAndIgnoresTestFailures(t *testing.T) {
	base := secureTempDir(t)
	own := initRun(t, base, "run-1", validToken)
	writeShard(t, base, own, "run-1", validToken, producerA, passingReport())
	writeShard(t, base, own, "run-1", validToken, producerB, failingReport())

	out := t.TempDir()
	jsonPath, xmlPath, txtPath, htmlPath := finalizeTargets(t, out)
	manifest := writeManifest(t, t.TempDir(), producerA, producerB)

	_, stderr, code := runCLI(t, []string{
		"finalize",
		"-run-id", "run-1", "-token", string(validToken), "-report-dir", base,
		"-producers", manifest,
		"-json", jsonPath, "-xml", xmlPath, "-txt", txtPath, "-html", htmlPath,
	}, nil)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (a failing test case must not change it); stderr=%s", code, stderr)
	}
	for _, p := range []string{jsonPath, xmlPath, txtPath, htmlPath} {
		if info, err := os.Stat(p); err != nil || info.Size() == 0 {
			t.Fatalf("target %s was not written (err=%v)", p, err)
		}
	}
}

func TestFinalizeOwnershipAndConfigurationFailuresExit78WithNoOutput(t *testing.T) {
	cases := map[string]func(t *testing.T, base, manifest string) []string{
		"wrong token": func(t *testing.T, base, manifest string) []string {
			return []string{"finalize", "-run-id", "run-1", "-token", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"-report-dir", base, "-producers", manifest}
		},
		"wrong run-id, no marker": func(t *testing.T, base, manifest string) []string {
			return []string{"finalize", "-run-id", "never-initialized", "-token", string(validToken),
				"-report-dir", base, "-producers", manifest}
		},
		"relative report dir": func(t *testing.T, base, manifest string) []string {
			return []string{"finalize", "-run-id", "run-1", "-token", string(validToken),
				"-report-dir", "relative/runs", "-producers", manifest}
		},
		"missing -producers flag": func(t *testing.T, base, manifest string) []string {
			return []string{"finalize", "-run-id", "run-1", "-token", string(validToken), "-report-dir", base}
		},
		"empty manifest": func(t *testing.T, base, manifest string) []string {
			empty := writeManifest(t, t.TempDir(), "", "# only a comment")
			return []string{"finalize", "-run-id", "run-1", "-token", string(validToken),
				"-report-dir", base, "-producers", empty}
		},
		"unreadable manifest": func(t *testing.T, base, manifest string) []string {
			return []string{"finalize", "-run-id", "run-1", "-token", string(validToken),
				"-report-dir", base, "-producers", filepath.Join(t.TempDir(), "does-not-exist.txt")}
		},
	}

	for name, buildArgs := range cases {
		t.Run(name, func(t *testing.T) {
			base := secureTempDir(t)
			own := initRun(t, base, "run-1", validToken)
			writeShard(t, base, own, "run-1", validToken, producerA, passingReport())
			manifest := writeManifest(t, t.TempDir(), producerA)

			out := t.TempDir()
			jsonPath, xmlPath, txtPath, htmlPath := finalizeTargets(t, out)
			args := append(buildArgs(t, base, manifest), "-json", jsonPath, "-xml", xmlPath, "-txt", txtPath, "-html", htmlPath)

			_, stderr, code := runCLI(t, args, nil)
			if code != coordination.ExitConfig {
				t.Fatalf("exit code = %d, want %d (ExitConfig); stderr=%s", code, coordination.ExitConfig, stderr)
			}
			for _, p := range []string{jsonPath, xmlPath, txtPath, htmlPath} {
				if _, err := os.Stat(p); err == nil {
					t.Fatalf("target %s was written despite a configuration failure", p)
				}
			}
		})
	}
}

func TestFinalizeReportingFailureExitsOneNeverEXitConfig(t *testing.T) {
	t.Run("missing shard for an expected producer", func(t *testing.T) {
		base := secureTempDir(t)
		own := initRun(t, base, "run-1", validToken)
		writeShard(t, base, own, "run-1", validToken, producerA, passingReport())
		manifest := writeManifest(t, t.TempDir(), producerA, producerB) // producerB never publishes

		_, stderr, code := runCLI(t, []string{
			"finalize", "-run-id", "run-1", "-token", string(validToken), "-report-dir", base, "-producers", manifest,
		}, nil)
		if code != coordination.ExitReportingFailure {
			t.Fatalf("exit code = %d, want %d (ExitReportingFailure)", code, coordination.ExitReportingFailure)
		}
		if !strings.Contains(stderr, producerB) {
			t.Fatalf("stderr does not name the missing producer %s:\n%s", producerB, stderr)
		}
	})

	t.Run("corrupt shard is rejected", func(t *testing.T) {
		base := secureTempDir(t)
		own := initRun(t, base, "run-1", validToken)
		writeShard(t, base, own, "run-1", validToken, producerA, passingReport())
		writeShard(t, base, own, "run-1", validToken, producerB, passingReport())
		corruptShard(t, own, producerB)
		manifest := writeManifest(t, t.TempDir(), producerA, producerB)

		_, _, code := runCLI(t, []string{
			"finalize", "-run-id", "run-1", "-token", string(validToken), "-report-dir", base, "-producers", manifest,
		}, nil)
		if code != coordination.ExitReportingFailure {
			t.Fatalf("exit code = %d, want %d (ExitReportingFailure)", code, coordination.ExitReportingFailure)
		}
	})
}

func TestFinalizationBarrierNeverMergesAPartialSetAsSuccess(t *testing.T) {
	base := secureTempDir(t)
	own := initRun(t, base, "run-1", validToken)
	writeShard(t, base, own, "run-1", validToken, producerA, passingReport())
	manifest := writeManifest(t, t.TempDir(), producerA, producerB)

	args := []string{"finalize", "-run-id", "run-1", "-token", string(validToken), "-report-dir", base, "-producers", manifest}

	if _, _, code := runCLI(t, args, nil); code != coordination.ExitReportingFailure {
		t.Fatalf("first finalize (producerB missing): exit code = %d, want %d", code, coordination.ExitReportingFailure)
	}

	writeShard(t, base, own, "run-1", validToken, producerB, passingReport())

	if _, stderr, code := runCLI(t, args, nil); code != 0 {
		t.Fatalf("second finalize (both producers published): exit code = %d, want 0; stderr=%s", code, stderr)
	}
}

func TestFinalizeConfigErrorRecordExits78(t *testing.T) {
	base := secureTempDir(t)
	initRun(t, base, "run-1", validToken)
	if err := coordination.WriteConfigError(base, "run-1", producerA, coordination.ReasonMissingRunToken, "diag"); err != nil {
		t.Fatal(err)
	}
	manifest := writeManifest(t, t.TempDir(), producerA)

	_, stderr, code := runCLI(t, []string{
		"finalize", "-run-id", "run-1", "-token", string(validToken), "-report-dir", base, "-producers", manifest,
	}, nil)
	if code != coordination.ExitConfig {
		t.Fatalf("exit code = %d, want %d (ExitConfig); stderr=%s", code, coordination.ExitConfig, stderr)
	}
}
