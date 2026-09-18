package coordination

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const (
	fixtureAlpha = "github.com/getsyntegrity/go-specs/report/coordination/internal/shardfixture/alpha"
	fixtureBeta  = "github.com/getsyntegrity/go-specs/report/coordination/internal/shardfixture/beta"
	fixturePkgs  = "./report/coordination/internal/shardfixture/..."
)

// repoRoot locates the module root from this file's own path, so the integration tests do not
// depend on the working directory `go test` happened to use.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate this test file")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

type goTestResult struct {
	out      string
	exitCode int
}

func (r goTestResult) cached() bool { return strings.Contains(r.out, "(cached)") }

// runGoTest invokes the real toolchain against the fixture packages. These tests exist because the
// claims they cover are toolchain behaviour: nothing observable from inside one process can tell
// you whether a package was served from cache or whether cmd/go forwarded an exit code.
func runGoTest(t *testing.T, env map[string]string, args ...string) goTestResult {
	t.Helper()
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("no go toolchain on PATH: %v", err)
	}

	cmd := exec.CommandContext(context.Background(), gobin, append([]string{"test"}, args...)...)
	cmd.Dir = repoRoot(t)

	// Start from a clean slate: an ambient GO_SPECS_* export in the developer's shell must not
	// decide what these tests observe.
	base := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "GO_SPECS_") {
			continue
		}
		base = append(base, kv)
	}
	for k, v := range env {
		base = append(base, k+"="+v)
	}
	cmd.Env = base

	out, err := cmd.CombinedOutput()
	code := 0
	var exitErr *exec.ExitError
	if err != nil {
		if !asExitError(err, &exitErr) {
			t.Fatalf("running go test: %v\n%s", err, out)
		}
		code = exitErr.ExitCode()
	}
	return goTestResult{out: string(out), exitCode: code}
}

func asExitError(err error, target **exec.ExitError) bool {
	e, ok := err.(*exec.ExitError)
	if ok {
		*target = e
	}
	return ok
}

func shardNames(t *testing.T, base string, id RunID) []string {
	t.Helper()
	entries, err := os.ReadDir(shardsDir(base, id))
	if errors.Is(err, fs.ErrNotExist) {
		// A run that was never initialized has no shard directory, which is "no shards published".
		return nil
	}
	if err != nil {
		t.Fatalf("read shard directory: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func runEnv(base string, id RunID, tok RunToken) map[string]string {
	return map[string]string{
		EnvGate:      "1",
		EnvRunID:     string(id),
		EnvRunToken:  string(tok),
		EnvReportDir: base,
	}
}

func TestTwoPackageProcessesPublishIsolatedShardsConcurrently(t *testing.T) {
	// The headline acceptance criterion: two package binaries from one `go test` invocation emit
	// complete, identifiable shards without sharing or corrupting a destination.
	base := secureTempDir(t)
	mustInitRun(t, base, "run-1", validToken)

	res := runGoTest(t, runEnv(base, "run-1", validToken), "-count=1", fixturePkgs)
	if res.exitCode != 0 {
		t.Fatalf("go test failed (%d):\n%s", res.exitCode, res.out)
	}

	names := shardNames(t, base, "run-1")
	if len(names) != 2 {
		t.Fatalf("got %d shards, want one per participating package: %v", len(names), names)
	}
	for _, n := range names {
		if strings.Contains(n, ".tmp-") {
			t.Fatalf("a temp file was left discoverable: %s", n)
		}
	}

	seen := map[string]bool{}
	for _, n := range names {
		env := readEnvelope(t, filepath.Join(shardsDir(base, "run-1"), n))
		if env.RunID != "run-1" {
			t.Fatalf("shard %s reports run %q", n, env.RunID)
		}
		wantName := ShardFileName(env.PackagePath)
		if n != wantName {
			t.Fatalf("shard %s does not match the digest of its own package path (%s)", n, wantName)
		}
		if len(env.Report.Coverage.Packages) != 0 {
			t.Fatalf("shard %s carries coverage", n)
		}
		seen[env.PackagePath] = true
	}
	for _, pkg := range []string{fixtureAlpha, fixtureBeta} {
		if !seen[pkg] {
			t.Fatalf("no shard for %s; shards present: %v", pkg, names)
		}
	}
}

func TestConcurrentIndependentRunsDoNotSeeEachOthersShards(t *testing.T) {
	base := secureTempDir(t)
	mustInitRun(t, base, "run-a", validToken)
	otherToken := RunToken(strings.Repeat("ab", 24))
	mustInitRun(t, base, "run-b", otherToken)

	if res := runGoTest(t, runEnv(base, "run-a", validToken), "-count=1", fixturePkgs); res.exitCode != 0 {
		t.Fatalf("run-a failed:\n%s", res.out)
	}
	if res := runGoTest(t, runEnv(base, "run-b", otherToken), "-count=1", fixturePkgs); res.exitCode != 0 {
		t.Fatalf("run-b failed:\n%s", res.out)
	}

	for _, id := range []RunID{"run-a", "run-b"} {
		if n := len(shardNames(t, base, id)); n != 2 {
			t.Fatalf("run %q sees %d shards, want only its own 2", id, n)
		}
	}
}

func TestAFailingPackageStillPublishesACompleteShard(t *testing.T) {
	// Claim B5. TestMain's post-m.Run() code runs on ordinary failure, including a panic recovered
	// by the testing package, so a red package is fully represented in the merged report
	// (contract v1.2.7 §8).
	base := secureTempDir(t)
	mustInitRun(t, base, "run-1", validToken)

	env := runEnv(base, "run-1", validToken)
	env["GO_SPECS_FIXTURE_FAIL"] = "1"
	env["GO_SPECS_FIXTURE_PANIC"] = "1"

	res := runGoTest(t, env, "-count=1", fixturePkgs)
	if res.exitCode == 0 {
		t.Fatalf("the fixture was supposed to fail:\n%s", res.out)
	}

	names := shardNames(t, base, "run-1")
	if len(names) != 2 {
		t.Fatalf("a red run published %d shards, want 2: %v", len(names), names)
	}
	failuresSeen := 0
	for _, n := range names {
		env := readEnvelope(t, filepath.Join(shardsDir(base, "run-1"), n))
		if env.Report.Execution.Failed > 0 {
			failuresSeen++
		}
	}
	if failuresSeen != 2 {
		t.Fatalf("%d of 2 shards recorded the failure; the test result must survive into the report", failuresSeen)
	}
}

func TestCmdGoDoesNotPropagateATestBinaryExitCode(t *testing.T) {
	// Claim B3, and the entire reason config-error.json exists. On a failed test action cmd/go
	// sets the literal 1, having already reported the package as FAIL; the child's status never
	// reaches base.SetExitStatus. If this ever stopped holding, the simpler exit-code design would
	// be correct and the record would be needless machinery (contract v1.2.7 §8).
	base := secureTempDir(t)
	mustInitRun(t, base, "run-1", validToken)

	env := runEnv(base, "run-1", validToken)
	env["GO_SPECS_FIXTURE_EXIT"] = "78"

	res := runGoTest(t, env, "-count=1", "./report/coordination/internal/shardfixture/alpha")
	if res.exitCode == 78 {
		t.Fatal("cmd/go propagated the test binary's exit code; the contract's premise for config-error.json no longer holds")
	}
	if res.exitCode != 1 {
		t.Fatalf("go test exited %d, want 1:\n%s", res.exitCode, res.out)
	}
	// Stronger than contract v1.2.7 §8 claims. The note there says a TestMain calling os.Exit(2)
	// at least "produces the text `exit status 2` in the output". Measured here, a binary whose
	// tests PASSED and which then exits 78 leaves no trace of 78 anywhere: cmd/go prints PASS,
	// then FAIL for the package, and exits 1. So CI cannot branch on the code even by scraping
	// the log — which closes the last escape hatch and is why the distinction has to live in a
	// file on disk.
	if strings.Contains(res.out, "78") {
		t.Fatalf("the child's exit code is visible in the output after all, so this test no longer pins what it claims:\n%s", res.out)
	}
}

func TestACachedPackagePublishesNothingWhichIsWhyCountOneIsMandatory(t *testing.T) {
	// Claims B2 and B4, and the finding that corrects contract v1.2.7 §5.
	//
	// §5 states that reading run identity through the environment makes a new GO_SPECS_RUN_ID
	// invalidate that package's cached result, and calls -count=1 a supporting requirement rather
	// than the mechanism. Measured here: the read happens in TestMain before m.Run(), where
	// testlog is not yet active, so it contributes NOTHING to the cache key. -count=1 is not a
	// supplement — it is the only thing standing between a cached package and a silently missing
	// shard.
	base := secureTempDir(t)
	mustInitRun(t, base, "run-1", validToken)
	env := runEnv(base, "run-1", validToken)

	if res := runGoTest(t, env, "-count=1", "./report/coordination/internal/shardfixture/alpha"); res.exitCode != 0 {
		t.Fatalf("seed run failed:\n%s", res.out)
	}
	// Warm the cache with the identical environment, then clear the shard so a second publish
	// would be visible.
	if res := runGoTest(t, env, "./report/coordination/internal/shardfixture/alpha"); res.exitCode != 0 {
		t.Fatalf("cache-warming run failed:\n%s", res.out)
	}
	for _, n := range shardNames(t, base, "run-1") {
		if err := os.Remove(filepath.Join(shardsDir(base, "run-1"), n)); err != nil {
			t.Fatal(err)
		}
	}

	cachedRun := runGoTest(t, env, "./report/coordination/internal/shardfixture/alpha")
	if !cachedRun.cached() {
		t.Skipf("the run was not served from cache, so this test cannot observe the condition:\n%s", cachedRun.out)
	}
	if names := shardNames(t, base, "run-1"); len(names) != 0 {
		t.Fatalf("a cached package published %v; TestMain cannot have run", names)
	}
}

func TestACachedPackageHidesAConfigurationErrorEntirely(t *testing.T) {
	// The sharpest operational consequence, and the one worth a test of its own: with the gate on
	// and an invalid configuration, contract v1.2.7 §3 step 3 requires the binary to fail loudly
	// before m.Run(). On a cached package it does not run at all, so `go test` reports ok (cached)
	// and exits 0 — a misconfigured reporting run that looks exactly like a healthy green one.
	//
	// The gate variable itself cannot rescue this: it is read in TestMain too, so switching it on
	// does not invalidate the cached result either.
	warm := runGoTest(t, map[string]string{}, "./report/coordination/internal/shardfixture/beta")
	if warm.exitCode != 0 {
		t.Fatalf("warm-up run failed:\n%s", warm.out)
	}

	broken := runGoTest(t, map[string]string{EnvGate: "1"}, "./report/coordination/internal/shardfixture/beta")
	if !broken.cached() {
		t.Skipf("the run was not served from cache, so this test cannot observe the condition:\n%s", broken.out)
	}
	if broken.exitCode != 0 {
		t.Fatalf("expected the cached run to pass silently, got exit %d:\n%s", broken.exitCode, broken.out)
	}

	// With -count=1 the same misconfiguration fails loudly, naming the variable and the remedy.
	strict := runGoTest(t, map[string]string{EnvGate: "1"}, "-count=1", "./report/coordination/internal/shardfixture/beta")
	if strict.exitCode == 0 {
		t.Fatalf("the misconfiguration passed even with -count=1:\n%s", strict.out)
	}
	if !strings.Contains(strict.out, EnvRunID) {
		t.Fatalf("the diagnostic does not name the missing variable:\n%s", strict.out)
	}
}

func TestAMisconfiguredRunWithNoOwnerRecordsConfigErrorJSON(t *testing.T) {
	// The useful case: the preflight was skipped, so no marker exists and no legitimate run can be
	// harmed. The record is what lets finalize say "misconfigured" rather than merely "incomplete"
	// — a distinction the test binary's own exit status cannot carry (contract v1.2.7 §8).
	base := secureTempDir(t)
	env := runEnv(base, "never-initialized", validToken)

	res := runGoTest(t, env, "-count=1", "./report/coordination/internal/shardfixture/alpha")
	if res.exitCode == 0 {
		t.Fatalf("a run with no marker was accepted:\n%s", res.out)
	}

	rec := readConfigError(t, base, "never-initialized")
	if rec.Reason != ReasonMarkerMissing {
		t.Fatalf("Reason = %q, want %q", rec.Reason, ReasonMarkerMissing)
	}
	if rec.PackagePath != fixtureAlpha {
		t.Fatalf("PackagePath = %q", rec.PackagePath)
	}
	if strings.Contains(rec.Diagnostic, validToken) {
		t.Fatal("the record echoes the token")
	}
	if names := shardNames(t, base, "never-initialized"); len(names) != 0 {
		t.Fatalf("a process that failed its checks still published %v", names)
	}
}

func TestAForeignInvocationNeverPoisonsAHealthyRunsDirectory(t *testing.T) {
	// A second invocation reuses a run id that a healthy run already owns. It must fail loudly for
	// itself and leave the owner's directory completely untouched: config-error.json is
	// first-writer-wins and the finalizer maps its presence to exit 78, so a record dropped here
	// would fail a run that was fine.
	base := secureTempDir(t)
	mustInitRun(t, base, "run-1", validToken)

	for name, token := range map[string]RunToken{
		"foreign token": RunToken(strings.Repeat("cd", 20)),
		"no token":      "",
	} {
		t.Run(name, func(t *testing.T) {
			env := runEnv(base, "run-1", token)
			if token == "" {
				delete(env, EnvRunToken)
			}

			res := runGoTest(t, env, "-count=1", "./report/coordination/internal/shardfixture/alpha")
			if res.exitCode == 0 {
				t.Fatalf("an invocation that does not own run-1 was accepted:\n%s", res.out)
			}

			if _, err := os.Stat(configErrorPath(base, "run-1")); !errors.Is(err, fs.ErrNotExist) {
				t.Fatal("a process that could not prove ownership wrote config-error.json into the owner's run directory")
			}
			if names := shardNames(t, base, "run-1"); len(names) != 0 {
				t.Fatalf("it also published %v", names)
			}
		})
	}

	// The legitimate owner is still able to run afterwards, which is the property all of this
	// protects.
	if res := runGoTest(t, runEnv(base, "run-1", validToken), "-count=1", "./report/coordination/internal/shardfixture/alpha"); res.exitCode != 0 {
		t.Fatalf("the healthy run was broken by the intruders:\n%s", res.out)
	}
	if names := shardNames(t, base, "run-1"); len(names) != 1 {
		t.Fatalf("the owner published %d shards, want 1", len(names))
	}
}

const fixtureProbe = "./report/coordination/internal/shardfixture/envprobe"

func probeRun(t *testing.T, mode, value string) goTestResult {
	t.Helper()
	// Deliberately no -count=1: it bypasses the cache, so a seeding run with it seeds nothing.
	return runGoTest(t, map[string]string{
		"GO_SPECS_PROBE_MODE":  mode,
		"GO_SPECS_PROBE_VALUE": value,
	}, fixtureProbe)
}

// probeNonce makes every execution of this test start cold.
//
// Without it the test is order-dependent and reports a false failure on its second run: the
// control arm's second value is still in the cache from the previous execution against the same
// binary, so it comes back (cached) and the test concludes it cannot distinguish the two access
// paths. A fresh nonce guarantees no entry exists for either arm.
func probeNonce(t *testing.T) string {
	t.Helper()
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("generating a probe nonce: %v", err)
	}
	return hex.EncodeToString(b[:])
}

func TestTheEnvironScanIsActuallyAnEnvironScan(t *testing.T) {
	// This pins the PRODUCTION implementation of envread.Scan, which nothing else does.
	//
	// The unit tests elsewhere use a fake environment to assert that the resolver calls Scan
	// rather than Lookup on the disabled path. That is worth having, but it pins the caller's
	// choice of method — a name — and not what the method does underneath. Rewriting
	// envread.Scan's body as os.LookupEnv keeps every one of those tests green while
	// reintroducing the cache-enrollment regression the rule exists to prevent. This test is what
	// fails in that case.
	//
	// It only works from inside a test function: in TestMain the test log is not yet installed and
	// the two access paths are indistinguishable.
	//
	// This is NOT the cache-stability test contract v1.2.7 §5 withdraws and forbids reintroducing.
	// That one reads from TestMain, where neither access path reaches the cache key, so it cannot
	// fail. This one reads from a position where they differ, and it does fail when envread.Scan
	// is rewritten as os.LookupEnv. §5's mandated seam assertion is also present and is the pin of
	// record; this covers what the seam cannot see, which is the body of the method itself.
	nonce := probeNonce(t)

	if seed := probeRun(t, "scan", nonce+"-one"); seed.exitCode != 0 {
		t.Fatalf("scan seeding run failed:\n%s", seed.out)
	}
	scanned := probeRun(t, "scan", nonce+"-two")
	if scanned.exitCode != 0 {
		t.Fatalf("scan second run failed:\n%s", scanned.out)
	}

	// The control arm runs first-class rather than as a comment: without it, a change that made
	// BOTH paths uncacheable would leave the assertion above passing for the wrong reason, and a
	// change that made both cacheable would make it vacuous. The control decides which.
	if seed := probeRun(t, "lookup", nonce+"-one"); seed.exitCode != 0 {
		t.Fatalf("lookup seeding run failed:\n%s", seed.out)
	}
	lookedUp := probeRun(t, "lookup", nonce+"-two")
	if lookedUp.exitCode != 0 {
		t.Fatalf("lookup second run failed:\n%s", lookedUp.out)
	}

	if lookedUp.cached() {
		t.Fatalf("the control arm was cached too, so this test cannot tell the two access paths apart and proves nothing:\n%s", lookedUp.out)
	}
	if !scanned.cached() {
		t.Fatalf("envread.Scan enrolled %s in the test-cache key, so it is no longer an os.Environ scan; the disabled path now defeats caching for every package in the module:\n%s", "GO_SPECS_PROBE_VALUE", scanned.out)
	}
}

func TestAReportingFailureNeverFalsifiesAPassingTestResult(t *testing.T) {
	// Contract v1.2.7 §8, row 8: a write, publish or duplicate-producer failure after a valid,
	// ownership-verified m.Run() leaves the original test result "preserved, unchanged", and
	// reporting failure "never overwrites or falsifies the test result that already happened".
	//
	// The trigger is not exotic: re-running one package inside the same run id publishes a second
	// time and hits duplicate-producer. A TestMain that exits 1 on a Write error turns a green
	// package red for a reporting problem — which is the single outcome this whole design exists
	// to prevent.
	base := secureTempDir(t)
	mustInitRun(t, base, "run-1", validToken)
	env := runEnv(base, "run-1", validToken)
	pkg := "./report/coordination/internal/shardfixture/alpha"

	if first := runGoTest(t, env, "-count=1", pkg); first.exitCode != 0 {
		t.Fatalf("the first run failed:\n%s", first.out)
	}

	second := runGoTest(t, env, "-count=1", pkg)
	if second.exitCode != 0 {
		t.Fatalf("a duplicate publish turned a passing package red (exit %d):\n%s", second.exitCode, second.out)
	}
	if strings.Contains(second.out, "FAIL") {
		t.Fatalf("go test reported FAIL for a package whose tests passed:\n%s", second.out)
	}

	// The failure is reported, not swallowed — but cmd/go only shows a passing package's output
	// under -v, so plain `go test` hides it. That is the accepted cost of not falsifying the
	// result: the authoritative signal is the finalizer, which sees the producer as missing or
	// rejected and fails through its own independent exit status.
	verbose := runGoTest(t, env, "-count=1", "-v", pkg)
	if !strings.Contains(verbose.out, "already published") {
		t.Fatalf("the reporting failure was swallowed entirely, even under -v:\n%s", verbose.out)
	}
}

func TestProducersRejectARelativeReportingDirectory(t *testing.T) {
	// `go test` gives every package binary its own working directory — its package source
	// directory — so a relative GO_SPECS_REPORT_DIR resolves somewhere different in each package
	// of one invocation. The preflight would create the marker in one place and no producer would
	// find it. Rejecting it names the cause; not rejecting it surfaces as marker-missing, or as a
	// permission refusal naming a package directory nobody configured.
	base := secureTempDir(t)
	own := mustInitRun(t, base, "run-1", validToken)
	if !filepath.IsAbs(own.BaseDir) {
		t.Fatalf("InitializeRun reported a relative BaseDir %q; the invoker has nothing absolute to export", own.BaseDir)
	}

	res := runGoTest(t, map[string]string{
		EnvGate:      "1",
		EnvRunID:     "run-1",
		EnvRunToken:  validToken,
		EnvReportDir: ".go-specs/runs",
	}, "-count=1", "./report/coordination/internal/shardfixture/alpha")

	if res.exitCode == 0 {
		t.Fatalf("a relative reporting directory was accepted:\n%s", res.out)
	}
	if !strings.Contains(res.out, "absolute") || !strings.Contains(res.out, EnvReportDir) {
		t.Fatalf("the diagnostic does not explain the cause:\n%s", res.out)
	}
}
