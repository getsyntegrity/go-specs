package coordination

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strings"
)

// Environment variables that carry run coordination. Contract v1.2.7 §5 defines each one; they
// are named here so a diagnostic can never say "invalid reporting configuration" without saying
// which variable is at fault.
const (
	// EnvGate is the single activation gate. Nothing else turns coordination on.
	EnvGate = "GO_SPECS_REPORT_SHARDS"
	// EnvRunID is the readable, per-invocation run identifier. It is never proof of ownership.
	EnvRunID = "GO_SPECS_RUN_ID"
	// EnvRunToken is the per-invocation ownership nonce. It is never used as a path segment.
	EnvRunToken = "GO_SPECS_RUN_TOKEN"
	// EnvReportDir is the base directory for run subdirectories.
	EnvReportDir = "GO_SPECS_REPORT_DIR"
)

// DefaultBaseDir is used when EnvReportDir is unset (contract v1.2.7 §5).
const DefaultBaseDir = ".go-specs/runs"

// Patterns each identity value must match, kept as strings because they are quoted verbatim in
// diagnostics. The validators below implement them directly rather than through regexp: they run
// once per process, and an explicit loop keeps the rejection reason specific.
const (
	runIDPattern    = `^[A-Za-z0-9_.-]{1,128}$`
	runTokenPattern = `^([0-9a-fA-F]{2}){16,64}$`
)

const (
	maxRunIDLen = 128
	// Token bounds in bytes, before hex encoding. The floor is normative (contract v1.2.7 §10):
	// below it a conformant implementation could seed math/rand from the wall clock and two CI
	// jobs starting in the same second would produce the same token.
	minTokenBytes = 16
	maxTokenBytes = 64
	// generatedTokenBytes sits well above the floor and inside the cap.
	generatedTokenBytes = 32
)

// RunID identifies one module-wide `go test` invocation across all its package processes. It
// must come from the environment: no in-process mechanism can produce a value that multiple
// independently-launched package binaries would agree on (contract v1.2.7 F8).
//
// A RunID is a readable label and a path segment. It is never proof of ownership — two unrelated
// invocations that reuse one present identical RunIDs to every process involved. RunToken is what
// tells them apart.
type RunID string

// RunToken is the random, per-invocation ownership nonce. Unlike RunID it is never used for path
// construction; it exists only to be hashed and compared against the run marker
// (contract v1.2.7 §5).
//
// It is an ownership and collision-detection mechanism, not a security boundary: it travels in
// the environment, so every process in the test's own process tree can read it. It defends
// against accident, never against a local adversary already running as the invoking user
// (contract v1.2.7 §10). Nothing may treat a matching token as an authorization decision.
type RunToken string

// ValidateRunID enforces runIDPattern and additionally rejects "." and "..", which the character
// class alone admits. Both are legal under the pattern and neither is a usable directory name:
// contract v1.2.7 §10 requires traversal segments rejected before the value reaches a path.
func ValidateRunID(s string) (RunID, error) {
	if s == "" {
		return "", &ConfigError{
			Source: EnvRunID,
			Reason: ReasonInvalidRunID,
			Detail: fmt.Sprintf("the value is empty; it must match %s", runIDPattern),
			Remedy: remedyFor(EnvRunID),
		}
	}
	invalid := func(detail string) error {
		return &ConfigError{
			Source: EnvRunID,
			Value:  fmt.Sprintf("%q", s),
			Reason: ReasonInvalidRunID,
			Detail: detail,
			Remedy: remedyFor(EnvRunID),
		}
	}
	if len(s) > maxRunIDLen {
		return "", invalid(fmt.Sprintf("the value is %d bytes; it must match %s", len(s), runIDPattern))
	}
	for i := 0; i < len(s); i++ {
		if !isRunIDByte(s[i]) {
			return "", invalid(fmt.Sprintf("byte %d is not permitted; the value must match %s", i, runIDPattern))
		}
	}
	// "." and "-" are in the character class, so the pattern accepts these two on its own. They
	// would name the run directory's own parent or itself.
	if s == "." || s == ".." {
		return "", invalid(fmt.Sprintf("%q is a path traversal segment, not a run identifier; the value must match %s and name a directory", s, runIDPattern))
	}
	return RunID(s), nil
}

func isRunIDByte(b byte) bool {
	switch {
	case b >= 'A' && b <= 'Z', b >= 'a' && b <= 'z', b >= '0' && b <= '9':
		return true
	case b == '_', b == '.', b == '-':
		return true
	default:
		return false
	}
}

// ValidateRunToken enforces runTokenPattern: the hex encoding of at least 16 and at most 64 bytes.
//
// The pair quantifier is deliberate. `^[0-9a-fA-F]{32,128}$` would admit odd lengths, which are
// not valid hex encodings of any byte string and would fail inside hex.DecodeString at the point
// of use rather than here (contract v1.2.7 §10).
//
// This is a length and charset check. It cannot verify the source of the entropy, which is why
// the crypto/rand floor is stated normatively and GenerateRunToken exists.
//
// The token value is never placed in the returned error: contract v1.2.7 §5 forbids echoing it.
func ValidateRunToken(s string) (RunToken, error) {
	invalid := func(detail string) error {
		return &ConfigError{
			Source: EnvRunToken,
			// Value stays empty: the token is never echoed, not even when it is malformed.
			Reason: ReasonInvalidRunToken,
			Detail: detail,
			Remedy: remedyFor(EnvRunToken),
		}
	}
	if s == "" {
		return "", invalid(fmt.Sprintf("the value is empty; it must match %s", runTokenPattern))
	}
	if len(s)%2 != 0 {
		return "", invalid(fmt.Sprintf("the value has an odd number of hex digits (%d) and cannot encode whole bytes; it must match %s", len(s), runTokenPattern))
	}
	if n := len(s) / 2; n < minTokenBytes || n > maxTokenBytes {
		return "", invalid(fmt.Sprintf("the value encodes %d bytes; it must encode between %d and %d and match %s", n, minTokenBytes, maxTokenBytes, runTokenPattern))
	}
	for i := 0; i < len(s); i++ {
		if !isHexByte(s[i]) {
			return "", invalid(fmt.Sprintf("byte %d is not a hex digit; the value must match %s", i, runTokenPattern))
		}
	}
	return RunToken(s), nil
}

func isHexByte(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}

// GenerateRunToken produces a conforming token: bytes from crypto/rand, hex-encoded in lowercase.
//
// It exists so the preflight step has one obviously-correct path and nobody reaches for math/rand
// or a clock-derived value. A charset check cannot detect that mistake — two jobs starting in the
// same second would produce the same token and the run marker would cheerfully report a match
// (contract v1.2.7 §10).
//
// The output is lowercase regardless of what the pattern permits, so the mixed-case path stays a
// robustness guarantee rather than a routine one.
func GenerateRunToken() (RunToken, error) {
	raw := make([]byte, generatedTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("go-specs report: generate run token: %w", err)
	}
	return RunToken(hex.EncodeToString(raw)), nil
}

// HashRunToken hex-decodes the token and returns the lowercase hex SHA-256 digest of the
// resulting BYTES — never of the token's hex text.
//
// Hashing the text would make "AB…" and "ab…" produce different digests despite decoding to
// identical bytes, and verification would then fail on two values that are byte-identical. That
// defect surfaces only when some layer between the generator and the test binary case-folds the
// value, which is exactly what a shell, a CI secret store or a Windows environment round-trip can
// do — making it both rare and extremely hard to diagnose (contract v1.2.7 §5).
//
// This digest is the only form of the token ever written to disk.
//
// Reproducing it by hand, for anyone debugging an ownership mismatch at 2am — the obvious
// one-liner is wrong, because it hashes the hex text rather than the bytes it encodes:
//
//	printf %s "$GO_SPECS_RUN_TOKEN" | sha256sum                 # wrong  — hashes the hex text
//	printf %s "$GO_SPECS_RUN_TOKEN" | xxd -r -p | sha256sum     # correct — hashes the bytes
//	printf %s "$GO_SPECS_RUN_TOKEN" | xxd -r -p | shasum -a 256 # macOS, which has no sha256sum
func HashRunToken(t RunToken) (string, error) {
	raw, err := hex.DecodeString(string(t))
	if err != nil {
		// Callers that already ran ValidateRunToken may treat this as unreachable, but must not
		// discard it silently.
		return "", fmt.Errorf("go-specs report: decode %s: %w", EnvRunToken, err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// TokenHashesEqual reports whether two token digests match. It normalizes both to lowercase and
// then compares in constant time (contract v1.2.7 §5).
//
// Normalizing is safe here and only here: these are public digests, so lowercasing them leaks
// nothing. Normalizing the token itself would be a different matter. An empty digest never
// matches — an absent marker value is a failure to verify, never a successful verification.
func TokenHashesEqual(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	x := []byte(strings.ToLower(a))
	y := []byte(strings.ToLower(b))
	return subtle.ConstantTimeCompare(x, y) == 1
}
