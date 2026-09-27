package snapshots

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
)

// Snapshot comparison semantics.
//
// Two snapshots are equal when they denote the same JSON value, not when they carry the same bytes.
// Object key order and insignificant whitespace are therefore irrelevant — that is why the
// comparison decodes at all instead of comparing the raw text.
//
// Numbers follow the same rule, by exact numeric value: 1, 1.0, 1e0 and 100e-2 are the same
// snapshot, and so are 0 and -0. What they are emphatically NOT is float64. Decoding a JSON number
// into `any` lands it in float64, where every integer above 2^53 loses its last digits, so adjacent
// 64-bit IDs such as 9007199254740992 and 9007199254740993 collapse onto one value and a genuinely
// changed snapshot reports a false-positive match (issue #154). normalizeJSON keeps every digit by
// decoding numbers as json.Number and reducing each literal to an exact canonical form instead.

// canonicalNumber is one JSON number reduced to `<digits>e<exponent>`, with the mantissa stripped of
// leading and trailing zeros. Two literals share a canonical form exactly when they denote the same
// number, at any magnitude. It is a distinct type, not a plain string, so the canonical form of the
// number 1 can never compare equal to the JSON string "1e0".
type canonicalNumber string

// normalizeJSON decodes raw into the comparable value tree described above. It rejects trailing
// content after the value, matching the json.Unmarshal behavior it replaces.
func normalizeJSON(raw []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, &json.SyntaxError{Offset: dec.InputOffset()}
	}
	return canonicalize(v), nil
}

// canonicalize rewrites every json.Number in a decoded tree to its canonical form, in place.
func canonicalize(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, elem := range t {
			t[k] = canonicalize(elem)
		}
		return t
	case []any:
		for i, elem := range t {
			t[i] = canonicalize(elem)
		}
		return t
	case json.Number:
		return canonicalizeNumber(string(t))
	default:
		return v
	}
}

// canonicalizeNumber reduces a JSON number literal to `<digits>e<exponent>` without ever evaluating
// it, so precision is independent of magnitude: no float64, and no expansion of a large exponent
// into digits. A literal it cannot decompose is returned unchanged rather than forced into a wrong
// canonical form — two such literals then compare equal only if their text is identical, which is
// the conservative outcome.
func canonicalizeNumber(literal string) canonicalNumber {
	s := literal
	negative := false
	if len(s) > 0 && (s[0] == '-' || s[0] == '+') {
		negative = s[0] == '-'
		s = s[1:]
	}

	mantissa := s
	exponent := int64(0)
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		mantissa = s[:i]
		// A 32-bit bound is far wider than any representable value and keeps the exponent
		// arithmetic below free of overflow.
		e, err := strconv.ParseInt(s[i+1:], 10, 32)
		if err != nil {
			return canonicalNumber(literal)
		}
		exponent = e
	}

	digits := mantissa
	if i := strings.IndexByte(mantissa, '.'); i >= 0 {
		fraction := mantissa[i+1:]
		digits = mantissa[:i] + fraction
		exponent -= int64(len(fraction))
	}
	if digits == "" || strings.IndexFunc(digits, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return canonicalNumber(literal)
	}

	leading := 0
	for leading < len(digits)-1 && digits[leading] == '0' {
		leading++
	}
	digits = digits[leading:]
	for len(digits) > 1 && digits[len(digits)-1] == '0' {
		digits = digits[:len(digits)-1]
		exponent++
	}
	// Zero has one canonical form whatever its written scale or sign: 0, 0.00, -0 and 0e9 all
	// denote the same number.
	if digits == "0" {
		return canonicalNumber("0")
	}

	sign := ""
	if negative {
		sign = "-"
	}
	return canonicalNumber(sign + digits + "e" + strconv.FormatInt(exponent, 10))
}
