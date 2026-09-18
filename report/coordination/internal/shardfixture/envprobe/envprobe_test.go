package envprobe

import (
	"os"
	"testing"

	"github.com/getsyntegrity/go-specs/report/coordination/internal/envread"
)

// TestEnvAccessProbe reads GO_SPECS_PROBE_VALUE through whichever access path
// GO_SPECS_PROBE_MODE selects, and does nothing else.
//
// The caller varies GO_SPECS_PROBE_VALUE between two runs and watches whether `go test` reports
// (cached). The mode variable is held constant across each pair, so only the probed value moves.
//
// Idle by default, so the repository's own `go test ./...` skips it.
func TestEnvAccessProbe(t *testing.T) {
	mode, ok := os.LookupEnv("GO_SPECS_PROBE_MODE")
	if !ok {
		t.Skip("probe idle: GO_SPECS_PROBE_MODE is not set")
	}

	switch mode {
	case "scan":
		v, _ := envread.Scan("GO_SPECS_PROBE_VALUE")
		t.Logf("scan read %d bytes", len(v))
	case "lookup":
		v, _ := envread.Lookup("GO_SPECS_PROBE_VALUE")
		t.Logf("lookup read %d bytes", len(v))
	default:
		t.Fatalf("unknown probe mode %q", mode)
	}
}
