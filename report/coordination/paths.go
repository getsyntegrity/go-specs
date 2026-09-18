package coordination

import "path/filepath"

// The run directory layout from contract v1.2.6 §5:
//
//	<BaseDir>/<RunID>/
//	  run.json                                             # ownership marker
//	  config-error.json                                    # pre-test configuration failure record
//	  shards/<bounded-prefix>--<sha256-of-package-path>.shard.json
const (
	markerFileName      = "run.json"
	configErrorFileName = "config-error.json"
	shardsDirName       = "shards"
)

// RunID is validated before it reaches any of these, so it can never contribute a path separator
// or a traversal segment (contract v1.2.6 §10).

func runDir(baseDir string, id RunID) string {
	return filepath.Join(baseDir, string(id))
}

func markerPath(baseDir string, id RunID) string {
	return filepath.Join(runDir(baseDir, id), markerFileName)
}

func shardsDir(baseDir string, id RunID) string {
	return filepath.Join(runDir(baseDir, id), shardsDirName)
}

func configErrorPath(baseDir string, id RunID) string {
	return filepath.Join(runDir(baseDir, id), configErrorFileName)
}
