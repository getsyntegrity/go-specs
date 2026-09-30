package specs

import (
	"fmt"
	"slices"
	"strings"
)

// Table registers one spec per row, the typed equivalent of
//
//	for _, row := range rows {
//		s.It(name(row), func(ctx *Context) { body(ctx, row) })
//	}
//
// It is a convenience over that loop, not a second execution engine: every row becomes an ordinary
// It, so it is its own Go subtest (selectable with `go test -run`), is wrapped by the enclosing
// BeforeEach/AfterEach hooks, is reported as its own spec, and fails on its own. Plain `for` plus
// It stays fully supported.
//
// Table adds no hierarchy segment: a row named "negative" inside Describe("add") is reported as
// add/negative. Wrap the call in s.When to group rows under a segment of your choosing.
//
// Naming rules, checked for every row before any row is registered (a rejected table registers
// nothing, and the panic names the offending rows by index):
//   - name(row) must not be empty.
//   - names must be unique. A duplicate would get a "#01" suffix from go test and make -run
//     selection ambiguous, so it is rejected instead. Names that differ only by whitespace versus
//     underscore are duplicates too, because go test rewrites spaces to underscores.
//   - name and body must not be nil, and s must come from an entry point (Describe, When, ...).
//
// Ownership: rows is copied when Table is called, so later changes to the slice do not affect the
// registered specs, and each body call receives its own copy of the row value. Pointers, slices
// and maps inside a row are shared, as with any Go value copy. name runs once per row at
// registration time, never while specs execute.
func Table[T any](s *Spec, rows []T, name func(T) string, body func(*Context, T)) {
	registerTable("Table", s, rows, name, body, (*Spec).It)
}

// TableParallel is Table for rows that may run concurrently: each row is registered with
// ItParallel, so consecutive rows form one parallel group with the semantics documented on
// ItParallel (own *Context and subtest per row, ctx.T is the row's subtest, hooks still wrap each
// row, rows are dropped under focus). Naming and ownership rules are identical to Table. Because
// rows run at the same time, body must not write to state shared between rows without its own
// synchronization; the row value itself is private to its call.
func TableParallel[T any](s *Spec, rows []T, name func(T) string, body func(*Context, T)) {
	registerTable("TableParallel", s, rows, name, body, (*Spec).ItParallel)
}

func registerTable[T any](fn string, s *Spec, rows []T, name func(T) string, body func(*Context, T), register func(*Spec, string, func(*Context))) {
	if s == nil {
		panic(fmt.Sprintf("specs.%s: nil *Spec; obtain a Spec from Describe, When or another entry point", fn))
	}
	if name == nil {
		panic(fmt.Sprintf("specs.%s: nil name function", fn))
	}
	if body == nil {
		panic(fmt.Sprintf("specs.%s: nil body function", fn))
	}
	rows = slices.Clone(rows)
	names := make([]string, len(rows))
	firstByKey := make(map[string]int, len(rows))
	for i, row := range rows {
		n := name(row)
		if n == "" {
			panic(fmt.Sprintf("specs.%s: row %d has an empty name", fn, i))
		}
		key := strings.Map(rewriteSubtestSpace, n)
		if first, dup := firstByKey[key]; dup {
			panic(fmt.Sprintf("specs.%s: row %d name %q duplicates row %d name %q; row names must be unique", fn, i, n, first, names[first]))
		}
		firstByKey[key] = i
		names[i] = n
	}
	for i := range rows {
		row := rows[i]
		register(s, names[i], func(ctx *Context) { body(ctx, row) })
	}
}

// rewriteSubtestSpace mirrors how go test rewrites whitespace in a subtest name.
func rewriteSubtestSpace(r rune) rune {
	switch r {
	case ' ', '\t', '\n', '\v', '\f', '\r':
		return '_'
	}
	return r
}
