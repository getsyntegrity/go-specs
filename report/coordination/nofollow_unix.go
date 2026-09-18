//go:build unix

package coordination

import "syscall"

// oNoFollow makes an open fail rather than traverse a final-component symlink. Contract v1.2.6
// §10 requires it on every open of run.json, config-error.json, shard temp files and shard final
// names, on both the write and the read side: <base>/<run-id>/... is a predictable path, and the
// base directory frequently lives somewhere another local user can reach.
const oNoFollow = syscall.O_NOFOLLOW

// noFollowSupported reports whether the kernel enforces the flag atomically. On Unix it does, so
// no lstat pre-check is needed — and a pre-check would be a TOCTOU race anyway.
const noFollowSupported = true
