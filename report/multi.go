package report

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Format identifies one renderer this package ships.
type Format string

const (
	FormatXML  Format = "xml"
	FormatHTML Format = "html"
	FormatTXT  Format = "txt"
	FormatJSON Format = "json"
)

// Target is one requested report output: a Format rendered to Path.
type Target struct {
	Format Format
	Path   string
}

// ParseTarget parses one "format:path" spelling, the shape issue #141 illustrates for
// `-go-specs.report=format:path` (e.g. "xml:artifacts/report.xml"). It only parses one
// already-resolved string, however the caller obtained it (a flag, an env var, direct
// construction) — wiring this into `go test`'s own flag parsing across concurrent per-package
// binaries, and merging their outputs, is deferred to a follow-up (see package doc in events.go
// and the multi-package note in this file's MultiFormatReporter doc).
func ParseTarget(s string) (Target, error) {
	format, path, ok := strings.Cut(s, ":")
	if !ok || path == "" {
		return Target{}, fmt.Errorf("go-specs report target %q: expected format:path", s)
	}
	f := Format(format)
	switch f {
	case FormatXML, FormatHTML, FormatTXT, FormatJSON:
	default:
		return Target{}, fmt.Errorf("go-specs report target %q: unknown format %q (want xml, html, txt, or json)", s, format)
	}
	return Target{Format: f, Path: path}, nil
}

// MultiFormatReporter is an EventReporter that renders the collected report to every requested
// Target once Flush is called. It is disabled by default: NewMultiFormat with no targets still
// collects events (Report() and Collector's other methods work normally) but Flush writes no
// files at all — reporting only activates for the targets the caller explicitly passed in.
//
// MultiFormatReporter renders one process's events. It does not itself coordinate multiple
// concurrent `go test` package binaries writing to the same paths — pass distinct paths per
// package (e.g. include the package name) until a shard-and-merge mechanism for `go test ./...`
// lands; see issue #141's "Multi-package constraint".
type MultiFormatReporter struct {
	*Collector
	targets []Target
}

// NewMultiFormat returns a MultiFormatReporter that renders to targets on Flush.
func NewMultiFormat(targets ...Target) *MultiFormatReporter {
	return &MultiFormatReporter{Collector: NewCollector(), targets: targets}
}

// Flush renders the collected report to every configured target, creating each target's parent
// directory if missing. It returns the first error encountered — including a render failure or a
// write/close failure — rather than continuing past it: a report file that silently failed to
// write is CI evidence lost without warning, so Flush never swallows that error.
func (m *MultiFormatReporter) Flush() error {
	if len(m.targets) == 0 {
		return nil
	}
	rep := m.Report()
	for _, t := range m.targets {
		if err := renderTarget(t, rep); err != nil {
			return err
		}
	}
	return nil
}

func renderTarget(t Target, rep NormalizedReport) error {
	if dir := filepath.Dir(t.Path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("go-specs report: create directory for %s: %w", t.Path, err)
		}
	}
	f, err := os.Create(t.Path)
	if err != nil {
		return fmt.Errorf("go-specs report: create %s: %w", t.Path, err)
	}

	renderErr := renderByFormat(t.Format, f, rep)
	closeErr := f.Close()
	if renderErr != nil {
		return fmt.Errorf("go-specs report: render %s as %s: %w", t.Path, t.Format, renderErr)
	}
	if closeErr != nil {
		return fmt.Errorf("go-specs report: write %s: %w", t.Path, closeErr)
	}
	return nil
}

func renderByFormat(format Format, w io.Writer, rep NormalizedReport) error {
	switch format {
	case FormatXML:
		return RenderXML(w, rep)
	case FormatHTML:
		return RenderHTML(w, rep)
	case FormatTXT:
		return RenderTXT(w, rep)
	case FormatJSON:
		return RenderJSON(w, rep)
	default:
		return fmt.Errorf("unknown format %q", format)
	}
}

var _ EventReporter = (*MultiFormatReporter)(nil)
