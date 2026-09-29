package report

import (
	"bytes"
	"encoding/json"
	"sort"
	"testing"
)

// TestSchemaV3ContractPinsVocabularyAndOptionalFields pins what "schema version 3" promises a JSON
// consumer (issue #325), so a change that would need a version bump, or would silently change what
// a value means without one, fails here first.
//
// The version is a promise about shape: the closed status vocabulary and the set of always-present
// case keys. It is not a promise that a given status or message means what it meant in an earlier
// release. v0.3.0 kept version "3" while changing several meanings — a built-in assertion failure
// now carries its message (#272), a Builder.ItParallel panic is "error" rather than "failed"
// (#314), and cases arrive in declaration order (#315). docs/REPORTING.md "Consumer migration"
// lists them; this test does not pretend the version covers them.
func TestSchemaV3ContractPinsVocabularyAndOptionalFields(t *testing.T) {
	if SchemaVersion != "3" {
		t.Fatalf("SchemaVersion = %q: a change here must update this test and docs/REPORTING.md together", SchemaVersion)
	}

	statuses := []Status{StatusPassed, StatusFailed, StatusError, StatusSkipped, StatusFiltered, StatusPending, StatusUnstarted}
	var cases []Case
	for _, s := range statuses {
		cases = append(cases, Case{Name: string(s), Path: []string{"suite", string(s)}, Status: s})
	}
	var buf bytes.Buffer
	rep := NormalizedReport{SchemaVersion: SchemaVersion, Suites: []Suite{{Name: "suite", Cases: cases}}}
	if err := RenderJSON(&buf, rep); err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}

	var doc struct {
		Suites []struct {
			Cases []map[string]json.RawMessage `json:"cases"`
		} `json:"suites"`
	}
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got := doc.Suites[0].Cases
	if len(got) != len(statuses) {
		t.Fatalf("got %d cases, want %d", len(got), len(statuses))
	}

	// Always present on every case: a consumer may rely on these keys.
	required := []string{"durationMs", "name", "path", "status"}
	for i, c := range got {
		var keys []string
		for k := range c {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if len(keys) != len(required) {
			t.Errorf("case %s carries keys %v, want exactly %v when every optional field is empty", statuses[i], keys, required)
			continue
		}
		for j, k := range required {
			if keys[j] != k {
				t.Errorf("case %s carries keys %v, want exactly %v", statuses[i], keys, required)
				break
			}
		}
		var status string
		if err := json.Unmarshal(c["status"], &status); err != nil || status != string(statuses[i]) {
			t.Errorf("case %d status = %s, want %q", i, c["status"], statuses[i])
		}
	}
}

// TestSchemaV3OptionalFieldsAppearOnlyWhenSet pins that message, output, hook, declared and the
// suite-level package are additive: absent when empty, present when set. A consumer must therefore
// treat a failed case without "message" as valid — a spec that failed only through ctx.T directly
// has no structured text to carry.
func TestSchemaV3OptionalFieldsAppearOnlyWhenSet(t *testing.T) {
	rep := NormalizedReport{SchemaVersion: SchemaVersion, Suites: []Suite{{
		Name: "suite", Package: "example.com/m/pkg",
		Cases: []Case{
			{Name: "bare", Path: []string{"suite", "bare"}, Status: StatusFailed},
			{Name: "full", Path: []string{"suite", "full"}, Status: StatusError, Message: "panic: x", Output: "goroutine 1", Hook: "BeforeAll", Declared: "skip"},
		},
	}}}
	var buf bytes.Buffer
	if err := RenderJSON(&buf, rep); err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}
	var doc struct {
		Suites []struct {
			Package string                       `json:"package"`
			Cases   []map[string]json.RawMessage `json:"cases"`
		} `json:"suites"`
	}
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if doc.Suites[0].Package != "example.com/m/pkg" {
		t.Errorf("suite package = %q", doc.Suites[0].Package)
	}
	optional := []string{"message", "output", "hook", "declared"}
	for _, k := range optional {
		if _, ok := doc.Suites[0].Cases[0][k]; ok {
			t.Errorf("bare failed case carries optional key %q, want it omitted", k)
		}
		if _, ok := doc.Suites[0].Cases[1][k]; !ok {
			t.Errorf("full case lacks optional key %q, want it present", k)
		}
	}

	var single bytes.Buffer
	if err := RenderJSON(&single, NormalizedReport{SchemaVersion: SchemaVersion, Suites: []Suite{{Name: "s"}}}); err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}
	var top struct {
		Suites []map[string]json.RawMessage `json:"suites"`
	}
	if err := json.Unmarshal(single.Bytes(), &top); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := top.Suites[0]["package"]; ok {
		t.Error("a single-package suite carries a package key, want it omitted")
	}
}
