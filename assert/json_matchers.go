package assert

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// MatchJSON returns a matcher that expects actual to be a JSON document semantically equal to
// expected. Both are text: a string, a []byte (json.RawMessage included) or a named type of either.
//
// Equality is strict and structural. Object key order and whitespace between tokens are ignored;
// array order is significant; a missing key is different from a key whose value is null. There is no
// subset or partial matching.
//
// Numbers are compared by exact decimal value, never through float64: 1, 1.0, 1e0 and 10e-1 are
// equal, -0 equals 0, and 9007199254740993 differs from 9007199254740992. Digits and exponent are
// unbounded. Strings compare by decoded value, so "A" equals "A".
//
// Input that is not exactly one JSON document never matches: empty or blank text, a syntax error,
// content after the document ("{} {}"), and an object that repeats a key at any depth (RFC 8259 leaves
// duplicates undefined, so the matcher refuses to pick a winner). The same applies to expected: an
// invalid expected never matches and its FailureMessage says so, like MatchRegex with a bad pattern.
//
// The failure message lists up to ten differences as JSON paths ($.user.tags[1]) with the expected
// and actual value at each, in a deterministic order (object keys sorted), with every value and path
// cut to a bounded length.
func MatchJSON(expected any) Matcher {
	m := &matchJSONMatcher{}
	text, ok := jsonText(expected)
	if !ok {
		m.expectedErr = fmt.Errorf("expected must be a string or []byte, got %T", expected)
		return m
	}
	m.expected, m.expectedErr = parseJSONDocument("expected", text)
	return m
}

type matchJSONMatcher struct {
	expected    *jsonNode
	expectedErr error
}

func (m *matchJSONMatcher) Match(actual any) bool {
	if m.expectedErr != nil {
		return false
	}
	text, ok := jsonText(actual)
	if !ok {
		return false
	}
	node, err := parseJSONDocument("actual", text)
	if err != nil {
		return false
	}
	return jsonEqual(m.expected, node)
}

func (m *matchJSONMatcher) FailureMessage(actual any) string {
	if m.expectedErr != nil {
		return "MatchJSON: " + m.expectedErr.Error()
	}
	text, ok := jsonText(actual)
	if !ok {
		return notTextMessage("MatchJSON", actual)
	}
	node, err := parseJSONDocument("actual", text)
	if err != nil {
		return "MatchJSON: " + err.Error()
	}
	w := &jsonDiffWalker{limit: diffMaxEntries}
	w.walk(m.expected, node, &jsonPath{})
	if len(w.entries) == 0 {
		return "MatchJSON: documents are equal"
	}
	var b strings.Builder
	b.WriteString("expected JSON documents to be equal\ndifferences:")
	for _, e := range w.entries[:min(len(w.entries), diffMaxEntries)] {
		fmt.Fprintf(&b, "\n  %s: %s", e.path, e.detail)
	}
	if len(w.entries) > diffMaxEntries {
		fmt.Fprintf(&b, "\n  ... more differences not shown (limit %d)", diffMaxEntries)
	}
	return b.String()
}

// Description implements Describer; see equalMatcher.Description.
func (m *matchJSONMatcher) Description() string {
	if m.expectedErr != nil {
		return "equal to JSON (invalid expected)"
	}
	return "equal to JSON " + renderJSONNode(m.expected)
}

// jsonText returns v as bytes when it is text (string, []byte or a named kind of either).
func jsonText(v any) ([]byte, bool) {
	switch t := v.(type) {
	case string:
		return []byte(t), true
	case []byte:
		return t, true
	}
	if v == nil {
		return nil, false
	}
	if s, ok := namedString(v); ok {
		return []byte(s), true
	}
	if b, ok := namedBytes(v); ok {
		return b, true
	}
	return nil, false
}

type jsonKind uint8

const (
	jsonNull jsonKind = iota
	jsonBool
	jsonNumber
	jsonString
	jsonArray
	jsonObject
)

// jsonNode is a parsed JSON value. Numbers keep their source text for display and a canonical form
// for comparison.
type jsonNode struct {
	kind   jsonKind
	b      bool
	str    string // jsonString: the decoded value; jsonNumber: the source text
	canon  string // jsonNumber: canonical decimal form
	arr    []*jsonNode
	obj    map[string]*jsonNode
	sorted []string // jsonObject: keys in ascending order
}

// jsonPath is one segment of a path, linked to its parent so extending it costs one node.
type jsonPath struct {
	parent *jsonPath
	key    string
	idx    int
	kind   uint8 // 0 root, 1 key, 2 index
}

func (p *jsonPath) field(k string) *jsonPath { return &jsonPath{parent: p, key: k, kind: 1} }
func (p *jsonPath) index(i int) *jsonPath    { return &jsonPath{parent: p, idx: i, kind: 2} }

const (
	jsonMaxPathBytes = 160
	jsonRenderCap    = 4 * diffMaxValueRunes // bytes written before a render stops
)

// String renders the path as $.a[2]["odd key"], keeping at most the last jsonMaxPathBytes of it and
// marking the cut with a leading "...". It never walks more segments than it can show.
func (p *jsonPath) String() string {
	var segs []string
	size := 0
	cut := false
	for s := p; s != nil && s.kind != 0; s = s.parent {
		seg := s.segment()
		if size+len(seg) > jsonMaxPathBytes {
			cut = true
			break
		}
		segs = append(segs, seg)
		size += len(seg)
	}
	var b strings.Builder
	if cut {
		b.WriteString("...")
	} else {
		b.WriteString("$")
	}
	for i := len(segs) - 1; i >= 0; i-- {
		b.WriteString(segs[i])
	}
	return b.String()
}

func (s *jsonPath) segment() string {
	if s.kind == 2 {
		return "[" + strconv.Itoa(s.idx) + "]"
	}
	key, cut := s.key, false
	if len(key) > diffMaxValueRunes {
		if c := cutRunes(key, diffMaxValueRunes); len(c) < len(key) {
			key, cut = c, true
		}
	}
	if !cut && isJSONIdent(key) {
		return "." + key
	}
	q := strconv.Quote(key)
	if cut {
		q += "..."
	}
	return "[" + q + "]"
}

func isJSONIdent(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || i > 0 && c >= '0' && c <= '9' {
			continue
		}
		return false
	}
	return true
}

// parseJSONDocument parses text as exactly one JSON document. role ("expected" or "actual") prefixes
// the error so the message says which side is broken.
func parseJSONDocument(role string, text []byte) (*jsonNode, error) {
	if len(bytes.TrimSpace(text)) == 0 {
		return nil, fmt.Errorf("%s is empty: no JSON document", role)
	}
	dec := json.NewDecoder(bytes.NewReader(text))
	dec.UseNumber()
	node, err := parseJSONValue(dec, &jsonPath{})
	if err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %w", role, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s is not valid JSON: trailing content after the JSON document", role)
	}
	return node, nil
}

func parseJSONValue(dec *json.Decoder, path *jsonPath) (*jsonNode, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, jsonSyntaxError(err)
	}
	switch t := tok.(type) {
	case nil:
		return &jsonNode{kind: jsonNull}, nil
	case bool:
		return &jsonNode{kind: jsonBool, b: t}, nil
	case string:
		return &jsonNode{kind: jsonString, str: t}, nil
	case json.Number:
		return &jsonNode{kind: jsonNumber, str: string(t), canon: canonicalJSONNumber(string(t))}, nil
	case json.Delim:
		if t == '[' {
			n := &jsonNode{kind: jsonArray}
			for i := 0; dec.More(); i++ {
				el, err := parseJSONValue(dec, path.index(i))
				if err != nil {
					return nil, err
				}
				n.arr = append(n.arr, el)
			}
			if _, err := dec.Token(); err != nil {
				return nil, jsonSyntaxError(err)
			}
			return n, nil
		}
		n := &jsonNode{kind: jsonObject, obj: map[string]*jsonNode{}}
		for dec.More() {
			kt, err := dec.Token()
			if err != nil {
				return nil, jsonSyntaxError(err)
			}
			key, _ := kt.(string)
			if _, dup := n.obj[key]; dup {
				return nil, fmt.Errorf("duplicate key %s at %s", strconv.Quote(cutRunes(key, diffMaxValueRunes)), path)
			}
			val, err := parseJSONValue(dec, path.field(key))
			if err != nil {
				return nil, err
			}
			n.obj[key] = val
			n.sorted = append(n.sorted, key)
		}
		if _, err := dec.Token(); err != nil {
			return nil, jsonSyntaxError(err)
		}
		sort.Strings(n.sorted)
		return n, nil
	}
	return nil, errors.New("unexpected token")
}

func jsonSyntaxError(err error) error {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return errors.New("unexpected end of JSON input")
	}
	return err
}

// canonicalJSONNumber maps a valid JSON number literal to sign + significant digits + "e" + exponent,
// so equal values have equal text whatever their spelling. Zero, however written, is "0".
func canonicalJSONNumber(lit string) string {
	neg := strings.HasPrefix(lit, "-")
	lit = strings.TrimPrefix(lit, "-")
	mant, expText, _ := strings.Cut(strings.ToLower(lit), "e")
	intPart, frac, _ := strings.Cut(mant, ".")
	digits := strings.TrimLeft(intPart+frac, "0")
	if digits == "" {
		return "0"
	}
	exp := new(big.Int)
	if expText != "" {
		exp.SetString(strings.TrimPrefix(expText, "+"), 10)
	}
	exp.Sub(exp, big.NewInt(int64(len(frac))))
	trimmed := strings.TrimRight(digits, "0")
	exp.Add(exp, big.NewInt(int64(len(digits)-len(trimmed))))
	sign := ""
	if neg {
		sign = "-"
	}
	return sign + trimmed + "e" + exp.String()
}

func jsonEqual(a, b *jsonNode) bool {
	if a.kind != b.kind {
		return false
	}
	switch a.kind {
	case jsonBool:
		return a.b == b.b
	case jsonString:
		return a.str == b.str
	case jsonNumber:
		return a.canon == b.canon
	case jsonArray:
		if len(a.arr) != len(b.arr) {
			return false
		}
		for i := range a.arr {
			if !jsonEqual(a.arr[i], b.arr[i]) {
				return false
			}
		}
	case jsonObject:
		if len(a.obj) != len(b.obj) {
			return false
		}
		for k, av := range a.obj {
			bv, ok := b.obj[k]
			if !ok || !jsonEqual(av, bv) {
				return false
			}
		}
	}
	return true
}

type jsonDiffEntry struct{ path, detail string }

// jsonDiffWalker collects differences the way the structural diff of Equal does: at most limit+1
// entries (the extra one only proves there are more), then it stops walking.
type jsonDiffWalker struct {
	entries []jsonDiffEntry
	limit   int
}

func (w *jsonDiffWalker) full() bool { return len(w.entries) > w.limit }

func (w *jsonDiffWalker) add(path *jsonPath, detail string) {
	if !w.full() {
		w.entries = append(w.entries, jsonDiffEntry{path.String(), detail})
	}
}

func (w *jsonDiffWalker) walk(e, a *jsonNode, path *jsonPath) {
	if w.full() {
		return
	}
	if e.kind != a.kind || (e.kind != jsonArray && e.kind != jsonObject) {
		if !jsonEqual(e, a) {
			w.add(path, fmt.Sprintf("expected %s, got %s", renderJSONNode(e), renderJSONNode(a)))
		}
		return
	}
	if e.kind == jsonArray {
		for i := 0; i < len(e.arr) && i < len(a.arr) && !w.full(); i++ {
			w.walk(e.arr[i], a.arr[i], path.index(i))
		}
		for i := len(a.arr); i < len(e.arr) && !w.full(); i++ {
			w.add(path.index(i), "missing in actual, expected "+renderJSONNode(e.arr[i]))
		}
		for i := len(e.arr); i < len(a.arr) && !w.full(); i++ {
			w.add(path.index(i), "unexpected in actual, got "+renderJSONNode(a.arr[i]))
		}
		return
	}
	for _, k := range mergeSortedKeys(e.sorted, a.sorted) {
		if w.full() {
			return
		}
		ev, eok := e.obj[k]
		av, aok := a.obj[k]
		switch {
		case eok && aok:
			w.walk(ev, av, path.field(k))
		case eok:
			w.add(path.field(k), "missing in actual, expected "+renderJSONNode(ev))
		default:
			w.add(path.field(k), "unexpected in actual, got "+renderJSONNode(av))
		}
	}
}

// mergeSortedKeys returns the union of two ascending key lists, ascending.
func mergeSortedKeys(a, b []string) []string {
	out := make([]string, 0, max(len(a), len(b)))
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		switch {
		case j == len(b) || i < len(a) && a[i] < b[j]:
			out = append(out, a[i])
			i++
		case i == len(a) || b[j] < a[i]:
			out = append(out, b[j])
			j++
		default:
			out = append(out, a[i])
			i++
			j++
		}
	}
	return out
}

// renderJSONNode prints n as compact JSON (object keys sorted), cut to diffMaxValueRunes characters
// with a trailing "..." when anything was left out. It stops writing once it has enough, so a huge
// value costs a bounded amount of work.
func renderJSONNode(n *jsonNode) string {
	var b strings.Builder
	writeJSONNode(&b, n)
	s := b.String()
	if c := cutRunes(s, diffMaxValueRunes); len(c) < len(s) || b.Len() > jsonRenderCap {
		return c + "..."
	}
	return s
}

func writeJSONNode(b *strings.Builder, n *jsonNode) {
	if b.Len() > jsonRenderCap {
		return
	}
	switch n.kind {
	case jsonNull:
		b.WriteString("null")
	case jsonBool:
		b.WriteString(strconv.FormatBool(n.b))
	case jsonNumber:
		b.WriteString(cutRunes(n.str, diffMaxValueRunes+1))
	case jsonString:
		b.WriteString(strconv.Quote(cutRunes(n.str, diffMaxValueRunes+1)))
	case jsonArray:
		b.WriteByte('[')
		for i, el := range n.arr {
			if i > 0 {
				b.WriteByte(',')
			}
			writeJSONNode(b, el)
			if b.Len() > jsonRenderCap {
				break
			}
		}
		b.WriteByte(']')
	case jsonObject:
		b.WriteByte('{')
		for i, k := range n.sorted {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(strconv.Quote(cutRunes(k, diffMaxValueRunes+1)))
			b.WriteByte(':')
			writeJSONNode(b, n.obj[k])
			if b.Len() > jsonRenderCap {
				break
			}
		}
		b.WriteByte('}')
	}
}

// namedBytes reports the content of a value whose kind is a byte slice but whose type is not the
// builtin []byte (json.RawMessage, type Body []byte).
func namedBytes(actual any) ([]byte, bool) {
	rv := reflect.ValueOf(actual)
	if rv.Kind() != reflect.Slice || rv.Type().Elem().Kind() != reflect.Uint8 {
		return nil, false
	}
	return rv.Bytes(), true
}
