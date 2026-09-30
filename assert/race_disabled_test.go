//go:build !race

package assert

// raceEnabled reports whether the test binary was built with -race. See race_enabled_test.go.
const raceEnabled = false
