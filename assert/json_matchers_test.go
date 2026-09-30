package assert

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestMatchJSONEqualDocuments(t *testing.T) {
	cases := []struct {
		name, expected, actual string
	}{
		{"identical", `{"a":1}`, `{"a":1}`},
		{"reordered keys", `{"a":1,"b":{"x":true,"y":null}}`, `{"b":{"y":null,"x":true},"a":1}`},
		{"whitespace", `{"a":[1,2]}`, " {\n\t\"a\" : [ 1 ,\r\n 2 ]\n} \n"},
		{"scalar root", `"hi"`, ` "hi" `},
		{"null root", `null`, `null`},
		{"empty containers", `{"a":{},"b":[]}`, `{ "b": [ ], "a": { } }`},
		{"escaped string equals literal", `{"a":"A"}`, `{"a":"A"}`},
		{"one equals one point zero", `{"n":1}`, `{"n":1.0}`},
		{"exponent forms", `[100,0.5,-2]`, `[1e2,5e-1,-20e-1]`},
		{"negative zero", `0`, `-0.0`},
		{"huge exponent stays exact", `1e99999999999999999999`, `10e99999999999999999998`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := MatchJSON(tc.expected)
			if !m.Match(tc.actual) {
				t.Fatalf("expected match; message: %s", m.FailureMessage(tc.actual))
			}
			if !m.Match([]byte(tc.actual)) {
				t.Fatal("[]byte actual should match like string")
			}
		})
	}
}

func TestMatchJSONArrayOrderIsSignificant(t *testing.T) {
	m := MatchJSON(`{"a":[1,2,3]}`)
	actual := `{"a":[3,2,1]}`
	if m.Match(actual) {
		t.Fatal("reordered arrays must not match")
	}
	want := "differences:\n  $.a[0]: expected 1, got 3\n  $.a[2]: expected 3, got 1"
	if got := diffSection(m.FailureMessage(actual)); got != want {
		t.Fatalf("diff = %q, want %q", got, want)
	}
}

func TestMatchJSONPathDiagnostics(t *testing.T) {
	cases := []struct {
		name, expected, actual, want string
	}{
		{"changed value", `{"user":{"name":"ann"}}`, `{"user":{"name":"bob"}}`,
			"differences:\n  $.user.name: expected \"ann\", got \"bob\""},
		{"missing key", `{"a":1,"b":2}`, `{"a":1}`,
			"differences:\n  $.b: missing in actual, expected 2"},
		{"extra key", `{"a":1}`, `{"a":1,"b":[1]}`,
			"differences:\n  $.b: unexpected in actual, got [1]"},
		{"null versus missing", `{"a":null}`, `{}`,
			"differences:\n  $.a: missing in actual, expected null"},
		{"missing versus null", `{}`, `{"a":null}`,
			"differences:\n  $.a: unexpected in actual, got null"},
		{"null versus value", `{"a":null}`, `{"a":0}`,
			"differences:\n  $.a: expected null, got 0"},
		{"array shorter", `[1,2,3]`, `[1,2]`,
			"differences:\n  $[2]: missing in actual, expected 3"},
		{"array longer", `{"a":[1]}`, `{"a":[1,"x"]}`,
			"differences:\n  $.a[1]: unexpected in actual, got \"x\""},
		{"type mismatch", `{"a":[]}`, `{"a":{}}`,
			"differences:\n  $.a: expected [], got {}"},
		{"string vs number", `{"a":"1"}`, `{"a":1}`,
			"differences:\n  $.a: expected \"1\", got 1"},
		{"awkward key", `{"a b":{"c.d":1}}`, `{"a b":{"c.d":2}}`,
			"differences:\n  $[\"a b\"][\"c.d\"]: expected 1, got 2"},
		{"sorted keys", `{"z":1,"a":1}`, `{"z":2,"a":2}`,
			"differences:\n  $.a: expected 1, got 2\n  $.z: expected 1, got 2"},
		{"number display keeps source text", `{"n":1.50}`, `{"n":1.6}`,
			"differences:\n  $.n: expected 1.50, got 1.6"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := MatchJSON(tc.expected)
			if m.Match(tc.actual) {
				t.Fatal("expected mismatch")
			}
			if got := diffSection(m.FailureMessage(tc.actual)); got != tc.want {
				t.Fatalf("diff =\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

// diffSection returns the "differences:" part of a failure message.
func diffSection(msg string) string {
	_, rest, ok := strings.Cut(msg, "differences:")
	if !ok {
		return msg
	}
	return "differences:" + rest
}

func TestMatchJSONLargeNumbersKeepPrecision(t *testing.T) {
	// float64 cannot tell these apart: both round to 9007199254740992.
	m := MatchJSON(`{"id":9007199254740993}`)
	if m.Match(`{"id":9007199254740992}`) {
		t.Fatal("2^53+1 must differ from 2^53")
	}
	if !m.Match(`{"id":9007199254740993.0}`) {
		t.Fatal("same integer written with .0 must match")
	}
	if !MatchJSON(`123456789012345678901234567890.5`).Match(`1.234567890123456789012345678905e29`) {
		t.Fatal("arbitrary-precision decimals with equal value must match")
	}
	if MatchJSON(`0.1`).Match(`0.10000000000000001`) {
		t.Fatal("decimals beyond float64 precision must differ")
	}
	if MatchJSON(`1e400`).Match(`1e401`) {
		t.Fatal("out-of-float64-range numbers must still be compared exactly")
	}
}

func TestMatchJSONInvalidInput(t *testing.T) {
	m := MatchJSON(`{"a":1}`)
	for _, tc := range []struct{ name, actual, wantSub string }{
		{"empty", ``, "actual is empty"},
		{"blank", "  \n", "actual is empty"},
		{"syntax", `{"a":}`, "actual is not valid JSON"},
		{"truncated", `{"a":1`, "actual is not valid JSON"},
		{"single quotes", `{'a':1}`, "actual is not valid JSON"},
		{"trailing comma", `{"a":1,}`, "actual is not valid JSON"},
		{"NaN literal", `{"a":NaN}`, "actual is not valid JSON"},
		{"leading zero", `{"a":01}`, "actual is not valid JSON"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if m.Match(tc.actual) {
				t.Fatal("invalid JSON must not match")
			}
			if msg := m.FailureMessage(tc.actual); !strings.Contains(msg, tc.wantSub) {
				t.Fatalf("message %q lacks %q", msg, tc.wantSub)
			}
		})
	}
}

func TestMatchJSONInvalidExpected(t *testing.T) {
	m := MatchJSON(`{"a":`)
	if m.Match(`{"a":1}`) {
		t.Fatal("invalid expected never matches")
	}
	if msg := m.FailureMessage(`{"a":1}`); !strings.Contains(msg, "expected is not valid JSON") {
		t.Fatalf("message = %q", msg)
	}
	if d := m.(Describer).Description(); !strings.Contains(d, "invalid") {
		t.Fatalf("description = %q", d)
	}
}

func TestMatchJSONDuplicateKeysAreRejected(t *testing.T) {
	m := MatchJSON(`{"a":1}`)
	for _, actual := range []string{`{"a":1,"a":1}`, `{"a":1,"a":2}`, `{"x":{"y":[{"k":1,"k":1}]}}`} {
		if m.Match(actual) {
			t.Fatalf("%s must not match", actual)
		}
	}
	msg := m.FailureMessage(`{"x":{"y":[{"k":1,"k":2}]}}`)
	if !strings.Contains(msg, `duplicate key "k" at $.x.y[0]`) {
		t.Fatalf("message = %q", msg)
	}
	if !strings.Contains(MatchJSON(`{"a":1,"a":1}`).FailureMessage(`{"a":1}`), "expected is not valid JSON") {
		t.Fatal("duplicates in expected are rejected too")
	}
}

func TestMatchJSONTrailingContent(t *testing.T) {
	m := MatchJSON(`{"a":1}`)
	for _, actual := range []string{`{"a":1} {"a":1}`, `{"a":1}x`, `{"a":1}]`, `{"a":1} 2`, `1 2`} {
		if m.Match(actual) {
			t.Fatalf("%q must not match", actual)
		}
	}
	if msg := m.FailureMessage(`{"a":1}{"a":1}`); !strings.Contains(msg, "trailing content after the JSON document") {
		t.Fatalf("message = %q", msg)
	}
	if !m.Match("{\"a\":1}\n") {
		t.Fatal("trailing whitespace is insignificant")
	}
}

func TestMatchJSONActualTypes(t *testing.T) {
	m := MatchJSON(`{"a":1}`)
	if !m.Match(json.RawMessage(`{"a":1}`)) {
		t.Fatal("json.RawMessage should match")
	}
	type body string
	if !m.Match(body(`{"a":1}`)) {
		t.Fatal("named string should match")
	}
	if m.Match(42) || m.Match(nil) || m.Match(map[string]any{"a": 1}) {
		t.Fatal("non-text actual must not match")
	}
	if msg := m.FailureMessage(42); msg != "MatchJSON: int is not a string or []byte" {
		t.Fatalf("message = %q", msg)
	}
	if !MatchJSON([]byte(`{"a":1}`)).Match(`{"a":1}`) || !MatchJSON(json.RawMessage(`[]`)).Match(`[ ]`) {
		t.Fatal("expected may be []byte or json.RawMessage")
	}
	if msg := MatchJSON(42).FailureMessage(`1`); !strings.Contains(msg, "expected must be a string or []byte") {
		t.Fatalf("message = %q", msg)
	}
}

func TestMatchJSONNotComposes(t *testing.T) {
	if !Not(MatchJSON(`{"a":1}`)).Match(`{"a":2}`) {
		t.Fatal("Not should negate")
	}
	if d := MatchJSON(`{"a":1}`).(Describer).Description(); d == "" {
		t.Fatal("empty description")
	}
}

func TestMatchJSONFailureOutputIsBoundedAndDeterministic(t *testing.T) {
	var e, a strings.Builder
	e.WriteString("{")
	a.WriteString("{")
	for i := 0; i < 500; i++ {
		if i > 0 {
			e.WriteString(",")
			a.WriteString(",")
		}
		key := `"k` + strconv.Itoa(1000+i) + `"`
		e.WriteString(key + ":1")
		a.WriteString(key + ":2")
	}
	e.WriteString("}")
	a.WriteString("}")
	m := MatchJSON(e.String())
	first := m.FailureMessage(a.String())
	for i := 0; i < 5; i++ {
		if got := m.FailureMessage(a.String()); got != first {
			t.Fatal("message is not deterministic")
		}
	}
	if !strings.Contains(first, "... more differences not shown (limit 10)") {
		t.Fatalf("missing limit marker: %s", first)
	}
	if lines := strings.Count(first, "\n"); lines > 14 {
		t.Fatalf("message has %d lines", lines)
	}

	long := strings.Repeat("x", 10_000)
	msg := MatchJSON(`{"a":"` + long + `"}`).FailureMessage(`{"a":"y"}`)
	if len(msg) > 1000 {
		t.Fatalf("long value not cut: %d bytes", len(msg))
	}
	if !strings.Contains(msg, "...") {
		t.Fatalf("cut value should be marked: %s", msg)
	}

	longKey := strings.Repeat("k", 10_000)
	msg = MatchJSON(`{"` + longKey + `":1}`).FailureMessage(`{"` + longKey + `":2}`)
	if len(msg) > 1000 {
		t.Fatalf("long key not cut: %d bytes", len(msg))
	}

	deep := strings.Repeat("[", 200) + "1" + strings.Repeat("]", 200)
	deepOther := strings.Repeat("[", 200) + "2" + strings.Repeat("]", 200)
	if msg := MatchJSON(deep).FailureMessage(deepOther); len(msg) > 2000 {
		t.Fatalf("deep path not bounded: %d bytes", len(msg))
	}
	wide := "[" + strings.Repeat("[1],", 5000) + "[1]]"
	if msg := MatchJSON(wide).FailureMessage(`[[2]]`); len(msg) > 2000 {
		t.Fatalf("wide value not bounded: %d bytes", len(msg))
	}
}

func TestMatchJSONHTTPRecorderFixture(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"tags":["a","b"],"id":42,"owner":{"name":"ann"}}`))
	})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/items", strings.NewReader(`{}`)))
	if !MatchJSON(`{"id": 42, "owner": {"name": "ann"}, "tags": ["a", "b"]}`).Match(rec.Body.Bytes()) {
		t.Fatal("recorded body should match")
	}
}
