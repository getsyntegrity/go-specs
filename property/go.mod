module github.com/getsyntegrity/go-specs/property

go 1.27.0

require (
	github.com/getsyntegrity/go-specs v0.0.0
	pgregory.net/rapid v1.3.0
)

// The nested module is developed against the parent checkout. A release drops this line and
// requires a tagged core version instead; see docs/PROPERTY_TESTING.md.
replace github.com/getsyntegrity/go-specs => ../
