package assert

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
)

// A handler's response is compared against a fixture. Key order and whitespace do not matter.
func ExampleMatchJSON() {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tags":["a","b"],"id":42,"owner":{"name":"ann"}}`))
	})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/items/42", nil))

	m := MatchJSON(`{
		"id": 42,
		"owner": {"name": "ann"},
		"tags": ["a", "b"]
	}`)
	fmt.Println(m.Match(rec.Body.Bytes()))
	// Output:
	// true
}

// A mismatch names each difference by JSON path.
func ExampleMatchJSON_failure() {
	rec := httptest.NewRecorder()
	_, _ = rec.WriteString(`{"id":42,"owner":{"name":"bob"},"tags":["b","a"]}`)

	m := MatchJSON(`{"id":42,"owner":{"name":"ann","email":"ann@example.com"},"tags":["a","b"]}`)
	fmt.Println(m.Match(rec.Body.String()))
	fmt.Println(strings.TrimSpace(m.FailureMessage(rec.Body.String())))
	// Output:
	// false
	// expected JSON documents to be equal
	// differences:
	//   $.owner.email: missing in actual, expected "ann@example.com"
	//   $.owner.name: expected "ann", got "bob"
	//   $.tags[0]: expected "a", got "b"
	//   $.tags[1]: expected "b", got "a"
}

// Numbers compare by exact value: 1 equals 1.0, and integers beyond 2^53 keep every digit.
func ExampleMatchJSON_numbers() {
	fmt.Println(MatchJSON(`{"n":1}`).Match(`{"n":1.0}`))
	fmt.Println(MatchJSON(`{"id":9007199254740993}`).Match(`{"id":9007199254740992}`))
	// Output:
	// true
	// false
}
