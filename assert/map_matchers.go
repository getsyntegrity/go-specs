package assert

import (
	"fmt"
	"reflect"
)

// The map matchers support any map. map[string]any and map[string]string have typed fast paths that
// never allocate on a match; every other map goes through reflection. A key whose type cannot be
// assigned to the map's key type is a plain non-match, never a panic, and FailureMessage says so
// instead of reporting a missing key. An actual that is not a map never matches.

// HaveKey returns a matcher that expects actual (a map) to contain key. The key's type must be
// assignable to the map's key type, so a plain string is not a key of a map[MyString]V, and nil is
// only a key of a map whose key type is an interface. A key with a nil value in the map is still a
// key.
func HaveKey(key any) Matcher {
	return &haveKeyMatcher{key: key}
}

type haveKeyMatcher struct {
	key any
}

func (m *haveKeyMatcher) Match(actual any) bool {
	_, status := lookupKey(actual, m.key, false)
	return status == lookupFound
}

func (m *haveKeyMatcher) FailureMessage(actual any) string {
	_, status := lookupKey(actual, m.key, false)
	switch status {
	case lookupNotMap:
		return notMapMessage("HaveKey", actual)
	case lookupKeyMismatch:
		return fmt.Sprintf("expected %v to have key %v — %s", actual, m.key, keyMismatchReason(actual, m.key))
	default:
		return fmt.Sprintf("expected %v to have key %v", actual, m.key)
	}
}

// Description implements Describer; see equalMatcher.Description.
func (m *haveKeyMatcher) Description() string {
	return fmt.Sprintf("having key %v", m.key)
}

// HaveValue returns a matcher that expects actual (a map) to contain at least one value equal to
// value. Equality is ValuesEqual's: the comparable fast path, errors by identity, then
// reflect.DeepEqual, so an int 1 never equals an int64 1.
func HaveValue(value any) Matcher {
	return &haveValueMatcher{value: value}
}

type haveValueMatcher struct {
	value any
}

func (m *haveValueMatcher) Match(actual any) bool {
	switch a := actual.(type) {
	case map[string]any:
		for _, v := range a {
			if ValuesEqual(m.value, v) {
				return true
			}
		}
		return false
	case map[string]string:
		want, ok := m.value.(string)
		if !ok {
			return false
		}
		for _, v := range a {
			if v == want {
				return true
			}
		}
		return false
	}
	rv, ok := mapValueOf(actual)
	if !ok {
		return false
	}
	for it := rv.MapRange(); it.Next(); {
		if ValuesEqual(m.value, it.Value().Interface()) {
			return true
		}
	}
	return false
}

func (m *haveValueMatcher) FailureMessage(actual any) string {
	rv, ok := mapValueOf(actual)
	if !ok {
		return notMapMessage("HaveValue", actual)
	}
	message := fmt.Sprintf("expected %v to have value %v", actual, m.value)
	if elem := rv.Type().Elem(); elem.Kind() != reflect.Interface {
		if valueType := reflect.TypeOf(m.value); valueType == nil || !valueType.AssignableTo(elem) {
			message += fmt.Sprintf(" — %s values are %s, got %T", rv.Type(), elem, m.value)
		}
	}
	return message
}

// Description implements Describer; see equalMatcher.Description.
func (m *haveValueMatcher) Description() string {
	return fmt.Sprintf("having value %v", m.value)
}

// HavePair returns a matcher that expects actual (a map) to contain key mapped to a value equal to
// value: the key rules are HaveKey's and the equality is HaveValue's. A value found only under a
// different key does not count.
func HavePair(key, value any) Matcher {
	return &havePairMatcher{key: key, value: value}
}

type havePairMatcher struct {
	key   any
	value any
}

func (m *havePairMatcher) Match(actual any) bool {
	if a, ok := actual.(map[string]string); ok {
		k, kOK := m.key.(string)
		want, vOK := m.value.(string)
		if !kOK || !vOK {
			return false
		}
		got, present := a[k]
		return present && got == want
	}
	got, status := lookupKey(actual, m.key, true)
	return status == lookupFound && ValuesEqual(m.value, got)
}

func (m *havePairMatcher) FailureMessage(actual any) string {
	got, status := lookupKey(actual, m.key, true)
	switch status {
	case lookupNotMap:
		return notMapMessage("HavePair", actual)
	case lookupKeyMismatch:
		return fmt.Sprintf("expected %v to have key %v with value %v — %s", actual, m.key, m.value, keyMismatchReason(actual, m.key))
	case lookupMissing:
		return fmt.Sprintf("expected %v to have key %v with value %v — key is missing", actual, m.key, m.value)
	default:
		return fmt.Sprintf("expected %v to have key %v with value %v — key has value %v", actual, m.key, m.value, got)
	}
}

// Description implements Describer; see equalMatcher.Description.
func (m *havePairMatcher) Description() string {
	return fmt.Sprintf("having key %v with value %v", m.key, m.value)
}

type lookupStatus int

const (
	lookupNotMap lookupStatus = iota
	lookupKeyMismatch
	lookupMissing
	lookupFound
)

// lookupKey finds key in the map actual. The value is returned only when wantValue is set and the
// key is found, so a key-only lookup never boxes a map[string]string element.
func lookupKey(actual, key any, wantValue bool) (any, lookupStatus) {
	switch a := actual.(type) {
	case map[string]any:
		k, ok := key.(string)
		if !ok {
			return nil, lookupKeyMismatch
		}
		v, present := a[k]
		if !present {
			return nil, lookupMissing
		}
		return v, lookupFound
	case map[string]string:
		k, ok := key.(string)
		if !ok {
			return nil, lookupKeyMismatch
		}
		v, present := a[k]
		if !present {
			return nil, lookupMissing
		}
		if wantValue {
			return v, lookupFound
		}
		return nil, lookupFound
	}
	rv, ok := mapValueOf(actual)
	if !ok {
		return nil, lookupNotMap
	}
	keyType := rv.Type().Key()
	var keyValue reflect.Value
	if key == nil {
		if keyType.Kind() != reflect.Interface {
			return nil, lookupKeyMismatch
		}
		keyValue = reflect.Zero(keyType)
	} else {
		if !reflect.TypeOf(key).AssignableTo(keyType) {
			return nil, lookupKeyMismatch
		}
		keyValue = reflect.ValueOf(key)
	}
	found, ok := safeMapIndex(rv, keyValue)
	if !ok {
		return nil, lookupMissing
	}
	if wantValue {
		return found.Interface(), lookupFound
	}
	return nil, lookupFound
}

// safeMapIndex is MapIndex that reports an unhashable key (a slice stored in an interface key, say)
// as absent instead of panicking: no such key can be in the map.
func safeMapIndex(rv, key reflect.Value) (value reflect.Value, ok bool) {
	defer func() {
		if recover() != nil {
			value, ok = reflect.Value{}, false
		}
	}()
	value = rv.MapIndex(key)
	return value, value.IsValid()
}

func mapValueOf(actual any) (reflect.Value, bool) {
	if actual == nil {
		return reflect.Value{}, false
	}
	rv := reflect.ValueOf(actual)
	return rv, rv.Kind() == reflect.Map
}

func notMapMessage(matcher string, actual any) string {
	return fmt.Sprintf("%s: %T is not a map", matcher, actual)
}

func keyMismatchReason(actual, key any) string {
	t := reflect.TypeOf(actual)
	return fmt.Sprintf("%s keys are %s, got %T", t, t.Key(), key)
}
