package report

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseTarget(t *testing.T) {
	cases := []struct {
		in      string
		want    Target
		wantErr bool
	}{
		{"xml:artifacts/report.xml", Target{FormatXML, "artifacts/report.xml"}, false},
		{"html:out.html", Target{FormatHTML, "out.html"}, false},
		{"txt:out.txt", Target{FormatTXT, "out.txt"}, false},
		{"json:out.json", Target{FormatJSON, "out.json"}, false},
		{"bogus:out.txt", Target{}, true},
		{"noformat", Target{}, true},
		{"xml:", Target{}, true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParseTarget(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseTarget(%q): expected an error, got %+v", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseTarget(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("ParseTarget(%q) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}

func TestMultiFormatReporterDisabledByDefault(t *testing.T) {
	dir := t.TempDir()
	m := NewMultiFormat() // no targets
	m.SuiteStarted(SuiteStartEvent{Name: "S"})
	m.SuiteFinished(SuiteEndEvent{Name: "S"})
	if err := m.Flush(); err != nil {
		t.Fatalf("Flush with no targets: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("Flush with no targets wrote files: %v", entries)
	}
}

func TestMultiFormatReporterWritesAllRequestedTargets(t *testing.T) {
	dir := t.TempDir()
	targets := []Target{
		{FormatXML, filepath.Join(dir, "nested", "report.xml")},
		{FormatHTML, filepath.Join(dir, "report.html")},
		{FormatTXT, filepath.Join(dir, "report.txt")},
		{FormatJSON, filepath.Join(dir, "report.json")},
	}
	m := NewMultiFormat(targets...)
	m.SuiteStarted(SuiteStartEvent{Name: "S"})
	m.SpecFinished(SpecResultEvent{SpecStartEvent: SpecStartEvent{Name: "case"}})
	m.SuiteFinished(SuiteEndEvent{Name: "S"})

	if err := m.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	for _, tg := range targets {
		info, err := os.Stat(tg.Path)
		if err != nil {
			t.Fatalf("target %s was not written: %v", tg.Path, err)
		}
		if info.Size() == 0 {
			t.Fatalf("target %s is empty", tg.Path)
		}
	}
}

func TestMultiFormatReporterFailsExplicitlyOnWriteError(t *testing.T) {
	dir := t.TempDir()
	// A path whose parent is itself a file: os.MkdirAll must fail, and Flush must surface that
	// instead of silently dropping the target.
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewMultiFormat(Target{FormatTXT, filepath.Join(blocker, "report.txt")})
	m.SuiteStarted(SuiteStartEvent{Name: "S"})
	m.SuiteFinished(SuiteEndEvent{Name: "S"})

	if err := m.Flush(); err == nil {
		t.Fatal("expected Flush to return an error when a target's directory cannot be created, got nil")
	}
}
