package coordination

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func mustInitRun(t *testing.T, base string, id RunID, tok RunToken) RunOwnership {
	t.Helper()
	own, err := InitializeRun(context.Background(), InitializeRunOptions{
		RunID: id, Token: tok, BaseDir: base,
	})
	if err != nil {
		t.Fatalf("InitializeRun: %v", err)
	}
	return own
}

func modeOf(t *testing.T, path string) fs.FileMode {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("lstat %s: %v", path, err)
	}
	return info.Mode().Perm()
}

func TestInitializeRunCreatesTheMarkerAndDirectoriesWithLeastPrivilege(t *testing.T) {
	base := secureTempDir(t)
	own := mustInitRun(t, base, "run-1", validToken)

	runDir := filepath.Join(base, "run-1")
	if got := modeOf(t, runDir); got != 0o700 {
		t.Fatalf("run directory mode = %04o, want 0700", got)
	}
	if got := modeOf(t, filepath.Join(runDir, "shards")); got != 0o700 {
		t.Fatalf("shard directory mode = %04o, want 0700", got)
	}
	if got := modeOf(t, own.MarkerPath); got != 0o600 {
		t.Fatalf("run.json mode = %04o, want 0600", got)
	}
	if want := filepath.Join(runDir, "run.json"); own.MarkerPath != want {
		t.Fatalf("MarkerPath = %q, want %q", own.MarkerPath, want)
	}
}

func TestRunMarkerStoresTheTokenDigestAndNeverTheToken(t *testing.T) {
	base := secureTempDir(t)
	own := mustInitRun(t, base, "run-1", validToken)

	raw, err := os.ReadFile(own.MarkerPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), validToken) {
		t.Fatalf("run.json contains the raw token:\n%s", raw)
	}

	var m runMarker
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("run.json is not valid JSON: %v", err)
	}
	wantHash, err := HashRunToken(validToken)
	if err != nil {
		t.Fatal(err)
	}
	if m.RunTokenHash != wantHash {
		t.Fatalf("RunTokenHash = %q, want %q", m.RunTokenHash, wantHash)
	}
	if m.RunID != "run-1" {
		t.Fatalf("RunID = %q", m.RunID)
	}
	if m.SchemaVersion == "" {
		t.Fatal("run.json carries no schema version; a reader cannot tell what it is trusting")
	}
	if own.TokenHash != wantHash {
		t.Fatalf("RunOwnership.TokenHash = %q, want %q", own.TokenHash, wantHash)
	}
}

func TestInitializeRunRefusesAnExistingMarker(t *testing.T) {
	base := secureTempDir(t)
	mustInitRun(t, base, "run-1", validToken)

	_, err := InitializeRun(context.Background(), InitializeRunOptions{
		RunID: "run-1", Token: RunToken(strings.Repeat("ff", 16)), BaseDir: base,
	})
	if err == nil {
		t.Fatal("a second InitializeRun on the same RunID succeeded; exclusive creation is what makes RunID reuse detectable")
	}
	// The failure must be actionable: it means either the RunID generator is not unique per
	// invocation, or a previous run was abandoned (contract v1.2.7 §5).
	msg := err.Error()
	for _, want := range []string{"run-1", "gc", "--force"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q does not mention %q, so the operator cannot act on it", msg, want)
		}
	}
}

func TestInitializeRunIsExclusiveUnderConcurrency(t *testing.T) {
	// Claim A4. Exclusive creation is the whole collision-detection mechanism; if two independent
	// invocations can both believe they own the run, every later ownership check is theatre.
	base := secureTempDir(t)
	const racers = 16

	var wg sync.WaitGroup
	results := make([]error, racers)
	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tok := RunToken(strings.Repeat("0", 31) + string(rune('0'+i%10)))
			<-start
			_, results[i] = InitializeRun(context.Background(), InitializeRunOptions{
				RunID: "contended", Token: tok, BaseDir: base,
			})
		}(i)
	}
	close(start)
	wg.Wait()

	winners := 0
	for _, err := range results {
		if err == nil {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("%d of %d concurrent InitializeRun calls succeeded, want exactly 1", winners, racers)
	}
}

func TestVerifyRunOwnershipAcceptsTheGenuineToken(t *testing.T) {
	base := secureTempDir(t)
	mustInitRun(t, base, "run-1", validToken)

	own, err := VerifyRunOwnership(base, "run-1", validToken)
	if err != nil {
		t.Fatalf("VerifyRunOwnership: %v", err)
	}
	wantHash, _ := HashRunToken(validToken)
	if own.TokenHash != wantHash {
		t.Fatalf("TokenHash = %q, want %q", own.TokenHash, wantHash)
	}
}

func TestVerifyRunOwnershipAcceptsACaseFoldedToken(t *testing.T) {
	// Claim D1 at the verification boundary: the marker was written from the lowercase form and a
	// layer between the generator and this process upper-cased the value. The two decode to
	// identical bytes, so verification must succeed.
	base := secureTempDir(t)
	mustInitRun(t, base, "run-1", validToken)

	if _, err := VerifyRunOwnership(base, "run-1", RunToken(validTokenUp)); err != nil {
		t.Fatalf("a case-folded token was rejected: %v", err)
	}
}

func TestVerifyRunOwnershipFailsClosedOnEveryUnprovableState(t *testing.T) {
	wantReason := func(t *testing.T, err error, want ConfigErrorReason) {
		t.Helper()
		var cfgErr *ConfigError
		if !errors.As(err, &cfgErr) {
			t.Fatalf("got %v, want a *ConfigError", err)
		}
		if cfgErr.Reason != want {
			t.Fatalf("Reason = %q, want %q", cfgErr.Reason, want)
		}
	}

	t.Run("marker absent while the gate is on", func(t *testing.T) {
		base := secureTempDir(t)
		_, err := VerifyRunOwnership(base, "never-initialized", validToken)
		if err == nil {
			t.Fatal("verification succeeded with no marker at all")
		}
		wantReason(t, err, ReasonMarkerMissing)
	})

	t.Run("different token is a RunID collision", func(t *testing.T) {
		// The only mechanism in the contract that tells "another producer of the same run" apart
		// from "an unrelated invocation that reused the same RunID": both present identical
		// RunIDs, and only the genuine one holds the matching token.
		base := secureTempDir(t)
		mustInitRun(t, base, "run-1", validToken)

		_, err := VerifyRunOwnership(base, "run-1", RunToken(strings.Repeat("ff", 16)))
		if err == nil {
			t.Fatal("verification succeeded for a foreign token; a RunID collision would go undetected")
		}
		wantReason(t, err, ReasonMarkerMismatch)
		if strings.Contains(err.Error(), strings.Repeat("ff", 16)) {
			t.Fatalf("diagnostic %q echoes the token", err)
		}
	})

	t.Run("corrupt marker", func(t *testing.T) {
		base := secureTempDir(t)
		own := mustInitRun(t, base, "run-1", validToken)
		if err := os.WriteFile(own.MarkerPath, []byte("{not json"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := VerifyRunOwnership(base, "run-1", validToken)
		if err == nil {
			t.Fatal("verification succeeded against an unparseable marker")
		}
		wantReason(t, err, ReasonMarkerUnreadable)
	})

	t.Run("marker with an empty digest", func(t *testing.T) {
		// An absent digest must never compare equal to anything. This is the shape a partially
		// written or hand-edited marker takes.
		base := secureTempDir(t)
		own := mustInitRun(t, base, "run-1", validToken)
		if err := os.WriteFile(own.MarkerPath, []byte(`{"schemaVersion":"1","runId":"run-1","runTokenHash":""}`), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := VerifyRunOwnership(base, "run-1", validToken)
		if err == nil {
			t.Fatal("an empty stored digest verified successfully")
		}
	})

	t.Run("marker recorded for a different run", func(t *testing.T) {
		base := secureTempDir(t)
		own := mustInitRun(t, base, "run-1", validToken)
		hash, _ := HashRunToken(validToken)
		body := `{"schemaVersion":"1","runId":"someone-else","runTokenHash":"` + hash + `"}`
		if err := os.WriteFile(own.MarkerPath, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := VerifyRunOwnership(base, "run-1", validToken)
		if err == nil {
			t.Fatal("a marker naming a different run verified successfully")
		}
	})

	t.Run("unknown schema version", func(t *testing.T) {
		base := secureTempDir(t)
		own := mustInitRun(t, base, "run-1", validToken)
		hash, _ := HashRunToken(validToken)
		body := `{"schemaVersion":"99","runId":"run-1","runTokenHash":"` + hash + `"}`
		if err := os.WriteFile(own.MarkerPath, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := VerifyRunOwnership(base, "run-1", validToken)
		if err == nil {
			t.Fatal("a marker with an unknown schema version was trusted")
		}
	})
}

func TestVerifyRunOwnershipAdmitsEverySecondProducerOfTheSameRun(t *testing.T) {
	// The normal case, and the one a fail-closed design must not break: many package binaries in
	// one invocation all verify against the same marker, concurrently.
	base := secureTempDir(t)
	mustInitRun(t, base, "run-1", validToken)

	var wg sync.WaitGroup
	errs := make([]error, 32)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = VerifyRunOwnership(base, "run-1", validToken)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("producer %d was rejected from its own run: %v", i, err)
		}
	}
}

func TestForceRecreatesTheMarkerAndClearsTheShardDirectory(t *testing.T) {
	base := secureTempDir(t)
	own := mustInitRun(t, base, "run-1", validToken)
	stale := filepath.Join(filepath.Dir(own.MarkerPath), "shards", "stale--x.shard.json")
	if err := os.WriteFile(stale, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	newTok := RunToken(strings.Repeat("ff", 16))
	got, err := InitializeRun(context.Background(), InitializeRunOptions{
		RunID: "run-1", Token: newTok, BaseDir: base, Force: true,
	})
	if err != nil {
		t.Fatalf("forced InitializeRun: %v", err)
	}
	wantHash, _ := HashRunToken(newTok)
	if got.TokenHash != wantHash {
		t.Fatal("force did not rebind the marker to the new token")
	}
	if _, err := os.Stat(stale); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("force left a shard from the previous run behind")
	}
	if _, err := VerifyRunOwnership(base, "run-1", validToken); err == nil {
		t.Fatal("the previous token still verifies after a forced re-initialization")
	}
}

func TestInitializeRunRejectsInvalidIdentityBeforeTouchingTheFilesystem(t *testing.T) {
	base := secureTempDir(t)
	for name, opts := range map[string]InitializeRunOptions{
		"traversal run id": {RunID: "../escape", Token: validToken, BaseDir: base},
		"empty run id":     {RunID: "", Token: validToken, BaseDir: base},
		"short token":      {RunID: "run-1", Token: "dead", BaseDir: base},
		"non hex token":    {RunID: "run-1", Token: RunToken(strings.Repeat("zz", 16)), BaseDir: base},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := InitializeRun(context.Background(), opts); err == nil {
				t.Fatal("InitializeRun accepted invalid identity")
			}
			entries, err := os.ReadDir(base)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatalf("InitializeRun created %v before validating its inputs", entries)
			}
		})
	}
}
