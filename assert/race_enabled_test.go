//go:build race

package assert

// raceEnabled reports whether the test binary was built with -race. Wall-clock sanity checks widen
// their limit under the race detector, which slows reflection-heavy code several times over.
const raceEnabled = true
