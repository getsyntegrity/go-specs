package main

import "testing"

// usage_test.go pins T3 of issue #146: stdlib `flag` usage-error conventions (contract v1.2.9 §7:
// "the only binary precedent in the repo is tools/perfcheck, so it adds no new dependency").

func TestUsageWithNoArgumentsExitsTwo(t *testing.T) {
	_, stderr, code := runCLI(t, nil, nil)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if stderr == "" {
		t.Fatal("no usage was printed on stderr")
	}
}

func TestUsageWithAnUnknownVerbExitsTwo(t *testing.T) {
	_, stderr, code := runCLI(t, []string{"bogus"}, nil)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if stderr == "" {
		t.Fatal("no usage was printed on stderr")
	}
}

func TestUsageWithAnUnknownFlagExitsTwo(t *testing.T) {
	_, stderr, code := runCLI(t, []string{"init", "-not-a-real-flag"}, nil)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if stderr == "" {
		t.Fatal("no usage was printed on stderr")
	}
}

func TestUsageWithUnexpectedPositionalArgumentsExitsTwo(t *testing.T) {
	base := secureTempDir(t)
	_, stderr, code := runCLI(t, []string{"init", "-run-id", "run-1", "-token", string(validToken), "-report-dir", base, "extra-positional-arg"}, nil)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if stderr == "" {
		t.Fatal("no usage was printed on stderr")
	}
}
