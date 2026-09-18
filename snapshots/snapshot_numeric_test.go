package snapshots

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// seedSnapshot writes a snapshot file holding one key with the exact raw JSON given, and returns
// the caller file path Evaluate should be driven with. Raw bytes are the point: these specs are
// about how a stored literal compares, so the stored text must not pass through a Go value first.
func seedSnapshot(t *testing.T, name string, raw string) string {
	t.Helper()
	dir := t.TempDir()
	callerFile := filepath.Join(dir, "numeric_test.go")
	snapshotDir := filepath.Join(dir, "__snapshots__")
	if err := os.MkdirAll(snapshotDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := filepath.Join(snapshotDir, "numeric_test.snap.json")
	if err := Save(path, map[string]json.RawMessage{name: json.RawMessage(raw)}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	return callerFile
}

// TestEvaluate_AdjacentIntegersAbove2Pow53AreDistinct verifies #154: decoding into `any` lands JSON
// numbers in float64, where 9007199254740992 and 9007199254740993 collapse to the same value and a
// changed ID reports a false-positive match.
func TestEvaluate_AdjacentIntegersAbove2Pow53AreDistinct(t *testing.T) {
	callerFile := seedSnapshot(t, "id", `9007199254740992`)

	result := Evaluate(nil, callerFile, "id", uint64(9007199254740993))
	if result.Passed {
		t.Fatal("expected adjacent integers above 2^53 to compare as different snapshots")
	}
}

// TestEvaluate_LargeIntegerPrecisionSurvivesNesting verifies the exact-number comparison reaches
// values nested inside objects and arrays, not just a bare top-level number.
func TestEvaluate_LargeIntegerPrecisionSurvivesNesting(t *testing.T) {
	callerFile := seedSnapshot(t, "nested", `{"ids":[1,9007199254740992]}`)

	value := map[string]any{"ids": []any{1, uint64(9007199254740993)}}
	result := Evaluate(nil, callerFile, "nested", value)
	if result.Passed {
		t.Fatal("expected a nested integer difference above 2^53 to be reported as a mismatch")
	}
}

// TestEvaluate_LexicallyDifferentNumbersOfEqualValueMatch pins the documented semantics: numbers
// compare by exact numeric value, not by literal text, so a snapshot written as 1.0 still matches a
// value marshaled as 1. Only a difference in value makes a snapshot differ.
func TestEvaluate_LexicallyDifferentNumbersOfEqualValueMatch(t *testing.T) {
	for _, stored := range []string{`1`, `1.0`, `1e0`, `0.1e1`, `100e-2`} {
		callerFile := seedSnapshot(t, "one", stored)

		result := Evaluate(nil, callerFile, "one", 1)
		if !result.Passed {
			t.Errorf("stored %s: expected an equal numeric value to match, got %s", stored, result.Message)
		}
	}
}

// TestEvaluate_NumbersDifferingInValueMismatch is the other half of the value-not-text rule: equal
// text is not required, but equal value is.
func TestEvaluate_NumbersDifferingInValueMismatch(t *testing.T) {
	callerFile := seedSnapshot(t, "one", `1.5`)

	result := Evaluate(nil, callerFile, "one", 1)
	if result.Passed {
		t.Fatal("expected numbers of different value to compare as different snapshots")
	}
}

// TestEvaluate_NumbersAndStringsNeverCompareEqual guards the canonical form itself: turning a
// number into a comparable token must not make the number 1 equal to the string "1".
func TestEvaluate_NumbersAndStringsNeverCompareEqual(t *testing.T) {
	callerFile := seedSnapshot(t, "one", `"1"`)

	result := Evaluate(nil, callerFile, "one", 1)
	if result.Passed {
		t.Fatal("expected a JSON string and a JSON number never to compare equal")
	}
}

// TestEvaluate_FormattingDifferencesStillMatch keeps the original reason the comparison decodes at
// all: key order and whitespace are not part of a snapshot's identity.
func TestEvaluate_FormattingDifferencesStillMatch(t *testing.T) {
	callerFile := seedSnapshot(t, "obj", `{"b":2,   "a":1}`)

	value := map[string]any{"a": 1, "b": 2}
	result := Evaluate(nil, callerFile, "obj", value)
	if !result.Passed {
		t.Fatalf("expected key order and whitespace to be irrelevant, got %s", result.Message)
	}
}
