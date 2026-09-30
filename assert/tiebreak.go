package assert

import (
	"bytes"
	"encoding/binary"
	"math"
	"math/bits"
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
// Limits per entry: fingerprintMaxBytes bytes, fingerprintMaxNodes values visited, fingerprintMaxDepth
// levels and fingerprintMaxMapEntries entries per nested map (a larger nested map is recorded by its
// length only and marked incomplete). Each nested map entry gets an equal share of the bytes and of
// the nodes left, so which part of a map is cut never depends on iteration order. Sorting the entries
// of a nested map is charged to the nodes as well, up front, so a map whose sort does not fit is
// recorded by its length only.
//
// Limit per message: the cost of a single entry is bounded, but a message can hold thousands of tied
// entries and render the same map many times, so the work of all the fingerprints of one message is
// bounded too, by fingerprintMessageBudget nodes (see fpMeter). The budget is split without looking at
// iteration order: the tied entries of a map are counted first, and each of them gets the same
// allowance. An entry that runs out of allowance is cut, deterministically, and entries that agree on
// what was visited form an ambiguity group as before; running out of budget is never a claim of
// equality.
const (
	fingerprintMaxBytes      = 4096
	fingerprintMaxNodes      = 4096
	fingerprintMaxDepth      = 64
	fingerprintMaxMapEntries = 256

	// fingerprintMessageBudget is the number of nodes all the fingerprints of one diagnostic message
	// may visit together. At about 150 ns a node it costs a few milliseconds at most.
	fingerprintMessageBudget = 32768
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

// newValueFingerprint returns a fingerprint that owns its bytes, computed with the whole per-entry
// allowance and no message meter. Production code fingerprints through an fpMeter instead.
func newValueFingerprint(v reflect.Value) *valueFingerprint {
	var b fpBuilder
	fp := b.compute(v, fingerprintMaxNodes)
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
	left       int  // nodes this builder may still visit
	full       bool // no more room: stop writing
	incomplete bool // something was cut, here or in a nested part
	nodeCut    bool // the cut was for want of nodes, so a larger allowance could see more
	path       *[]refKey
	own        []refKey
}

// compute fingerprints v into the builder's buffer, reusing it, with at most allowance nodes: the
// returned data is only valid until the next call. The nodes visited are allowance - b.left.
func (b *fpBuilder) compute(v reflect.Value, allowance int) valueFingerprint {
	b.buf, b.max, b.full, b.incomplete, b.nodeCut = b.buf[:0], fingerprintMaxBytes, false, false, false
	b.left = allowance
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
	if b.left <= 0 {
		b.full, b.incomplete, b.nodeCut = true, true, true
		return
	}
	b.left--
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
// the room and of the nodes left, so the cut does not depend on the order the runtime iterates the
// map. Visiting the entries and sorting them cost n + n*log2(n) nodes, charged before any work is
// done; a map that does not fit is recorded by its length only.
func (b *fpBuilder) mapEntries(m reflect.Value, depth int) {
	n := m.Len()
	if n == 0 || b.full {
		return
	}
	cost := n + n*bits.Len(uint(n))
	if cost > b.left {
		b.incomplete, b.nodeCut = true, true
		return
	}
	b.left -= cost
	share, nodeShare := (b.max-len(b.buf))/n, b.left/n
	subs := make([]valueFingerprint, 0, n)
	for it := m.MapRange(); it.Next(); {
		sub := fpBuilder{max: share, left: nodeShare, path: b.path, buf: make([]byte, 0, min(share, 64))}
		sub.value(it.Key(), depth+1)
		sub.value(it.Value(), depth+1)
		b.left -= nodeShare - sub.left
		b.nodeCut = b.nodeCut || sub.nodeCut
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

// fpMeter is the work budget of ONE diagnostic message. Every renderer and walker of the message
// shares it, so the fingerprints of the whole message together visit at most
// fingerprintMessageBudget nodes, however many tied entries it holds and however often the same map
// is rendered.
//
// The budget is spent map by map, in the deterministic order the message visits maps, and split
// inside a map without reading iteration order: tieAllowance counts the tied entries first and gives
// each of them the same number of nodes. Which entry is fingerprinted first therefore never changes
// what any entry gets.
type fpMeter struct {
	left    int // nodes still available to this message
	visited int // nodes visited by this message's fingerprints so far
	memo    map[fpMemoKey]fpMemoEntry
	windows map[windowKey]mapWindow // windows of big maps already selected in this message
	scans   int                     // maps scanned to select a window, for tests
}

func newFPMeter() *fpMeter { return &fpMeter{left: fingerprintMessageBudget} }

// tieAllowance is the number of nodes each of tied entries of one map may visit: an equal share of
// half of what is left (so a map cannot starve the maps after it), at most fingerprintMaxNodes,
// rounded down to a power of two. The rounding keeps the allowance stable while the message spends
// its budget in small steps, which lets a fingerprint computed earlier be recalled instead of built
// again.
func (m *fpMeter) tieAllowance(tied int) int {
	if tied <= 0 {
		return 0
	}
	a := min(fingerprintMaxNodes, m.left/2/tied)
	if a < 1 {
		return 0
	}
	return 1 << (bits.Len(uint(a)) - 1)
}

// fpMemoKey identifies the value of a tied entry that is, or holds through an interface, a pointer,
// map or slice: the static type of the value and the identity of the reference. Its fingerprint
// starts from an empty path, so it is a pure function of what the reference reaches.
type fpMemoKey struct {
	typ reflect.Type
	ref refKey
}

type fpMemoEntry struct {
	allowance int
	nodeCut   bool // cut for want of nodes: valid for this allowance only
	fp        *valueFingerprint
}

func fpMemoKeyOf(v reflect.Value) (fpMemoKey, bool) {
	e := v
	for e.Kind() == reflect.Interface && !e.IsNil() {
		e = e.Elem()
	}
	ref, ok := refIdentity(e)
	return fpMemoKey{typ: v.Type(), ref: ref}, ok
}

// tieFingerprint returns the fingerprint of the value of a tied entry within allowance nodes, and
// charges the meter for the nodes it visits. A value that is a reference already fingerprinted in
// this message is recalled without work when the recalled result is exactly what a new computation
// would give: the same allowance, or a larger one when nothing was cut for want of nodes. shared says
// that fp.data belongs to the meter and may be kept; otherwise it is b's buffer, valid until b is
// used again.
func (m *fpMeter) tieFingerprint(b *fpBuilder, v reflect.Value, allowance int) (fp valueFingerprint, shared bool) {
	if allowance <= 0 {
		return valueFingerprint{}, true // nothing visited: empty and incomplete, so entries tie as truncated
	}
	key, keyed := fpMemoKeyOf(v)
	if keyed {
		if e, ok := m.memo[key]; ok && (e.allowance == allowance || (!e.nodeCut && allowance > e.allowance)) {
			return *e.fp, true
		}
	}
	fp = b.compute(v, allowance)
	used := allowance - b.left
	m.left -= used
	m.visited += used
	if !keyed {
		return fp, false
	}
	owned := &valueFingerprint{data: bytes.Clone(fp.data), complete: fp.complete}
	if m.memo == nil {
		m.memo = map[fpMemoKey]fpMemoEntry{}
	}
	m.memo[key] = fpMemoEntry{allowance: allowance, nodeCut: b.nodeCut, fp: owned}
	return *owned, true
}

// ownedFingerprint returns fp with bytes that outlive the builder.
func ownedFingerprint(fp valueFingerprint, shared bool) *valueFingerprint {
	if !shared {
		fp.data = bytes.Clone(fp.data)
	}
	return &fp
}

// sortEntries puts entries in compareMapEntries order and gives every entry whose key ties a
// fingerprint. Tie classes are found first, by key alone, so their sizes do not depend on iteration
// order; the whole map then has one allowance per tied entry; and only then are fingerprints built,
// class by class in key order.
func (m *fpMeter) sortEntries(entries []mapEntry) {
	sort.SliceStable(entries, func(i, j int) bool { return compareMapKeys(entries[i].key, entries[j].key) < 0 })
	tied := 0
	eachTieClass(entries, func(lo, hi int) { tied += hi - lo })
	if tied == 0 {
		return
	}
	allowance := m.tieAllowance(tied)
	var b fpBuilder
	eachTieClass(entries, func(lo, hi int) {
		for i := lo; i < hi; i++ {
			fp, shared := m.tieFingerprint(&b, entries[i].val, allowance)
			entries[i].fp = ownedFingerprint(fp, shared)
		}
		class := entries[lo:hi]
		sort.SliceStable(class, func(i, j int) bool { return compareMapEntries(&class[i], &class[j]) < 0 })
	})
}

// eachTieClass calls f for every run of two or more entries of sorted whose keys tie.
func eachTieClass(sorted []mapEntry, f func(lo, hi int)) {
	for lo := 0; lo < len(sorted); {
		hi := lo + 1
		for hi < len(sorted) && compareMapKeys(sorted[lo].key, sorted[hi].key) == 0 {
			hi++
		}
		if hi-lo > 1 {
			f(lo, hi)
		}
		lo = hi
	}
}
