// sharding_test.go shows how to split one suite across several CI jobs.
//
// Sharding solves slow suites: CI starts M jobs, job N (0-based) runs shard N/M, and together they
// cover every spec exactly once. Assignment is deterministic and round-robin over the suite's
// units, so the same tree gives the same split on every job. A BeforeAll/AfterAll group or an
// ItParallel batch is one unit, because it cannot be split without running a hook twice or not at
// all.
//
// A job learns its shard from the -shard flag (`-shard 1/4`) or from the SHARD, or
// SHARD_INDEX/SHARD_TOTAL, environment variables. A configuration that is present but unusable is
// an error, never "run everything". See docs/CI.md.
package examples_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
)

// BuildSuite compiles once, RunShard runs one slice. Running both halves of a 2-way split runs
// each spec in exactly one shard. Here two shards run in one test; in CI each job runs only its
// own shard, taken from ShardFromArgsOrEnv. Every spec checks only itself: a spec assigned to
// both shards would see its own counter at 2 on the second run and fail. Nothing counts how many
// specs ran, so the example passes however `go test -run` selects subtests.
func TestSharding_runShardPartitionsTheSuite(t *testing.T) {
	seen := map[string]int{}
	suite := specs.BuildSuite(t, "shardable", func(s *specs.Spec) {
		for _, name := range []string{"a", "b", "c", "d", "e"} {
			s.It(name, func(ctx *specs.Context) {
				seen[name]++
				ctx.Expect(seen[name]).ToEqual(1) // this spec ran in one shard only
			})
		}
	})

	const total = 2
	for shard := range total {
		suite.RunShard(t, shard, total)
	}
}

// ParseShardString reads the "N/M" form used by the -shard flag and the SHARD variable.
func Example_parseShardString() {
	shard, total, err := specs.ParseShardString("2/10")
	fmt.Println(shard, total, err)
	// Output: 2 10 <nil>
}

// An empty value means "not configured", reported as ErrShardNotConfigured. Test it with
// errors.Is, and treat it as "run everything". Any other error is a misconfiguration
// (*specs.ShardConfigError) that must fail the job.
func Example_parseShardStringErrors() {
	_, _, err := specs.ParseShardString("")
	fmt.Println("not configured:", errors.Is(err, specs.ErrShardNotConfigured))

	_, _, err = specs.ParseShardString("5/3")
	var cfgErr *specs.ShardConfigError
	fmt.Println("misconfigured:", errors.As(err, &cfgErr))
	// Output:
	// not configured: true
	// misconfigured: true
}

// FormatShardFlag is the inverse: it builds the flag a CI script should pass.
func Example_formatShardFlag() {
	fmt.Println(specs.FormatShardFlag(1, 4))
	// Output: -shard 1/4
}

// ShardFromArgsOrEnv is what a TestMain calls. The -shard flag wins when present, then the
// environment. With neither, it returns ErrShardNotConfigured and the whole suite runs. Setting
// SHARD makes the environment path deterministic for this example.
func TestSharding_fromEnvironment(t *testing.T) {
	t.Setenv("SHARD", "1/3")
	shard, total, err := specs.ShardFromArgsOrEnv()
	if err != nil || shard != 1 || total != 3 {
		t.Fatalf("got %d/%d err=%v, want 1/3", shard, total, err)
	}
}
