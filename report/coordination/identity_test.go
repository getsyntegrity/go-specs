package coordination

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func TestValidateRunIDAcceptsConformingValues(t *testing.T) {
	for _, in := range []string{
		"a",
		"1",
		"ci-run.42_x",
		strings.Repeat("a", 128),
		"17293847-1-test-0",
	} {
		t.Run(in, func(t *testing.T) {
			got, err := ValidateRunID(in)
			if err != nil {
				t.Fatalf("ValidateRunID(%q): %v", in, err)
			}
			if string(got) != in {
				t.Fatalf("ValidateRunID(%q) = %q, want the value unchanged", in, got)
			}
		})
	}
}

func TestValidateRunIDRejectsPathAndTraversalValues(t *testing.T) {
	// Every one of these is a path-traversal or path-separator vector: contract v1.2.6 §10
	// requires them rejected before the value is ever used as a path segment.
	for name, in := range map[string]string{
		"empty":         "",
		"dot dot":       "..",
		"single dot":    ".",
		"slash":         "a/b",
		"backslash":     `a\b`,
		"leading slash": "/abs",
		"traversal":     "../../etc",
		"nul byte":      "a\x00b",
		"newline":       "a\nb",
		"space":         "a b",
		"tilde":         "~",
		"too long":      strings.Repeat("a", 129),
		"colon":         "a:b",
		"percent":       "a%2Fb",
		"non ascii":     "café",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ValidateRunID(in); err == nil {
				t.Fatalf("ValidateRunID(%q) = nil error, want a configuration error", in)
			}
		})
	}
}

func TestValidateRunIDErrorNamesTheVariableAndTheRemedy(t *testing.T) {
	_, err := ValidateRunID("../escape")

	var cfgErr *ConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("ValidateRunID error is %T, want *ConfigError", err)
	}
	if cfgErr.Source != EnvRunID {
		t.Fatalf("Source = %q, want %q", cfgErr.Source, EnvRunID)
	}
	if cfgErr.Reason != ReasonInvalidRunID {
		t.Fatalf("Reason = %q, want %q", cfgErr.Reason, ReasonInvalidRunID)
	}
	// Contract v1.2.6 §5: "Invalid reporting configuration" without a variable name is not an
	// acceptable diagnostic — the person hitting this usually does not know the variable is set.
	msg := cfgErr.Error()
	for _, want := range []string{EnvRunID, "unset", `^[A-Za-z0-9_.-]{1,128}$`} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q does not mention %q", msg, want)
		}
	}
}

func TestValidateRunTokenAcceptsOnlyWholeBytePairsInRange(t *testing.T) {
	lower := strings.Repeat("ab", 16)
	upper := strings.ToUpper(lower)

	for name, tc := range map[string]struct {
		in   string
		want bool
	}{
		"16 bytes lowercase":      {lower, true},
		"16 bytes uppercase":      {upper, true},
		"16 bytes mixed case":     {strings.Repeat("aB", 16), true},
		"64 bytes":                {strings.Repeat("ab", 64), true},
		"15 bytes is under floor": {strings.Repeat("ab", 15), false},
		"65 bytes is over cap":    {strings.Repeat("ab", 65), false},
		"odd length":              {strings.Repeat("ab", 16) + "c", false},
		"non hex":                 {strings.Repeat("zz", 16), false},
		"empty":                   {"", false},
		"whitespace padded":       {" " + lower, false},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ValidateRunToken(tc.in)
			if tc.want && err != nil {
				t.Fatalf("ValidateRunToken(%q): %v", tc.in, err)
			}
			if !tc.want && err == nil {
				t.Fatalf("ValidateRunToken(%q) = nil error, want a configuration error", tc.in)
			}
		})
	}
}

func TestValidateRunTokenErrorNeverEchoesTheTokenValue(t *testing.T) {
	// Contract v1.2.6 §5: "Never echo the value." A truncated-but-secret-looking token must not
	// reach a CI log through the diagnostic that rejects it.
	secret := strings.Repeat("dead", 9) + "a" // odd number of hex digits => invalid
	_, err := ValidateRunToken(secret)
	if err == nil {
		t.Fatal("ValidateRunToken accepted an odd-length token")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error %q echoes the token value", err)
	}

	var cfgErr *ConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("error is %T, want *ConfigError", err)
	}
	if cfgErr.Value != "" {
		t.Fatalf("ConfigError.Value = %q, want empty for a token fault", cfgErr.Value)
	}
	if cfgErr.Source != EnvRunToken {
		t.Fatalf("Source = %q, want %q", cfgErr.Source, EnvRunToken)
	}
}

func TestGenerateRunTokenProducesConformingLowercaseHex(t *testing.T) {
	seen := make(map[RunToken]bool, 64)
	for i := 0; i < 64; i++ {
		tok, err := GenerateRunToken()
		if err != nil {
			t.Fatalf("GenerateRunToken: %v", err)
		}
		if _, err := ValidateRunToken(string(tok)); err != nil {
			t.Fatalf("GenerateRunToken produced a value ValidateRunToken rejects: %v", err)
		}
		if string(tok) != strings.ToLower(string(tok)) {
			t.Fatalf("GenerateRunToken = %q, want lowercase hex", tok)
		}
		if len(tok) < 32 {
			t.Fatalf("GenerateRunToken produced %d hex chars, want at least 32 (16 bytes)", len(tok))
		}
		if seen[tok] {
			t.Fatalf("GenerateRunToken repeated a value after %d draws", i)
		}
		seen[tok] = true
	}
}

func TestHashRunTokenHashesDecodedBytesNotHexText(t *testing.T) {
	// Contract v1.2.6 §5, claim D1: the digest is taken over the token's DECODED BYTES. Hashing
	// the hex text would make AB… and ab… differ despite decoding to identical bytes, and that
	// only surfaces when a shell, a CI secret store or a Windows environment round-trip
	// case-folds the value.
	lower := RunToken(strings.Repeat("ab", 16))
	upper := RunToken(strings.ToUpper(string(lower)))

	gotLower, err := HashRunToken(lower)
	if err != nil {
		t.Fatalf("HashRunToken(lowercase): %v", err)
	}
	gotUpper, err := HashRunToken(upper)
	if err != nil {
		t.Fatalf("HashRunToken(uppercase): %v", err)
	}
	if gotLower != gotUpper {
		t.Fatalf("HashRunToken is case-sensitive: %q != %q", gotLower, gotUpper)
	}

	raw, err := hex.DecodeString(string(lower))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if want := hex.EncodeToString(sum[:]); gotLower != want {
		t.Fatalf("HashRunToken = %q, want the sha256 of the decoded bytes %q", gotLower, want)
	}
	// The wrong one-liner, pinned so a future "simplification" to it fails here.
	wrong := sha256.Sum256([]byte(lower))
	if gotLower == hex.EncodeToString(wrong[:]) {
		t.Fatal("HashRunToken hashed the hex text instead of the decoded bytes")
	}
}

func TestHashRunTokenEmitsLowercaseHexAndRejectsNonHex(t *testing.T) {
	got, err := HashRunToken(RunToken(strings.ToUpper(strings.Repeat("ab", 16))))
	if err != nil {
		t.Fatalf("HashRunToken: %v", err)
	}
	if got != strings.ToLower(got) {
		t.Fatalf("HashRunToken = %q, want lowercase hex", got)
	}
	if _, err := HashRunToken(RunToken("zz")); err == nil {
		t.Fatal("HashRunToken accepted a non-hex token")
	}
}

func TestTokenHashesEqualComparesNormalizedDigestsInConstantTime(t *testing.T) {
	lower := strings.Repeat("0a", 32)
	if !TokenHashesEqual(lower, strings.ToUpper(lower)) {
		t.Fatal("TokenHashesEqual is case-sensitive; contract v1.2.6 §5 normalizes before comparing")
	}
	if TokenHashesEqual(lower, strings.Repeat("0b", 32)) {
		t.Fatal("TokenHashesEqual accepted two different digests")
	}
	if TokenHashesEqual("", "") {
		t.Fatal("TokenHashesEqual accepted two empty digests; an absent digest is never a match")
	}
	if TokenHashesEqual(lower, lower[:len(lower)-2]) {
		t.Fatal("TokenHashesEqual accepted digests of different length")
	}
}
