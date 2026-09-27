//go:build race

package specs

// raceEnabled reports whether the test binary was built with -race. Under the race detector,
// sync.Pool.Put randomly drops one item in four on purpose, so pooled paths allocate and
// allocation contracts that depend on pool reuse cannot be measured.
const raceEnabled = true
