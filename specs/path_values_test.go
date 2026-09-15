package specs

import (
	"strings"
	"testing"
)

func newPathValues(t *testing.T, kv map[string]any) PathValues {
	t.Helper()
	index := make(map[string]int, len(kv))
	pv := PathValues{
		values:  make([]any, len(kv)),
		present: make([]bool, len(kv)),
		index:   index,
	}
	i := 0
	for name, val := range kv {
		index[name] = i
		pv.values[i] = val
		pv.present[i] = true
		i++
	}
	return pv
}

func TestPathValuesHashDeterministicAcrossClones(t *testing.T) {
	a := newPathValues(t, map[string]any{"x": "hello", "y": 3.14})
	b := newPathValues(t, map[string]any{"x": "hello", "y": 3.14})

	if a.Hash() != b.Hash() {
		t.Fatalf("expected equal PathValues to hash the same, got %d and %d", a.Hash(), b.Hash())
	}
}

func TestPathValuesHashDistinguishesStringContent(t *testing.T) {
	a := newPathValues(t, map[string]any{"x": "foo"})
	b := newPathValues(t, map[string]any{"x": "bar"})

	if a.Hash() == b.Hash() {
		t.Fatalf("expected different string content to produce different hashes, both got %d", a.Hash())
	}
}

func TestPathValuesHashDistinguishesLongStringsSharingAPrefix(t *testing.T) {
	prefix := strings.Repeat("a", 512)
	a := newPathValues(t, map[string]any{"x": prefix + "1"})
	b := newPathValues(t, map[string]any{"x": prefix + "2"})

	if a.Hash() == b.Hash() {
		t.Fatalf("expected long strings sharing a prefix to hash differently, both got %d", a.Hash())
	}
}

func TestPathValuesHashDistinguishesFloatContent(t *testing.T) {
	f64a := newPathValues(t, map[string]any{"x": float64(1.5)})
	f64b := newPathValues(t, map[string]any{"x": float64(2.5)})
	if f64a.Hash() == f64b.Hash() {
		t.Fatalf("expected different float64 content to produce different hashes, both got %d", f64a.Hash())
	}

	f32a := newPathValues(t, map[string]any{"x": float32(1.5)})
	f32b := newPathValues(t, map[string]any{"x": float32(2.5)})
	if f32a.Hash() == f32b.Hash() {
		t.Fatalf("expected different float32 content to produce different hashes, both got %d", f32a.Hash())
	}
}

func TestPathValuesHashPointerFallsBackToPositionOnly(t *testing.T) {
	x, y := 1, 2
	a := newPathValues(t, map[string]any{"x": &x})
	b := newPathValues(t, map[string]any{"x": &y})

	if a.Hash() != b.Hash() {
		t.Fatalf("expected pointer values at the same position to fall back to position-only hashing, got %d and %d", a.Hash(), b.Hash())
	}
}
