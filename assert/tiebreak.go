package assert

import (
	"bytes"
	"encoding/binary"
	"math"
	"reflect"
	"sort"
)

// Entries of a map whose keys tie (compareMapKeys == 0: NaN keys with the same bit pattern, or a
// struct or array holding one) can only be told apart by their values. This file defines how.
//
// Policy. Every tied entry gets, once, a bounded canonical fingerprint of its value: a byte string
// that is a pure function of the value's content, plus a flag saying whether the whole value fit.
// Entries are ordered by (key, fingerprint bytes, complete before truncated). Both parts are
// precomputed data, so the order is consistent (repeating a comparison gives the same answer, with no
// shared budget or visited set to leak state between comparisons), antisymmetric and transitive.
//
//   - Two entries with equal complete fingerprints are identical in key and value, so their text is
//     identical and their relative order cannot show.
//   - Two entries with equal truncated fingerprints agree on everything the budget could see and are
//     never called equal: they form an ambiguity group. The diff prints one summary line for the
//     group, without ordinals or values, and the renderers print the marker ambiguousValueMarker
//     instead of a value.
//
// Fingerprint content: kinds, integer and float bits (a NaN keeps its payload, -0 differs from +0),
// strings, struct fields in order, interface dynamic type name and package path, pointers
// dereferenced (never addresses), a pointer, map or slice already on the current path as a
// back-reference to its depth index (so cycles terminate and isomorphic cycles agree), maps as their
// entries in the canonical order of their own fingerprints, and funcs, channels and unsafe pointers
// by nil-ness only. Only reflect is used and never Interface, so user methods are never called and
// unexported fields work.
//
// Limits: fingerprintMaxBytes bytes, fingerprintMaxDepth levels and fingerprintMaxMapEntries entries
// per nested map (a larger nested map is recorded by its length only and marked incomplete). Each
// nested map entry gets an equal share of the bytes left, so which part of a map is cut never depends
// on iteration order.
const (
	fingerprintMaxBytes      = 4096
	fingerprintMaxDepth      = 64
	fingerprintMaxMapEntries = 256
)

// ambiguousValueMarker replaces the value of an entry that cannot be told apart from another entry
// with the same key within the fingerprint budget.
const ambiguousValueMarker = "<ambiguous>"

// valueFingerprint is the bounded canonical serialization of one value. complete is false when
// anything was cut, in which case data holds only the part that fit.
type valueFingerprint struct {
	data     []byte
	complete bool
}

// newValueFingerprint returns a fingerprint that owns its bytes.
func newValueFingerprint(v reflect.Value) *valueFingerprint {
	var b fpBuilder
	fp := b.compute(v)
	fp.data = bytes.Clone(fp.data)
	return &fp
}

// compareFingerprints orders by bytes, then complete before truncated. It reads only its arguments.
func compareFingerprints(x, y *valueFingerprint) int {
	if c := bytes.Compare(x.data, y.data); c != 0 {
		return c
	}
	return cmpBool(!x.complete, !y.complete)
}

// fingerprintsAmbiguous reports two fingerprints that tie without being the whole value: the values
// agree on everything the budget saw and nothing more is known.
func fingerprintsAmbiguous(x, y *valueFingerprint) bool {
	return !x.complete && compareFingerprints(x, y) == 0
}

// compareTiedValues orders the values of two entries whose keys tie.
func compareTiedValues(a, b reflect.Value) int {
	return compareFingerprints(newValueFingerprint(a), newValueFingerprint(b))
}

const (
	fpInvalid  byte = 0
	fpDepthCut byte = 0xFF
)

type fpBuilder struct {
	buf        []byte
	max        int
	full       bool // no more room: stop writing
	incomplete bool // something was cut, here or in a nested part
	path       *[]refKey
	own        []refKey
}

// compute fingerprints v into the builder's buffer, reusing it: the returned data is only valid until
// the next call.
func (b *fpBuilder) compute(v reflect.Value) valueFingerprint {
	b.buf, b.max, b.full, b.incomplete = b.buf[:0], fingerprintMaxBytes, false, false
	b.own = b.own[:0]
	b.path = &b.own
	b.value(v, 0)
	return valueFingerprint{data: b.buf, complete: !b.incomplete}
}

func (b *fpBuilder) room(n int) bool {
	if b.full {
		return false
	}
	if len(b.buf)+n > b.max {
		b.full, b.incomplete = true, true
		return false
	}
	return true
}

func (b *fpBuilder) byteOf(x byte) {
	if b.room(1) {
		b.buf = append(b.buf, x)
	}
}

func (b *fpBuilder) u64(x uint64) {
	if b.room(8) {
		b.buf = binary.BigEndian.AppendUint64(b.buf, x)
	}
}

func (b *fpBuilder) uvarint(x uint64) {
	if b.room(uvarintSize(x)) {
		b.buf = binary.AppendUvarint(b.buf, x)
	}
}

func uvarintSize(x uint64) int {
	n := 1
	for ; x >= 0x80; x >>= 7 {
		n++
	}
	return n
}

// str writes s with 0x00 escaped and a terminator, so byte order follows string order and no string
// runs into the next field. A string longer than the room left is cut and the fingerprint marked
// incomplete.
func (b *fpBuilder) str(s string) {
	for i := 0; i < len(s); i++ {
		if s[i] == 0 {
			if !b.room(2) {
				return
			}
			b.buf = append(b.buf, 0, 0xFF)
			continue
		}
		if !b.room(1) {
			return
		}
		b.buf = append(b.buf, s[i])
	}
	if b.room(2) {
		b.buf = append(b.buf, 0, 1)
	}
}

// orderedFloatBits maps float bits to an integer whose order is a total order over every bit
// pattern, so NaN payloads and signed zeros are all kept.
func orderedFloatBits(f float64) uint64 {
	u := math.Float64bits(f)
	if u>>63 != 0 {
		return ^u
	}
	return u | 1<<63
}

// pathIndex returns the depth index of key on the current path, or -1.
func (b *fpBuilder) pathIndex(key refKey) int {
	for i, k := range *b.path {
		if k == key {
			return i
		}
	}
	return -1
}

// backReference writes the back-reference of a pointer, map or slice already on the current path and
// reports cyclic; otherwise it writes nothing. tracked says whether v is a reference worth putting on
// the path (an empty slice is not).
func (b *fpBuilder) backReference(v reflect.Value) (key refKey, tracked, cyclic bool) {
	key, tracked = refIdentity(v)
	if !tracked {
		return key, false, false
	}
	if i := b.pathIndex(key); i >= 0 {
		b.byteOf(1)
		b.uvarint(uint64(i))
		return key, true, true
	}
	return key, true, false
}

func (b *fpBuilder) value(v reflect.Value, depth int) {
	if b.full {
		return
	}
	if !v.IsValid() {
		b.byteOf(fpInvalid)
		return
	}
	if depth > fingerprintMaxDepth {
		b.incomplete = true
		b.byteOf(fpDepthCut)
		return
	}
	k := v.Kind()
	b.byteOf(byte(k) + 1)
	switch k {
	case reflect.Bool:
		if v.Bool() {
			b.byteOf(1)
		} else {
			b.byteOf(0)
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		b.u64(uint64(v.Int()) ^ 1<<63)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		b.u64(v.Uint())
	case reflect.Float32, reflect.Float64:
		b.u64(orderedFloatBits(v.Float()))
	case reflect.Complex64, reflect.Complex128:
		c := v.Complex()
		b.u64(orderedFloatBits(real(c)))
		b.u64(orderedFloatBits(imag(c)))
	case reflect.String:
		b.str(v.String())
	case reflect.Interface:
		if v.IsNil() {
			b.byteOf(0)
			return
		}
		e := v.Elem()
		b.byteOf(1)
		b.str(e.Type().String())
		b.str(e.Type().PkgPath())
		b.value(e, depth+1)
	case reflect.Pointer:
		if v.IsNil() {
			b.byteOf(0)
			return
		}
		key, _, cyclic := b.backReference(v)
		if cyclic {
			return
		}
		b.byteOf(2)
		b.descend(key, v.Elem(), depth)
	case reflect.Struct:
		for i := 0; i < v.NumField() && !b.full; i++ {
			b.value(v.Field(i), depth+1)
		}
	case reflect.Array:
		b.uvarint(uint64(v.Len()))
		for i := 0; i < v.Len() && !b.full; i++ {
			b.value(v.Index(i), depth+1)
		}
	case reflect.Slice:
		if v.IsNil() {
			b.byteOf(0)
			return
		}
		key, tracked, cyclic := b.backReference(v)
		if cyclic {
			return
		}
		b.byteOf(2)
		b.uvarint(uint64(v.Len()))
		if tracked {
			*b.path = append(*b.path, key)
		}
		for i := 0; i < v.Len() && !b.full; i++ {
			b.value(v.Index(i), depth+1)
		}
		if tracked {
			*b.path = (*b.path)[:len(*b.path)-1]
		}
	case reflect.Map:
		if v.IsNil() {
			b.byteOf(0)
			return
		}
		key, _, cyclic := b.backReference(v)
		if cyclic {
			return
		}
		b.byteOf(2)
		b.uvarint(uint64(v.Len()))
		if v.Len() > fingerprintMaxMapEntries {
			b.incomplete = true
			return
		}
		*b.path = append(*b.path, key)
		b.mapEntries(v, depth)
		*b.path = (*b.path)[:len(*b.path)-1]
	case reflect.Func, reflect.Chan, reflect.UnsafePointer:
		if v.IsNil() {
			b.byteOf(0)
		} else {
			b.byteOf(1)
		}
	}
}

// descend fingerprints the target of a tracked reference with the reference on the path.
func (b *fpBuilder) descend(key refKey, target reflect.Value, depth int) {
	*b.path = append(*b.path, key)
	b.value(target, depth+1)
	*b.path = (*b.path)[:len(*b.path)-1]
}

// mapEntries writes the entries of a nested map in the canonical order of their own fingerprints
// (key and value together, so tied keys are ordered by value too). Each entry gets an equal share of
// the room left, so the cut does not depend on the order the runtime iterates the map.
func (b *fpBuilder) mapEntries(m reflect.Value, depth int) {
	n := m.Len()
	if n == 0 || b.full {
		return
	}
	share := (b.max - len(b.buf)) / n
	subs := make([]valueFingerprint, 0, n)
	for it := m.MapRange(); it.Next(); {
		sub := fpBuilder{max: share, path: b.path, buf: make([]byte, 0, min(share, 64))}
		sub.value(it.Key(), depth+1)
		sub.value(it.Value(), depth+1)
		subs = append(subs, valueFingerprint{data: sub.buf, complete: !sub.incomplete})
	}
	sort.Slice(subs, func(i, j int) bool { return compareFingerprints(&subs[i], &subs[j]) < 0 })
	for i := range subs {
		s := &subs[i]
		if !s.complete {
			b.incomplete = true
		}
		if !b.room(uvarintSize(uint64(len(s.data))) + len(s.data) + 1) {
			return
		}
		b.buf = binary.AppendUvarint(b.buf, uint64(len(s.data)))
		b.buf = append(b.buf, s.data...)
		if s.complete {
			b.buf = append(b.buf, 1)
		} else {
			b.buf = append(b.buf, 0)
		}
	}
}
