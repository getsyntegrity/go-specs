package coordination

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// maxShardBytes bounds a shard's read before decoding. A shard carries a full package
// NormalizedReport — potentially many suites, cases and captured output — so the budget is far
// larger than run.json's (marker.go), but it is still bounded: an unbounded read of a
// finalizer-glob-discovered, attacker-influenceable path is an OOM waiting to happen
// (contract v1.2.8 §10), same rationale as marker.go's maxMarkerBytes.
const maxShardBytes = 64 << 20

// maxConfigErrorReadBytes bounds config-error.json's read the same way marker.go bounds run.json:
// the record is a few hundred bytes, so anything far larger is corrupt or hostile.
const maxConfigErrorReadBytes = 64 << 10

// The closed shard-rejection reason vocabulary from contract v1.2.8 §7. RejectedShard.Reason is
// always one of these; nothing else belongs in it.
const (
	ReasonCorrupt               = "corrupt"
	ReasonDuplicatePackage      = "duplicate-package"
	ReasonUnexpectedPackage     = "unexpected-package"
	ReasonSchemaVersionMismatch = "schema-version-mismatch"
	ReasonWrongRunID            = "wrong-run-id"
	ReasonOwnershipMismatch     = "ownership-mismatch"
	ReasonFilenameHashMismatch  = "filename-hash-mismatch"
	ReasonStale                 = "stale"
)

// RejectedShard is one shard the finalizer found but refused to merge, and why (contract v1.2.8
// §7). A shard is never silently dropped or silently picked among duplicates: every rejection is
// reported here.
type RejectedShard struct {
	Path   string
	Reason string
}

// ErrInvalidExpectedProducers reports that FinalizeOptions.ExpectedProducers was empty, nil, or
// contained no usable package path after normalization.
//
// The finalizer never infers this list itself — not even with `go list ./...`, which returns the
// broader module-discovery set and would misreport packages that do not link go-specs as missing
// producers (contract v1.2.8 §5, §7, "Expected producer set"). An invoker that asked for strict
// verification but supplied nothing to verify against is a misconfiguration, not an empty-but-
// valid result.
var ErrInvalidExpectedProducers = errors.New(
	"go-specs report: FinalizeOptions.ExpectedProducers is required and must be a non-empty, " +
		"invoker-supplied list of expected package paths; it is never inferred (e.g. via `go list`)")

// shardDiscoveryOptions groups Finalize's shard-discovery inputs (contract v1.2.8 §7 steps 3-5).
type shardDiscoveryOptions struct {
	BaseDir string
	RunID   RunID
	// RunTokenHash is the digest VerifyRunOwnership already proved for THIS process, never the raw
	// token. Every shard's own RunTokenHash is compared against it in constant time.
	RunTokenHash      string
	ExpectedProducers []string
}

// shardDiscoveryResult is what discoverShards found: every accepted envelope (ordered by
// PackagePath, ready for mergeReports), every expected producer with no valid shard, and every
// shard rejected, with why.
type shardDiscoveryResult struct {
	Accepted []ShardEnvelope
	Missing  []string
	Rejected []RejectedShard
}

// discoverShards lists, reads and verifies every final shard file for one run, against the
// authoritative ExpectedProducers list, and reports config-error.json separately through
// readConfigErrorIfPresent.
//
// It never opens a `.tmp-*` file: the finalizer only ever globs the final naming pattern, so a
// shard is atomically either wholly absent or wholly present (contract v1.2.8 §5, publish.go).
// Every rejection uses the closed reason vocabulary above; nothing is ever silently dropped or
// silently picked among duplicates.
func discoverShards(opts shardDiscoveryOptions) (shardDiscoveryResult, error) {
	expected, err := normalizeExpectedProducers(opts.ExpectedProducers)
	if err != nil {
		return shardDiscoveryResult{}, err
	}

	marker, err := readMarker(markerPath(opts.BaseDir, opts.RunID))
	if err != nil {
		return shardDiscoveryResult{}, err
	}

	dir := shardsDir(opts.BaseDir, opts.RunID)
	names, err := listShardFiles(dir)
	if err != nil {
		return shardDiscoveryResult{}, err
	}

	type candidate struct {
		path string
		env  ShardEnvelope
	}
	var valid []candidate
	var rejected []RejectedShard

	for _, name := range names {
		path := filepath.Join(dir, name)
		env, err := readShardEnvelope(path)
		if err != nil {
			rejected = append(rejected, RejectedShard{Path: path, Reason: ReasonCorrupt})
			continue
		}
		if env.ShardSchemaVersion != ShardSchemaVersion {
			rejected = append(rejected, RejectedShard{Path: path, Reason: ReasonSchemaVersionMismatch})
			continue
		}
		if env.RunID != opts.RunID {
			rejected = append(rejected, RejectedShard{Path: path, Reason: ReasonWrongRunID})
			continue
		}
		if !TokenHashesEqual(env.RunTokenHash, opts.RunTokenHash) {
			rejected = append(rejected, RejectedShard{Path: path, Reason: ReasonOwnershipMismatch})
			continue
		}
		if !filenameDigestMatches(name, env.PackagePath) {
			rejected = append(rejected, RejectedShard{Path: path, Reason: ReasonFilenameHashMismatch})
			continue
		}
		if env.ProducedAt.Before(marker.CreatedAt) {
			rejected = append(rejected, RejectedShard{Path: path, Reason: ReasonStale})
			continue
		}
		valid = append(valid, candidate{path: path, env: env})
	}

	// Duplicate producer identity: two shard filenames identify the same package if and only if
	// their digests are equal, whatever their (never-parsed, readability-only) prefixes say
	// (shardname.go). Two valid shards whose envelopes report the same PackagePath are BOTH
	// rejected — never silently picked between — and neither counts as found (contract v1.2.8 §5,
	// "Duplicate producer identity").
	byPkg := make(map[string][]candidate, len(valid))
	for _, c := range valid {
		byPkg[c.env.PackagePath] = append(byPkg[c.env.PackagePath], c)
	}

	expectedSet := make(map[string]bool, len(expected))
	for _, p := range expected {
		expectedSet[p] = true
	}

	var accepted []ShardEnvelope
	found := make(map[string]bool, len(expected))
	for pkg, cands := range byPkg {
		if len(cands) > 1 {
			for _, c := range cands {
				rejected = append(rejected, RejectedShard{Path: c.path, Reason: ReasonDuplicatePackage})
			}
			continue
		}
		c := cands[0]
		if !expectedSet[pkg] {
			rejected = append(rejected, RejectedShard{Path: c.path, Reason: ReasonUnexpectedPackage})
			continue
		}
		accepted = append(accepted, c.env)
		found[pkg] = true
	}

	var missing []string
	for _, p := range expected {
		if !found[p] {
			missing = append(missing, p)
		}
	}
	sort.Strings(missing)
	sort.Slice(accepted, func(i, j int) bool { return accepted[i].PackagePath < accepted[j].PackagePath })
	sort.Slice(rejected, func(i, j int) bool { return rejected[i].Path < rejected[j].Path })

	return shardDiscoveryResult{Accepted: accepted, Missing: missing, Rejected: rejected}, nil
}

// normalizeExpectedProducers trims whitespace, drops empties, de-duplicates and sorts
// ExpectedProducers (contract v1.2.8 §7: "The list is normalized ..., de-duplicated"). An empty
// result — nil input, an empty slice, or a slice of nothing but blanks — is
// ErrInvalidExpectedProducers: the finalizer requires an authoritative, non-empty, invoker-
// supplied list and never derives one itself.
func normalizeExpectedProducers(pkgs []string) ([]string, error) {
	seen := make(map[string]bool, len(pkgs))
	out := make([]string, 0, len(pkgs))
	for _, p := range pkgs {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, ErrInvalidExpectedProducers
	}
	sort.Strings(out)
	return out, nil
}

// listShardFiles returns the final (non-`.tmp-*`) shard file names in dir, sorted for a
// deterministic scan order. A run directory whose shards subdirectory does not exist yet (the run
// never got far enough to publish anything) is reported as zero shards, not an error.
func listShardFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("go-specs report: list %s: %w", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		// The finalizer only ever globs the final naming pattern and never opens a `.tmp-*` file:
		// a temp name is finalName + ".tmp-<pid>", which never ends in shardFileSuffix.
		if !strings.HasSuffix(e.Name(), shardFileSuffix) {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names, nil
}

// readShardEnvelope performs a bounded, no-follow read of one shard file, the same discipline
// marker.go's readMarker applies to run.json.
func readShardEnvelope(path string) (ShardEnvelope, error) {
	f, err := openFileNoFollow(path, os.O_RDONLY, 0)
	if err != nil {
		return ShardEnvelope{}, fmt.Errorf("go-specs report: read %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	raw, err := io.ReadAll(io.LimitReader(f, maxShardBytes))
	if err != nil {
		return ShardEnvelope{}, fmt.Errorf("go-specs report: read %s: %w", path, err)
	}
	var env ShardEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return ShardEnvelope{}, fmt.Errorf("go-specs report: decode %s: %w", path, err)
	}
	return env, nil
}

// filenameDigestMatches reports whether name's identity digest — the 64 lowercase hex characters
// between the FINAL prefixSeparator and the shardFileSuffix — equals sha256(packagePath)'s hex
// digest.
//
// Per shardname.go's own contract, the digest alone carries producer identity: "Two shard
// filenames are for the same package if and only if their digests are equal, whatever their
// prefixes say." The readability prefix is therefore never compared, and it is never parsed by
// byte offset either — it is variable-length and may itself legally contain "--" — so the digest
// is located from the end of the string, exactly as shardname.go documents.
func filenameDigestMatches(name, packagePath string) bool {
	got, ok := shardDigest(name)
	if !ok {
		return false
	}
	want, ok := shardDigest(ShardFileName(packagePath))
	if !ok {
		return false
	}
	return got == want
}

// shardDigest extracts a shard filename's identity digest, or reports ok=false when name is not
// shaped like a shard filename at all.
func shardDigest(name string) (digest string, ok bool) {
	if !strings.HasSuffix(name, shardFileSuffix) {
		return "", false
	}
	trimmed := strings.TrimSuffix(name, shardFileSuffix)
	idx := strings.LastIndex(trimmed, prefixSeparator)
	if idx < 0 {
		return "", false
	}
	digest = trimmed[idx+len(prefixSeparator):]
	if len(digest) != 2*sha256.Size {
		return "", false
	}
	return digest, true
}

// readConfigErrorIfPresent reads <BaseDir>/<RunID>/config-error.json, if present.
//
// It returns (nil, nil) when the file does not exist — the ordinary case, no pre-test
// configuration failure was recorded — and a non-nil record when one was. Finalize maps a non-nil
// record to a result with no merge and no rendering, so the caller can map it to exit 78
// (contract v1.2.8 §7 step 2, §8).
func readConfigErrorIfPresent(baseDir string, runID RunID) (*ConfigErrorRecord, error) {
	path := configErrorPath(baseDir, runID)
	f, err := openFileNoFollow(path, os.O_RDONLY, 0)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("go-specs report: read %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	raw, err := io.ReadAll(io.LimitReader(f, maxConfigErrorReadBytes))
	if err != nil {
		return nil, fmt.Errorf("go-specs report: read %s: %w", path, err)
	}
	var rec ConfigErrorRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return nil, fmt.Errorf("go-specs report: decode %s: %w", path, err)
	}
	return &rec, nil
}
