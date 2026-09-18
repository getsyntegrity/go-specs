//go:build !unix

package coordination

import "fmt"

// linkCount is unavailable outside Unix. The NFS false-EEXIST recovery it supports is a POSIX
// link(2) behaviour, and the Windows publish path does not use link at all, so an error here
// simply leaves a genuine duplicate reported as a duplicate.
func linkCount(path string) (uint64, error) {
	return 0, fmt.Errorf("go-specs report: %s: link count is unavailable on this platform", path)
}
