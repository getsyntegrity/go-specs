// go-specs-report is a thin CLI over report/coordination, following the only binary precedent in
// this repo (tools/perfcheck): stdlib flag, one FlagSet per verb, no new dependency.
//
// It owns nothing normative. Ownership, merge and render logic all live in report/coordination
// (contract v1.2.9 §7); this package only parses flags and environment variables, calls the
// library, and prints results.
//
// Usage:
//
//	go-specs-report init     -run-id=<id> [-token=<tok>] [-report-dir=<dir>] [-force]
//	go-specs-report finalize -run-id=<id> -token=<tok> -report-dir=<dir> -producers=<file> [...]
//	go-specs-report gc       [-report-dir=<dir>] [-retention=24h] [-dry-run]
package main

import "os"

func main() {
	os.Exit(run(os.Args[1:], os.LookupEnv, os.Stdout, os.Stderr))
}
