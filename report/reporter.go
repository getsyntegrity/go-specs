package report

import (
	"fmt"
	"io"
	"sync"
)

// Reporter implements EventReporter and can write events to an io.Writer.
type Reporter struct {
	mu   sync.Mutex
	w    io.Writer
	path []string
}

// New returns a Reporter that emits events to w.
func New(w io.Writer) *Reporter {
	if w == nil {
		return &Reporter{}
	}
	return &Reporter{w: w}
}

// SuiteStarted implements EventReporter.
func (r *Reporter) SuiteStarted(e SuiteStartEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.path = []string{e.Name}
	if r.w != nil {
		_, _ = fmt.Fprintf(r.w, "SuiteStarted %s\n", e.Name)
	}
}

// SuiteFinished implements EventReporter.
func (r *Reporter) SuiteFinished(e SuiteEndEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.w != nil {
		_, _ = fmt.Fprintf(r.w, "SuiteFinished %s duration=%s skipped=%d\n", e.Name, e.Duration, e.SkippedSpecs)
	}
	r.path = nil
}

// SpecStarted implements EventReporter.
func (r *Reporter) SpecStarted(e SpecStartEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.w != nil {
		_, _ = fmt.Fprintf(r.w, "SpecStarted %s %v\n", e.Name, e.Path)
	}
}

// SpecFinished implements EventReporter.
func (r *Reporter) SpecFinished(e SpecResultEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.w != nil {
		_, _ = fmt.Fprintf(r.w, "SpecFinished %s %v failed=%v skipped=%v duration=%s\n", e.Name, e.Path, e.Failed, e.Skipped, e.Duration)
	}
}

var _ EventReporter = (*Reporter)(nil)
