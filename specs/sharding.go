// sharding.go provides deterministic test sharding for CI: split specs across jobs by index.
//
// Usage: run only the Nth shard of M total (e.g. shard 2 of 10). Filter before execution so
// the runner's loop is unchanged and allocation-free.
//
//   specs := collectSpecs()
//   if shard, total, ok := ParseShardFlag(os.Args); ok {
//       specs = ShardSpecs(specs, shard, total)
//   }
//   runner := NewMinimalRunnerFromSpecs(specs)
//   runner.Run(t)
package specs

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ShardSpecs returns the subset of specs belonging to the given shard (deterministic).
// Spec index i is included when i%total == shard. total must be > 0 and 0 <= shard < total.
// Allocations happen here (filtered slice); execution loop over the result has no extra allocations.
func ShardSpecs(specs []RunSpec, shard, total int) []RunSpec {
	if total <= 0 || shard < 0 || shard >= total {
		return specs
	}
	out := make([]RunSpec, 0, len(specs)/total+1)
	for i := range specs {
		if i%total == shard {
			out = append(out, specs[i])
		}
	}
	return out
}

// ShardBCProgram returns a BCProgram containing only specs whose index s satisfies s%total == shard.
// Deterministic; allocates new Code and SpecStarts; execution loop over the result is unchanged.
func ShardBCProgram(prog BCProgram, shard, total int) BCProgram {
	if total <= 0 || shard < 0 || shard >= total {
		return prog
	}
	nSpecs := prog.NumSpecs()
	if nSpecs == 0 {
		return prog
	}
	code := prog.Code
	starts := prog.SpecStarts

	var newCode []instruction
	newStarts := make([]int, 0, nSpecs/total+2)
	newStarts = append(newStarts, 0)

	for s := 0; s < nSpecs; s++ {
		if s%total != shard {
			continue
		}
		start, end := starts[s], starts[s+1]
		for j := start; j < end; j++ {
			newCode = append(newCode, code[j])
		}
		newStarts = append(newStarts, len(newCode))
	}

	if len(newStarts) == 1 {
		return BCProgram{Code: nil, SpecStarts: nil}
	}
	return BCProgram{Code: newCode, SpecStarts: newStarts}
}

// ParseShardFlag parses -shard <shard>/<total> from args (e.g. -shard 2/10).
// Returns (shard, total, true) when valid; (0, 0, false) otherwise.
// Use from TestMain: if shard, total, ok := ParseShardFlag(os.Args); ok { ... }
func ParseShardFlag(args []string) (shard, total int, ok bool) {
	for i := 0; i < len(args)-1; i++ {
		if args[i] != "-shard" {
			continue
		}
		return parseShardPair(args[i+1])
	}
	return 0, 0, false
}

// ParseShardEnv reads shard index and total from environment (e.g. CI sets SHARD_INDEX and SHARD_TOTAL).
// Alternatively supports SHARD=2/10. Returns (shard, total, true) when both are set and valid.
func ParseShardEnv() (shard, total int, ok bool) {
	if s := os.Getenv("SHARD"); s != "" {
		return parseShardPair(s)
	}
	idx := os.Getenv("SHARD_INDEX")
	tot := os.Getenv("SHARD_TOTAL")
	if idx == "" || tot == "" {
		return 0, 0, false
	}
	sh, err1 := strconv.Atoi(idx)
	totN, err2 := strconv.Atoi(tot)
	if err1 != nil || err2 != nil || totN <= 0 || sh < 0 || sh >= totN {
		return 0, 0, false
	}
	return sh, totN, true
}

// ParseShardString parses "N/M" (e.g. "2/10") into (shard, total). Returns (0, 0, false) when invalid.
func ParseShardString(s string) (shard, total int, ok bool) {
	return parseShardPair(s)
}

func parseShardPair(s string) (shard, total int, ok bool) {
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	sh, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	tot, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil || tot <= 0 || sh < 0 || sh >= tot {
		return 0, 0, false
	}
	return sh, tot, true
}

// ShardFromArgsOrEnv returns (shard, total, true) if sharding is requested via -shard flag or SHARD/SHARD_INDEX/SHARD_TOTAL env.
// Useful in TestMain to decide whether to call ShardSpecs or ShardBCProgram.
func ShardFromArgsOrEnv() (shard, total int, ok bool) {
	if shard, total, ok = ParseShardFlag(os.Args); ok {
		return shard, total, true
	}
	return ParseShardEnv()
}

// FormatShardFlag returns a flag string for the given shard and total, e.g. "-shard 2/10".
func FormatShardFlag(shard, total int) string {
	return fmt.Sprintf("-shard %d/%d", shard, total)
}
