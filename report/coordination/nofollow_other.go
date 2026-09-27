//go:build !unix

package coordination

// oNoFollow has no portable equivalent outside Unix. Callers fall back to an lstat pre-check,
// which is not atomic; see openFileNoFollow. Creating a symlink on Windows requires a privilege
// ordinary CI accounts do not hold, so the exposure this guards is materially smaller there.
const oNoFollow = 0

const noFollowSupported = false
