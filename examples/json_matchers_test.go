// json_matchers_test.go shows assert.MatchJSON (also specs.MatchJSON): compare a JSON document with
// an expected one by meaning, not by bytes.
//
// Use it for HTTP response bodies, messages and generated files, where key order and whitespace
// are noise and a byte-for-byte comparison would fail for the wrong reasons.
//
// Semantics:
//   - Both sides are text: a string, a []byte (json.RawMessage included) or a named type of either.
//     To compare a Go value, marshal it first (json.Marshal) and pass the bytes.
//   - Equality is strict and structural. Object key order and whitespace are ignored. Array order
//     matters. A missing key differs from a key whose value is null. There is no partial matching.
//   - Numbers compare by exact decimal value, never through float64: 1, 1.0, 1e0 and 10e-1 are
//     equal, -0 equals 0, and 9007199254740993 differs from 9007199254740992.
//   - Text that is not exactly one JSON document never matches: empty or blank, a syntax error,
//     content after the document ("{} {}"), or an object that repeats a key. The same goes for an
//     invalid expected document, and the failure message says which side is broken.
//   - A failure lists up to ten differences as JSON paths ($.owner.name), in a deterministic
//     order, with the expected and actual value at each.
package examples_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/getsyntegrity/go-specs/assert"
	"github.com/getsyntegrity/go-specs/specs"
)

func TestJSON_inASpec(t *testing.T) {
	specs.Describe(t, "MatchJSON", func(s *specs.Spec) {
		s.It("ignores key order and whitespace", func(ctx *specs.Context) {
			body := []byte(`{"name":"Ann","id":7}`)
			ctx.Expect(body).To(specs.MatchJSON(`{ "id": 7, "name": "Ann" }`))
		})
	})
}

func Example_matchJSON() {
	expected := assert.MatchJSON(`{"id": 42, "owner": {"name": "ann"}, "tags": ["a", "b"]}`)
	// Key order and whitespace do not matter.
	fmt.Println(expected.Match(`{"tags":["a","b"],"owner":{"name":"ann"},"id":42}`))
	// Array order does.
	fmt.Println(expected.Match(`{"id":42,"owner":{"name":"ann"},"tags":["b","a"]}`))
	// Output:
	// true
	// false
}

// The inputs can be a string, a []byte or a json.RawMessage, and the two sides need not be the same kind.
func Example_matchJSONInputKinds() {
	m := assert.MatchJSON([]byte(`{"ok":true}`))
	fmt.Println(m.Match(`{"ok": true}`))
	fmt.Println(m.Match([]byte(`{"ok":true}`)))
	fmt.Println(m.Match(json.RawMessage(`{"ok": true }`)))
	// A Go value is marshalled by the test first.
	body, _ := json.Marshal(map[string]any{"ok": true})
	fmt.Println(m.Match(body))
	// Not text: a struct is not JSON until it is marshalled.
	fmt.Println(m.Match(struct{ OK bool }{true}))
	// Output:
	// true
	// true
	// true
	// true
	// false
}

func Example_matchJSONNumbers() {
	fmt.Println(assert.MatchJSON(`{"n":1}`).Match(`{"n":1.0}`))
	fmt.Println(assert.MatchJSON(`{"n":1}`).Match(`{"n":1e0}`))
	// Large integers keep every digit: these differ, though float64 would call them equal.
	fmt.Println(assert.MatchJSON(`{"id":9007199254740993}`).Match(`{"id":9007199254740992}`))
	// Null is a value; a missing key is not.
	fmt.Println(assert.MatchJSON(`{"a":null}`).Match(`{}`))
	// Output:
	// true
	// true
	// false
	// false
}

// A mismatch names each difference by JSON path.
func Example_matchJSONPathDiagnostics() {
	m := assert.MatchJSON(`{"id":42,"owner":{"name":"ann","email":"ann@example.com"},"tags":["a","b"]}`)
	actual := `{"id":42,"owner":{"name":"bob"},"tags":["b","a"]}`
	_, failure := assert.Evaluate(m, actual)
	fmt.Println(failure)
	// Output:
	// expected JSON documents to be equal
	// differences:
	//   $.owner.email: missing in actual, expected "ann@example.com"
	//   $.owner.name: expected "ann", got "bob"
	//   $.tags[0]: expected "a", got "b"
	//   $.tags[1]: expected "b", got "a"
}

// Documents that are not exactly one JSON value never match; the message says which side is broken.
func Example_matchJSONInvalidInput() {
	m := assert.MatchJSON(`{"a":1}`)
	for _, actual := range []any{"", `{"a":1`, `{"a":1} {"a":1}`, `{"a":1,"a":1}`, 42} {
		matched, failure := assert.Evaluate(m, actual)
		fmt.Println(matched, failure)
	}
	// An invalid expected document is reported by the matcher, like an invalid MatchRegex pattern.
	_, failure := assert.Evaluate(assert.MatchJSON(`{oops}`), `{}`)
	fmt.Println(failure)
	_, failure = assert.Evaluate(assert.MatchJSON(42), `{}`)
	fmt.Println(failure)
	// Output:
	// false MatchJSON: actual is empty: no JSON document
	// false MatchJSON: actual is not valid JSON: unexpected end of JSON input
	// false MatchJSON: actual is not valid JSON: trailing content after the JSON document
	// false MatchJSON: actual is not valid JSON: duplicate key "a" at $
	// false MatchJSON: int is not a string or []byte
	// MatchJSON: expected is not valid JSON: invalid character 'o' looking for beginning of value
	// MatchJSON: expected must be a string or []byte, got int
}
