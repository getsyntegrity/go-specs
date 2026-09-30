package property

import (
	"fmt"
	"strings"
)

// maxStackLines bounds the panic stack kept in a report; Result.Panic.Stack holds all of it.
const maxStackLines = 24

// report renders res for a person: what happened, the counterexample, and how to replay it.
func report(cfg config, res Result) string {
	var b strings.Builder
	switch res.Outcome {
	case Failed:
		fmt.Fprintf(&b, "property %q falsified: an assertion failed after %d valid checks (%d rejected)\n", cfg.name, res.Checks, res.Rejected)
	case Panicked:
		fmt.Fprintf(&b, "property %q panicked after %d valid checks (%d rejected)\n", cfg.name, res.Checks, res.Rejected)
	case Exhausted:
		fmt.Fprintf(&b, "property %q exhausted: only %d valid inputs were generated, %d were rejected; generate only valid inputs, or relax the assumptions\n", cfg.name, res.Checks, res.Rejected)
		return b.String()
	default:
		return fmt.Sprintf("property %q %v", cfg.name, res.Outcome)
	}

	writeDraws(&b, "counterexample (shrunk)", res.Counterexample)
	if !sameDraws(res.Original, res.Counterexample) {
		writeDraws(&b, "original failing input", res.Original)
	}
	for _, f := range res.Failures {
		fmt.Fprintf(&b, "failure: %s\n", f)
	}
	if res.Panic != nil {
		fmt.Fprintf(&b, "panic: %s\n%s", res.Panic.Value, firstLines(res.Panic.Stack, maxStackLines))
	}
	switch {
	case res.Seed != 0:
		fmt.Fprintf(&b, "replay: property.WithSeed(%d), or -rapid.seed=%d on the command line", res.Seed, res.Seed)
		if res.FailFile != "" {
			fmt.Fprintf(&b, "; corpus file %s is replayed on every run", res.FailFile)
		}
		b.WriteByte('\n')
	case res.FailFile != "":
		fmt.Fprintf(&b, "replay: property.WithFailFile(%q), or -rapid.failfile=%q on the command line\n", res.FailFile, res.FailFile)
	}
	return b.String()
}

func writeDraws(b *strings.Builder, title string, draws []Drawn) {
	fmt.Fprintf(b, "%s:\n", title)
	if len(draws) == 0 {
		b.WriteString("  (no values were drawn)\n")
	}
	for _, d := range draws {
		fmt.Fprintf(b, "  %s = %s\n", d.Label, d.Value)
	}
}

func sameDraws(a, b []Drawn) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func firstLines(s string, n int) string {
	lines := strings.SplitAfter(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[:n], "") + "  ... (stack truncated; see Result.Panic.Stack)\n"
}
