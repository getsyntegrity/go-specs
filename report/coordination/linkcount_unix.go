//go:build unix

package coordination

import (
	"fmt"
	"os"
	"syscall"
)

// linkCount returns the number of directory entries pointing at path.
//
// It exists for one narrow purpose: distinguishing a genuine duplicate producer from NFS
// reporting EEXIST for a link that actually succeeded (contract v1.2.7 §10). See publishExclusive.
func linkCount(path string) (uint64, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return 0, err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, fmt.Errorf("go-specs report: %s: link count is unavailable on this platform", path)
	}
	return uint64(st.Nlink), nil
}
