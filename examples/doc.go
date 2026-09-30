// Package examples is the usage reference for go-specs.
//
// Every public feature has one file in this directory, with several small examples that you can
// read, copy and run. There is no folder or file per individual case: open the file for the
// feature you care about (dsl_test.go, hooks_test.go, table_test.go, ...) and look for the
// example whose name matches your situation.
//
// The examples are ordinary Go tests and runnable Example functions, so `go test ./examples/...`
// keeps them honest. None of them fails on purpose, needs a network, or touches an external
// service. Runnable examples use the package-level form Example_<name>, which is what `go vet`
// accepts for a package that declares no matching identifier.
//
// The index of features, files and cases lives in README.md next to this file.
package examples
