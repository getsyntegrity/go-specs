package specs

import "github.com/getsyntegrity/go-specs/assert"

// MatchJSON expects actual (a string or []byte) to be a JSON document semantically equal to
// expected: object key order and whitespace are ignored, array order and number values are exact,
// and invalid JSON, duplicate keys and trailing content never match. See assert.MatchJSON.
func MatchJSON(expected any) Matcher { return assert.MatchJSON(expected) }
