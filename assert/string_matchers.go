package assert

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
)

// The string matchers accept a string, a []byte (compared without converting it to a string, so no
// allocation) or any named type whose kind is string (via reflection). Everything else never
// matches, and FailureMessage says the actual is not text instead of reporting a bogus mismatch.

// StartWith returns a matcher that expects actual (a string or []byte) to start with prefix. An
// empty prefix matches any text.
func StartWith(prefix string) Matcher {
	return &startWithMatcher{prefix: prefix}
}

type startWithMatcher struct {
	prefix string
}

func (m *startWithMatcher) Match(actual any) bool {
	switch a := actual.(type) {
	case string:
		return strings.HasPrefix(a, m.prefix)
	case []byte:
		return len(a) >= len(m.prefix) && string(a[:len(m.prefix)]) == m.prefix
	}
	s, ok := namedString(actual)
	return ok && strings.HasPrefix(s, m.prefix)
}

func (m *startWithMatcher) FailureMessage(actual any) string {
	text, ok := textOf(actual)
	if !ok {
		return notTextMessage("StartWith", actual)
	}
	return fmt.Sprintf("expected %q to start with %q", text, m.prefix)
}

// Description implements Describer; see equalMatcher.Description.
func (m *startWithMatcher) Description() string {
	return fmt.Sprintf("starting with %q", m.prefix)
}

// EndWith returns a matcher that expects actual (a string or []byte) to end with suffix. An empty
// suffix matches any text.
func EndWith(suffix string) Matcher {
	return &endWithMatcher{suffix: suffix}
}

type endWithMatcher struct {
	suffix string
}

func (m *endWithMatcher) Match(actual any) bool {
	switch a := actual.(type) {
	case string:
		return strings.HasSuffix(a, m.suffix)
	case []byte:
		return len(a) >= len(m.suffix) && string(a[len(a)-len(m.suffix):]) == m.suffix
	}
	s, ok := namedString(actual)
	return ok && strings.HasSuffix(s, m.suffix)
}

func (m *endWithMatcher) FailureMessage(actual any) string {
	text, ok := textOf(actual)
	if !ok {
		return notTextMessage("EndWith", actual)
	}
	return fmt.Sprintf("expected %q to end with %q", text, m.suffix)
}

// Description implements Describer; see equalMatcher.Description.
func (m *endWithMatcher) Description() string {
	return fmt.Sprintf("ending with %q", m.suffix)
}

// MatchRegex returns a matcher that expects actual (a string or []byte) to contain a match for the
// regular expression pattern (RE2 syntax, unanchored: use ^ and $ to anchor). The pattern is
// compiled once, here. An invalid pattern does not panic: the matcher never matches and its
// FailureMessage carries the compile error, so the mistake shows up at the assertion that used it.
func MatchRegex(pattern string) Matcher {
	re, err := regexp.Compile(pattern)
	return &matchRegexMatcher{pattern: pattern, re: re, err: err}
}

type matchRegexMatcher struct {
	pattern string
	re      *regexp.Regexp
	err     error
}

func (m *matchRegexMatcher) Match(actual any) bool {
	if m.err != nil {
		return false
	}
	switch a := actual.(type) {
	case string:
		return m.re.MatchString(a)
	case []byte:
		return m.re.Match(a)
	}
	s, ok := namedString(actual)
	return ok && m.re.MatchString(s)
}

func (m *matchRegexMatcher) FailureMessage(actual any) string {
	if m.err != nil {
		return fmt.Sprintf("MatchRegex: invalid pattern %q: %v", m.pattern, m.err)
	}
	text, ok := textOf(actual)
	if !ok {
		return notTextMessage("MatchRegex", actual)
	}
	return fmt.Sprintf("expected %q to match regex %q", text, m.pattern)
}

// Description implements Describer; see equalMatcher.Description.
func (m *matchRegexMatcher) Description() string {
	if m.err != nil {
		return fmt.Sprintf("matching regex %q (invalid pattern)", m.pattern)
	}
	return fmt.Sprintf("matching regex %q", m.pattern)
}

func notTextMessage(matcher string, actual any) string {
	return fmt.Sprintf("%s: %T is not a string or []byte", matcher, actual)
}

// namedString reports the text of a value whose kind is string but whose type is not the builtin
// string (type Name string). The builtin string and []byte are handled by each matcher's own type
// switch, so this is only the reflect fallback.
func namedString(actual any) (string, bool) {
	if actual == nil {
		return "", false
	}
	rv := reflect.ValueOf(actual)
	if rv.Kind() != reflect.String {
		return "", false
	}
	return rv.String(), true
}

// textOf returns actual as a string for a failure message.
func textOf(actual any) (string, bool) {
	switch a := actual.(type) {
	case string:
		return a, true
	case []byte:
		return string(a), true
	}
	return namedString(actual)
}
