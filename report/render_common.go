package report

import (
	"fmt"
	"time"
)

// formatSeconds renders a duration the way every renderer's time fields agree on: fixed
// three-decimal seconds, so JUnit consumers and the other formats never disagree on precision.
func formatSeconds(d time.Duration) string {
	return fmt.Sprintf("%.3f", d.Seconds())
}

// formatPercentage renders a coverage percentage to two decimal places, consistently across
// every renderer.
func formatPercentage(p float64) string {
	return fmt.Sprintf("%.2f", p)
}
