// Package examples is the usage reference for the go-specs property module.
//
// It lives inside the nested module github.com/getsyntegrity/go-specs/property, not next to the
// core examples, because property testing needs pgregory.net/rapid and the core module must never
// import it. Run these examples from this module: `cd property && go test ./examples/...`.
//
// property_test.go has one small example per situation: a minimal property, a property inside a
// spec, generators, Assume and Reject, shrinking, replay by seed and by fail file, a native fuzz
// target, panics and composition with the go-specs matchers. None of them fails on purpose except
// through property.Run, which returns the failure instead of failing the test.
//
// The index of every feature in go-specs lives in ../../examples/README.md.
package examples
