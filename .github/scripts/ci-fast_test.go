package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestUnitSelectionExcludesExternalHelpersAndKeepsPureTests(t *testing.T) {
	dir := t.TempDir()
	input := `package fixture
import ("testing"; "os/exec")
func TestPure(t *testing.T) { pure() }
func pure() {}
func TestExternal(t *testing.T) { launch() }
func launch() { exec.Command("false").Run() }
func TestIntegration(t *testing.T) {}
func TestShortSuite(t *testing.T) { if testing.Short() { t.Skip() } }
func TestMain(m *testing.M) { m.Run() }
`
	if err := os.WriteFile(filepath.Join(dir, "unit_test.go"), []byte(input), 0600); err != nil {
		t.Fatal(err)
	}
	got := tests(pkg{Dir: dir, ImportPath: "fixture", TestGoFiles: []string{"unit_test.go"}})
	if !slices.Equal(got, []string{"TestPure"}) {
		t.Fatalf("selected %v; want only TestPure", got)
	}
}

func TestUnsafeTestMainExcludesTheTestBinary(t *testing.T) {
	dir := t.TempDir()
	input := `package fixture
import ("testing"; "net")
func TestPure(t *testing.T) {}
func TestMain(m *testing.M) { net.Listen("tcp", ":0"); m.Run() }
`
	if err := os.WriteFile(filepath.Join(dir, "unit_test.go"), []byte(input), 0600); err != nil {
		t.Fatal(err)
	}
	if got := tests(pkg{Dir: dir, TestGoFiles: []string{"unit_test.go"}}); len(got) != 0 {
		t.Fatalf("unsafe TestMain selected %v", got)
	}
}

func TestIntegrationFileExcludedEvenWithoutShortSkip(t *testing.T) {
	dir := t.TempDir()
	input := `package fixture
import "testing"
func TestLooksPure(t *testing.T) {}
`
	if err := os.WriteFile(filepath.Join(dir, "api_integration_test.go"), []byte(input), 0600); err != nil {
		t.Fatal(err)
	}
	if got := tests(pkg{Dir: dir, TestGoFiles: []string{"api_integration_test.go"}}); len(got) != 0 {
		t.Fatalf("integration file selected %v", got)
	}
}

func TestUnsafePackageInitializerExcludesTheTestBinary(t *testing.T) {
	dir := t.TempDir()
	input := `package fixture
import ("testing"; "net")
var listener, _ = net.Listen("tcp", ":0")
func TestPure(t *testing.T) {}
`
	if err := os.WriteFile(filepath.Join(dir, "unit_test.go"), []byte(input), 0600); err != nil {
		t.Fatal(err)
	}
	if got := tests(pkg{Dir: dir, TestGoFiles: []string{"unit_test.go"}}); len(got) != 0 {
		t.Fatalf("unsafe initializer selected %v", got)
	}
}
