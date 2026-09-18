package coordination

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestShardFileNameSeparatesCollidingSanitizations(t *testing.T) {
	// Claim C4, and hardening item 1 of issue #145. The sanitization "/" -> "_" is not injective:
	// these two distinct import paths sanitize to the same prefix. Under a sanitized-only scheme
	// they would produce one filename and one of the two shards would silently overwrite the
	// other.
	a := ShardFileName("foo/bar")
	b := ShardFileName("foo_bar")

	if a == b {
		t.Fatalf("both package paths produced %q; one shard would overwrite the other", a)
	}
	if sanitizedPrefix("foo/bar") != sanitizedPrefix("foo_bar") {
		t.Fatal("the test no longer exercises a real prefix collision; pick two paths that still collide")
	}
	if !strings.HasPrefix(a, "foo_bar--") || !strings.HasPrefix(b, "foo_bar--") {
		t.Fatalf("prefixes changed: %q / %q", a, b)
	}
}

func TestShardFileNameIdentityIsTheDigestOfTheUnsanitizedPath(t *testing.T) {
	const pkg = "example.com/Some-Module/pkg v2"
	name := ShardFileName(pkg)

	sum := sha256.Sum256([]byte(pkg))
	want := hex.EncodeToString(sum[:])
	if !strings.Contains(name, prefixSeparator+want+shardFileSuffix) {
		t.Fatalf("%q does not carry the digest of the original, unsanitized path", name)
	}
	if strings.ToLower(want) != want {
		t.Fatal("the digest must be lowercase hex")
	}
}

func TestShardFileNameStaysInsideItsByteBudget(t *testing.T) {
	// Claim C3. The filename is bounded at 117 bytes, not fixed at it: nothing is padded.
	for name, pkg := range map[string]string{
		"short":                 "a",
		"ordinary":              "github.com/getsyntegrity/go-specs/report/coordination",
		"very deep":             strings.Repeat("averylongsegment/", 40) + "leaf",
		"non ascii":             strings.Repeat("パッケージ/", 20) + "leaf",
		"all unsafe bytes":      strings.Repeat("\x01\x02\x03", 50),
		"escape on the seam":    strings.Repeat("a", 38) + "ñ" + strings.Repeat("b", 40),
		"escape spanning seam2": strings.Repeat("a", 39) + "ñ" + strings.Repeat("b", 40),
		"escape spanning seam3": strings.Repeat("a", 40) + "ñ" + strings.Repeat("b", 40),
	} {
		t.Run(name, func(t *testing.T) {
			got := ShardFileName(pkg)
			if len(got) > maxShardFileNameBytes {
				t.Fatalf("filename is %d bytes, budget is %d: %q", len(got), maxShardFileNameBytes, got)
			}

			prefix := strings.TrimSuffix(got, prefixSeparator+got[len(got)-len(shardFileSuffix)-2*sha256.Size:])
			if len(prefix) > maxPrefixBytes {
				t.Fatalf("prefix is %d bytes, budget is %d: %q", len(prefix), maxPrefixBytes, prefix)
			}
			assertNoSplitTriplet(t, prefix)
		})
	}
}

// assertNoSplitTriplet fails when the prefix ends inside a %XY escape. Truncating mid-triplet
// would leave a trailing "%" or "%A", which is not a decodable escape and makes the prefix
// ambiguous to read.
func assertNoSplitTriplet(t *testing.T, prefix string) {
	t.Helper()
	for i := 0; i < len(prefix); {
		if prefix[i] != '%' {
			i++
			continue
		}
		if i+2 >= len(prefix) {
			t.Fatalf("prefix %q ends inside a %%XY escape", prefix)
		}
		for _, c := range prefix[i+1 : i+3] {
			if !isHexByte(byte(c)) {
				t.Fatalf("prefix %q contains a malformed escape at byte %d", prefix, i)
			}
		}
		i += 3
	}
}

func TestBoundedPrefixBacksOffToASafeBoundary(t *testing.T) {
	// A truncation that would land inside a triplet backs off, yielding 38 to 40 bytes.
	for _, pkg := range []string{
		strings.Repeat("a", 38) + "ñ" + "tail",
		strings.Repeat("a", 39) + "ñ" + "tail",
		strings.Repeat("a", 40) + "ñ" + "tail",
	} {
		got := sanitizedPrefix(pkg)
		if len(got) > maxPrefixBytes {
			t.Fatalf("prefix %q is %d bytes", got, len(got))
		}
		if len(got) < maxPrefixBytes-2 {
			t.Fatalf("prefix %q is %d bytes; backing off should never cost more than 2", got, len(got))
		}
		assertNoSplitTriplet(t, got)
	}
}

func TestSanitizationMapsSlashesAndEscapesEverythingElse(t *testing.T) {
	if got := sanitizedPrefix("a/b.c-d_e"); got != "a_b.c-d_e" {
		t.Fatalf("sanitizedPrefix = %q, want the safe class preserved and / mapped to _", got)
	}
	if got := sanitizedPrefix("a b"); got != "a%20b" {
		t.Fatalf("sanitizedPrefix = %q, want unsafe bytes percent-escaped in uppercase hex", got)
	}
}
