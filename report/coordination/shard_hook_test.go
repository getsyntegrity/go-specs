package coordination

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getsyntegrity/go-specs/report"
)

// shard_hook_test.go pins issue #207 H10 at the shard level: report.Case.Hook must never appear
// as an empty key in a published shard, and a hook case's Hook value must survive the shard round
// trip unchanged.

// TestShardWithNoHookCasesEmitsNoHookKey proves that a shard built from a report containing only
// ordinary cases never serializes an empty "Hook" key. report.Case has no JSON tags, so
// writer.go's json.MarshalIndent over the embedded NormalizedReport picks up the plain Go field
// name "Hook" for every case unless the field is tagged omitempty — which would silently add
// `"Hook": ""` to every ordinary case in every shard, violating H10 ("report output unchanged for
// suites without group hooks") even though no suite here ever registers a BeforeAll/AfterAll.
func TestShardWithNoHookCasesEmitsNoHookKey(t *testing.T) {
	base := secureTempDir(t)
	cfg := enabledConfig(t, base, "example.com/m/calc")

	if err := NewShardWriter(cfg).Write(sampleReport()); err != nil {
		t.Fatalf("Write: %v", err)
	}

	path := filepath.Join(shardsDir(base, "run-1"), ShardFileName("example.com/m/calc"))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if strings.Contains(string(raw), `"Hook"`) {
		t.Fatalf("shard for a report with no hook cases carries a \"Hook\" key:\n%s", raw)
	}
}

// TestShardPreservesHookCaseThroughRoundTrip proves a shard carrying a synthetic group hook case
// survives the package's own shard read path (json.Unmarshal into ShardEnvelope, exactly as
// readEnvelope in writer_test.go does) with Case.Hook preserved.
func TestShardPreservesHookCaseThroughRoundTrip(t *testing.T) {
	base := secureTempDir(t)
	cfg := enabledConfig(t, base, "example.com/m/calc")

	rep := report.NormalizedReport{
		SchemaVersion: report.SchemaVersion,
		Execution:     report.Totals{Total: 1, Failed: 1},
		Suites: []report.Suite{{
			Name: "Checkout",
			Cases: []report.Case{{
				Name:   "[BeforeAll]",
				Path:   []string{"Checkout", "when cart has items"},
				Status: report.StatusFailed,
				Hook:   "BeforeAll",
			}},
		}},
	}
	if err := NewShardWriter(cfg).Write(rep); err != nil {
		t.Fatalf("Write: %v", err)
	}

	path := filepath.Join(shardsDir(base, "run-1"), ShardFileName("example.com/m/calc"))
	env := readEnvelope(t, path)
	if len(env.Report.Suites) != 1 || len(env.Report.Suites[0].Cases) != 1 {
		t.Fatalf("shard did not round-trip the suite/case: %+v", env.Report)
	}
	if got := env.Report.Suites[0].Cases[0].Hook; got != "BeforeAll" {
		t.Fatalf("Case.Hook after round trip = %q, want %q", got, "BeforeAll")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"Hook": "BeforeAll"`) {
		t.Fatalf("expected the shard to serialize the hook case's Hook value, got:\n%s", raw)
	}
}
