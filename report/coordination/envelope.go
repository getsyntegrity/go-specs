package coordination

import (
	"time"

	"github.com/getsyntegrity/go-specs/report"
)

// ShardSchemaVersion versions the on-disk shard envelope, independently of report.SchemaVersion.
// A reader checks it before trusting any other field; an incompatible shard is rejected
// explicitly, never partially trusted (contract v1.2.7 §10).
const ShardSchemaVersion = "1"

// ShardEnvelope is the on-disk shape of one shard file.
//
// It carries execution data only: neither pre-aggregated coverage numbers nor a coverage-profile
// path. Producers do not calculate, parse, copy or point at coverage data — the invoker hands the
// one combined `go test -coverprofile` file straight to the finalizer, which alone owns block
// deduplication and coverage arithmetic (contract v1.2.7 §9).
type ShardEnvelope struct {
	ShardSchemaVersion string `json:"shardSchemaVersion"`
	RunID              RunID  `json:"runId"`
	// RunTokenHash binds the shard to the run that owns the directory. The filename digest proves
	// which PACKAGE a shard claims to be; it proves nothing about which RUN wrote it. Without this
	// binding, any process able to write into the run directory could drop a well-formed file into
	// shards/ and have it merged into the module report (contract v1.2.7 §5, §10). The raw token
	// is never serialized.
	RunTokenHash string `json:"runTokenHash"`
	// PackagePath is the original, unsanitized import path. The finalizer recomputes its SHA-256
	// and rejects any shard whose filename digest disagrees with it.
	PackagePath string                  `json:"packagePath"`
	ProducedAt  time.Time               `json:"producedAt"`
	Report      report.NormalizedReport `json:"report"`
}
