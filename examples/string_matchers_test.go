// string_matchers_test.go shows the text matchers: StartWith, EndWith and MatchRegex.
//
// Use them to check prefixes, suffixes and patterns in log lines, URLs, identifiers and messages
// without hand-writing strings.HasPrefix and a boolean. For a substring, use Contain (see
// collection_matchers_test.go).
//
// Semantics:
//   - The actual can be a string, a []byte or any named type whose kind is string. Anything else
//     (an int, nil) never matches, and the failure message says the value is not text.
//   - An empty prefix or suffix matches any text.
//   - MatchRegex uses Go's RE2 syntax and is unanchored: it passes when the pattern matches
//     anywhere. Add ^ and $ to anchor it.
//   - An invalid pattern never panics. The matcher never matches and its failure message carries
//     the compile error, so the mistake shows up at the assertion that used it.
package examples_test

import (
	"fmt"

	"github.com/getsyntegrity/go-specs/assert"
)

type stringMatcherID string

func Example_startWithEndWith() {
	url := "https://example.com/api/v1/users"
	fmt.Println(assert.StartWith("https://").Match(url))
	fmt.Println(assert.EndWith("/users").Match(url))
	fmt.Println(assert.StartWith("http://").Match(url))
	_, failure := assert.Evaluate(assert.StartWith("http://"), url)
	fmt.Println(failure)
	_, failure = assert.Evaluate(assert.EndWith("/orders"), url)
	fmt.Println(failure)
	// Output:
	// true
	// true
	// false
	// expected "https://example.com/api/v1/users" to start with "http://"
	// expected "https://example.com/api/v1/users" to end with "/orders"
}

// []byte and named string types work as text; empty affixes match anything.
func Example_textKinds() {
	fmt.Println(assert.StartWith("ord_").Match([]byte("ord_123")))
	fmt.Println(assert.EndWith("123").Match(stringMatcherID("ord_123")))
	fmt.Println(assert.StartWith("").Match(""))
	// Not text: a clear message instead of a bogus mismatch.
	_, failure := assert.Evaluate(assert.StartWith("1"), 123)
	fmt.Println(failure)
	// Output:
	// true
	// true
	// true
	// StartWith: int is not a string or []byte
}

func Example_matchRegex() {
	fmt.Println(assert.MatchRegex(`^\d{4}-\d{2}-\d{2}$`).Match("2026-09-30"))
	// Unanchored: matches anywhere in the text.
	fmt.Println(assert.MatchRegex(`\d+`).Match("order 42 shipped"))
	// Anchoring makes it strict.
	fmt.Println(assert.MatchRegex(`^\d+$`).Match("order 42 shipped"))
	_, failure := assert.Evaluate(assert.MatchRegex(`^\d+$`), "order 42 shipped")
	fmt.Println(failure)
	// Output:
	// true
	// true
	// false
	// expected "order 42 shipped" to match regex "^\\d+$"
}

// A broken pattern fails the assertion with the compile error; it does not panic.
func Example_matchRegexInvalidPattern() {
	m := assert.MatchRegex(`([a-z`)
	fmt.Println(m.Match("abc"))
	fmt.Println(m.FailureMessage("abc"))
	// Output:
	// false
	// MatchRegex: invalid pattern "([a-z": error parsing regexp: missing closing ]: `[a-z`
}
