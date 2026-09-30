# Semantic JSON equality: `MatchJSON`

`MatchJSON(expected)` checks that a JSON document (a response body, a request payload, a fixture) means the same thing as another, regardless of how it was formatted. It lives in `assert/json_matchers.go` and is re-exported as `specs.MatchJSON`.

```go
rec := httptest.NewRecorder()
handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/items/42", nil))

ctx.Expect(rec.Body.Bytes()).To(specs.MatchJSON(`{"id": 42, "tags": ["a", "b"]}`))
```

Both sides are text: a `string`, a `[]byte` (so `rec.Body.Bytes()` and `json.RawMessage` work) or a named type of either. Any other actual never matches, and the failure says `MatchJSON: int is not a string or []byte`. As with `MatchRegex`, an invalid `expected` does not panic: the matcher never matches and its failure message says `expected is not valid JSON`.

## What counts as equal

| Aspect | Rule |
| --- | --- |
| Object key order | Ignored. |
| Whitespace between tokens | Ignored. |
| Array order | Significant. `[1,2]` is not `[2,1]`. |
| `null` versus a missing key | Different. `{"a":null}` does not equal `{}`. |
| Strings | Compared by decoded value: `"A"` equals `"A"`. |
| Numbers | Compared by exact decimal value, see below. |
| Extra or missing keys | Both are differences. There is no subset or partial matching. |

### Numbers

Numbers are never converted to `float64`. The matcher reads them with `json.Decoder.UseNumber`, then reduces each literal to a canonical decimal (sign, significant digits, exponent), so any number of digits and any exponent size is compared exactly:

- `1`, `1.0`, `1e0`, `10e-1` and `0.1e1` are **equal**. The matcher compares values, not spellings.
- `-0`, `0` and `0.0e5` are equal.
- `9007199254740993` and `9007199254740992` are **different**, even though both round to the same `float64`.
- `0.1` and `0.10000000000000001` are different.
- `1e400` and `1e401` are different; values outside the `float64` range are fine.

If you need `1` and `1.0` to differ (a schema that distinguishes integers from floats), compare the raw text instead.

### Invalid input, duplicate keys, trailing content

The input must be exactly one JSON document. Each of these never matches, and the failure message names the reason:

- **Empty or blank text:** `actual is empty: no JSON document`.
- **A syntax error** (single quotes, trailing comma, `NaN`, leading zeros, truncation): `actual is not valid JSON: ...`.
- **Trailing content** after the document, such as `{} {}` or `{}x`: `trailing content after the JSON document`. Trailing whitespace is fine.
- **A duplicate object key at any depth**: `duplicate key "k" at $.x.y[0]`. RFC 8259 leaves duplicate keys undefined, so different parsers keep the first or the last; the matcher refuses to pick a winner rather than hide a disagreement.

The same rules apply to `expected`; the message then says `expected is not valid JSON`.

## Failure output

```
expected JSON documents to be equal
differences:
  $.owner.email: missing in actual, expected "ann@example.com"
  $.owner.name: expected "ann", got "bob"
  $.tags[0]: expected "a", got "b"
  $.tags[1]: expected "b", got "a"
```

Each line is a JSON path (`$` is the document root; keys that are not plain identifiers print as `["a b"]`) and what differs there: `expected X, got Y`, `missing in actual, expected X` or `unexpected in actual, got X`. The output is deterministic and bounded, using the same limits as the structural diff of `Equal`:

- at most 10 differences, then `... more differences not shown (limit 10)`;
- object keys are visited in sorted order, arrays by index;
- each value is cut to 80 characters and marked `...`; a path keeps its last 160 bytes, marked with a leading `...`;
- numbers print as written in the input, so `1.50` stays `1.50` even though it equals `1.5`.

## How it differs from the alternatives

- **Snapshots** (`snapshots` package) store a serialized value on disk and compare it to a later run, including formatting. `MatchJSON` compares two documents in memory and ignores formatting; it never reads or writes a snapshot file, and snapshot semantics are unchanged.
- **String equality** (`Equal("...")`) fails on key order, indentation and a trailing newline, and its diff is a pair of long strings rather than a list of paths.
- **Go struct equality** (`Equal(want)` after `json.Unmarshal`) depends on the target types: unknown fields are dropped, `null` and a missing key both become the zero value, and numbers go through `float64` or a fixed integer type. `MatchJSON` works on the document itself, so none of those differences are hidden.
