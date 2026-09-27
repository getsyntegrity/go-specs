package coordination

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	shardFileSuffix = ".shard.json"
	// prefixSeparator splits the readability prefix from the identity digest. Nothing parses the
	// filename by byte offset — the prefix is variable-length — so a reader locates the digest by
	// splitting on the final separator, or simply recomputes it from the envelope.
	prefixSeparator = "--"
	// maxPrefixBytes bounds the readability prefix. Without the bound, the fixed 77 bytes of
	// digest, separator and suffix leave 178 bytes for the prefix, and percent-escaping consumes
	// up to three bytes per unsafe byte — so a legal Go import path spanning enough directory
	// components would fail to produce a filename even though the package builds and tests
	// normally (contract v1.2.7 §5).
	maxPrefixBytes = 40
	// maxShardFileNameBytes is what the budget above adds up to: 40 + 2 + 64 + 11. The filename is
	// bounded at this, not fixed at it — nothing is padded, and a short import path produces a
	// short prefix.
	maxShardFileNameBytes = maxPrefixBytes + len(prefixSeparator) + 2*sha256.Size + len(shardFileSuffix)
)

// ShardFileName returns the shard filename for a package import path.
//
// Normatively: the SHA-256 digest alone carries producer identity. The prefix is readability
// only — it is never parsed, never compared, never used to look a shard up, and never used to
// reconstruct a package path. Two shard filenames are "for the same package" if and only if their
// digests are equal, whatever their prefixes say (contract v1.2.7 §5, §10).
//
// That is not merely because the sanitization is lossy but because it is not injective: "foo/bar"
// and "foo_bar" both sanitize to "foo_bar", and would produce the same filename under a
// sanitized-only scheme — one silently overwriting the other.
//
// The digest is taken over the ORIGINAL, unsanitized import path, which is also what the envelope
// carries, so the finalizer can recompute it and reject any shard whose filename disagrees with
// its own contents.
func ShardFileName(packagePath string) string {
	sum := sha256.Sum256([]byte(packagePath))
	return sanitizedPrefix(packagePath) + prefixSeparator + hex.EncodeToString(sum[:]) + shardFileSuffix
}

// sanitizedPrefix produces the human-readable, non-identifying part of the filename: "/" becomes
// "_", every other byte outside [A-Za-z0-9_.-] is percent-escaped, and the result is bounded.
//
// Truncation applies to the sanitized string, which is pure ASCII by construction — every unsafe
// byte has already become a %XY triplet — so the only rule that bites is that truncation must not
// split a triplet. Backing off to the preceding safe boundary yields a prefix of 38 to 40 bytes.
func sanitizedPrefix(packagePath string) string {
	var b strings.Builder
	for i := 0; i < len(packagePath); i++ {
		c := packagePath[i]
		switch {
		case c == '/':
			b.WriteByte('_')
		case isRunIDByte(c):
			// [A-Za-z0-9_.-], the same safe class used for run identifiers.
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return boundPrefix(b.String())
}

// boundPrefix truncates to at most maxPrefixBytes without splitting a %XY triplet.
func boundPrefix(s string) string {
	used := 0
	for i := 0; i < len(s); {
		width := 1
		if s[i] == '%' {
			width = 3
		}
		if used+width > maxPrefixBytes {
			return s[:used]
		}
		used += width
		i += width
	}
	return s
}
