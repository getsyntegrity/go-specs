package assert

import (
	"bytes"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// Structural diffs explain a failed Equal / ToEqual comparison of composite values by naming the
// path of each difference (Order.Items[2].Price) with the expected and actual value found there.
//
// A diff never decides anything. It is built by EqualFailureMessage after ValuesEqual has already
// returned false, so the passing path allocates nothing extra and the verdict cannot change. The walk
// mirrors reflect.DeepEqual (the structural half of ValuesEqual) so a diff exists exactly for the
// values DeepEqual calls unequal: nil and empty slices and maps differ, funcs never match, and a
// pointer pair already being compared is treated as equal, which is how cycles terminate.
//
// Bounds, all documented in docs/DSL.md: at most diffMaxEntries lines, paths at most diffMaxDepth
// segments deep, and each rendered value at most diffMaxValueRunes characters. Anything cut off says
// so in the message instead of disappearing.
const (
	diffMaxEntries    = 10
	diffMaxDepth      = 8
	diffMaxValueRunes = 80
	renderMaxDepth    = 3
	renderMaxElems    = 4
)

type diffEntry struct{ path, detail string }

type diffWalker struct {
	entries []diffEntry
	limit   int  // stop once more than limit entries exist
	depth   int  // paths deeper than this are summarised
	opaque  bool // when set, Stringer structs with hidden fields are walked, not treated as values
	methods bool // both values passed safeForFmt, so rendering may use their String and Error text
	visited map[refPair]bool
}

// structuralDiff returns the "differences:" section for expected versus actual, or "" when the diff
// would add nothing: neither value is a composite, or the walk finds no difference (an oriented error
// comparison, for example, is decided elsewhere and has no structural story).
func structuralDiff(expected, actual any) string {
	ev, av := reflect.ValueOf(expected), reflect.ValueOf(actual)
	if !isDiffComposite(ev) && !isDiffComposite(av) {
		return ""
	}
	w := &diffWalker{limit: diffMaxEntries, depth: diffMaxDepth, visited: map[refPair]bool{},
		methods: safeForFmt(ev) && safeForFmt(av)}
	w.walk(ev, av, diffRootLabel(ev, av), 0)
	if len(w.entries) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("differences:")
	for i, e := range w.entries {
		if i == diffMaxEntries {
			fmt.Fprintf(&b, "\n  ... more differences not shown (limit %d)", diffMaxEntries)
			break
		}
		path := e.path
		if path == "" {
			path = "<root>"
		}
		fmt.Fprintf(&b, "\n  %s: %s", path, e.detail)
	}
	return b.String()
}

func isDiffComposite(v reflect.Value) bool {
	if !v.IsValid() {
		return false
	}
	switch v.Kind() {
	case reflect.Struct, reflect.Map, reflect.Slice, reflect.Array, reflect.Pointer:
		return true
	}
	return false
}

// diffRootLabel names the root after the value's type when it has a name (Order), so a path reads
// Order.Items[2].Price. An unnamed root has no label and shows as <root>.
func diffRootLabel(values ...reflect.Value) string {
	for _, v := range values {
		if !v.IsValid() {
			continue
		}
		t := v.Type()
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		if t.Kind() == reflect.Struct || t.Kind() == reflect.Map || t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
			return t.Name()
		}
	}
	return ""
}

// render renders v for one diff line with this walk's method policy.
func (w *diffWalker) render(v reflect.Value) string { return renderDiffValue(v, w.methods) }

func (w *diffWalker) full() bool { return len(w.entries) > w.limit }

func (w *diffWalker) add(path, detail string) {
	if !w.full() {
		w.entries = append(w.entries, diffEntry{path, detail})
	}
}

// differs reports whether a and b differ at all, stopping at the first difference. It is how the
// depth limit and the opaque-struct rule decide whether a subtree needs an entry, without walking
// or rendering it in full.
func (w *diffWalker) differs(a, b reflect.Value) bool {
	sub := &diffWalker{limit: 0, depth: 1 << 30, opaque: true, visited: map[refPair]bool{}}
	sub.walk(a, b, "", 0)
	return len(sub.entries) > 0
}

func (w *diffWalker) walk(a, b reflect.Value, path string, depth int) {
	if w.full() {
		return
	}
	if !a.IsValid() || !b.IsValid() {
		if a.IsValid() != b.IsValid() {
			w.add(path, fmt.Sprintf("expected %s, actual %s", w.render(a), w.render(b)))
		}
		return
	}
	if a.Type() != b.Type() {
		w.add(path, fmt.Sprintf("type mismatch: expected %s, actual %s", a.Type(), b.Type()))
		return
	}
	if depth >= w.depth && isDiffComposite(a) && w.differs(a, b) {
		w.add(path, fmt.Sprintf("differs below this point (depth limit %d reached)", diffMaxDepth))
		return
	}

	switch a.Kind() {
	case reflect.Interface:
		if a.IsNil() || b.IsNil() {
			if a.IsNil() != b.IsNil() {
				w.add(path, fmt.Sprintf("expected %s, actual %s", w.render(a), w.render(b)))
			}
			return
		}
		w.walkElemTypes(a.Elem(), b.Elem(), path, depth)
	case reflect.Pointer:
		if a.IsNil() || b.IsNil() {
			if a.IsNil() != b.IsNil() {
				w.add(path, fmt.Sprintf("expected %s, actual %s", w.render(a), w.render(b)))
			}
			return
		}
		if a.Pointer() == b.Pointer() || w.seen(a, b) {
			return
		}
		w.walk(a.Elem(), b.Elem(), path, depth)
	case reflect.Struct:
		w.walkStruct(a, b, path, depth)
	case reflect.Slice, reflect.Array:
		w.walkList(a, b, path, depth)
	case reflect.Map:
		w.walkMap(a, b, path, depth)
	case reflect.Func:
		if !a.IsNil() || !b.IsNil() {
			w.add(path, "func values are never equal")
		}
	default:
		if !diffScalarEqual(a, b) {
			w.add(path, fmt.Sprintf("expected %s, actual %s", w.render(a), w.render(b)))
		}
	}
}

// walkElemTypes compares the dynamic values held by two interfaces, which may have different types.
func (w *diffWalker) walkElemTypes(a, b reflect.Value, path string, depth int) {
	if a.Type() != b.Type() {
		w.add(path, fmt.Sprintf("type mismatch: expected %s (%s), actual %s (%s)",
			a.Type(), w.render(a), b.Type(), w.render(b)))
		return
	}
	w.walk(a, b, path, depth)
}

// seen records a pointer-like pair and reports whether it was already being compared.
func (w *diffWalker) seen(a, b reflect.Value) bool {
	key := refPairOf(a, b)
	if w.visited[key] {
		return true
	}
	w.visited[key] = true
	return false
}

func (w *diffWalker) walkStruct(a, b reflect.Value, path string, depth int) {
	if !w.opaque && isOpaqueStruct(a) && a.CanInterface() && b.CanInterface() {
		// time.Time and friends: their fields are an implementation detail, their String is the value.
		if w.differs(a, b) {
			w.add(path, fmt.Sprintf("expected %s, actual %s", w.render(a), w.render(b)))
		}
		return
	}
	t := a.Type()
	for i := 0; i < a.NumField(); i++ {
		w.walk(a.Field(i), b.Field(i), joinField(path, t.Field(i).Name), depth+1)
	}
}

var (
	stringerType = reflect.TypeFor[fmt.Stringer]()
	errorType    = reflect.TypeFor[error]()
)

// isOpaqueStruct reports a struct that renders itself (Stringer or error) and hides state in
// unexported fields, so its fields would only be noise in a path.
func isOpaqueStruct(v reflect.Value) bool {
	t := v.Type()
	if !t.Implements(stringerType) && !t.Implements(errorType) {
		return false
	}
	for i := 0; i < t.NumField(); i++ {
		if !t.Field(i).IsExported() {
			return true
		}
	}
	return false
}

func joinField(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}

func (w *diffWalker) walkList(a, b reflect.Value, path string, depth int) {
	if a.Kind() == reflect.Slice {
		if a.IsNil() != b.IsNil() {
			w.add(path, fmt.Sprintf("expected %s, actual %s", describeNilness(a, w.methods), describeNilness(b, w.methods)))
			return
		}
		if a.Pointer() == b.Pointer() && a.Len() == b.Len() {
			return
		}
		// A slice pair already under comparison is a cycle (a[0] == a): treat it as equal, as for
		// pointers and maps, so the walk and differs both terminate.
		if a.Len() > 0 && w.seen(a, b) {
			return
		}
	}
	la, lb := a.Len(), b.Len()
	if la != lb {
		w.add(path, fmt.Sprintf("length: expected %d, actual %d", la, lb))
	}
	common := min(la, lb)
	for i := 0; i < common; i++ {
		w.walk(a.Index(i), b.Index(i), fmt.Sprintf("%s[%d]", path, i), depth+1)
	}
	for i := common; i < la; i++ {
		w.add(fmt.Sprintf("%s[%d]", path, i), fmt.Sprintf("missing in actual (expected %s)", w.render(a.Index(i))))
	}
	for i := common; i < lb; i++ {
		w.add(fmt.Sprintf("%s[%d]", path, i), fmt.Sprintf("unexpected in actual (%s)", w.render(b.Index(i))))
	}
}

func (w *diffWalker) walkMap(a, b reflect.Value, path string, depth int) {
	if a.IsNil() != b.IsNil() {
		w.add(path, fmt.Sprintf("expected %s, actual %s", describeNilness(a, w.methods), describeNilness(b, w.methods)))
		return
	}
	if a.Pointer() == b.Pointer() || w.seen(a, b) {
		return
	}
	// Entries keep each key with its value from MapRange. A key that is not equal to itself (NaN, or a
	// struct or array holding one) can never be looked up again, so MapIndex on the source map would
	// return an invalid Value; only keys that are used to probe the OTHER map go through MapIndex, and
	// there "not found" is exactly reflect.DeepEqual's verdict.
	// Labels are computed over the union of both maps' entries, sorted once, so a key only in a and a
	// key only in b that render alike still get different paths.
	entries := rangeMapEntries(a, true, make([]mapEntry, 0, a.Len()+b.Len()))
	bOnly := rangeMapEntries(b, false, nil)
	for _, e := range bOnly {
		if !a.MapIndex(e.key).IsValid() {
			entries = append(entries, e)
		}
	}
	sort.SliceStable(entries, func(i, j int) bool { return compareMapEntries(&entries[i], &entries[j]) < 0 })
	units := tiedUnits(entries)
	keys := make([]reflect.Value, len(units))
	for i, u := range units {
		keys[i] = entries[u.first].key
	}
	labels := disambiguatedKeyLabels(keys, w.methods)
	for i, u := range units {
		e := entries[u.first]
		keyPath := fmt.Sprintf("%s[%s]", path, labels[i])
		if u.n > 1 {
			w.add(keyPath, ambiguityDetail(entries[u.first:u.first+u.n]))
			continue
		}
		if !e.fromA {
			continue
		}
		bv := b.MapIndex(e.key)
		if !bv.IsValid() {
			w.add(keyPath, fmt.Sprintf("missing in actual (expected %s)", w.render(e.val)))
			continue
		}
		w.walk(e.val, bv, keyPath, depth+1)
	}
	for i, u := range units {
		e := entries[u.first]
		if u.n > 1 || e.fromA {
			continue
		}
		w.add(fmt.Sprintf("%s[%s]", path, labels[i]),
			fmt.Sprintf("unexpected in actual (%s)", w.render(e.val)))
	}
}

// tiedUnit is a run of sorted entries reported as one line: a single entry, or an ambiguity group.
type tiedUnit struct{ first, n int }

// tiedUnits splits sorted entries into units. Entries with the same key whose value fingerprints tie
// while truncated cannot be told apart within the budget and form one group; every other entry is a
// unit of its own (entries with equal complete fingerprints are identical, so each keeps its own
// ordinal and the text does not depend on which comes first).
func tiedUnits(entries []mapEntry) []tiedUnit {
	units := make([]tiedUnit, 0, len(entries))
	for i := 0; i < len(entries); {
		j := i + 1
		for j < len(entries) && indistinguishable(&entries[i], &entries[j]) {
			j++
		}
		units = append(units, tiedUnit{first: i, n: j - i})
		i = j
	}
	return units
}

// ambiguityDetail is the one line that stands for a group of entries that cannot be told apart. It
// carries no value: the rendered text of such entries could differ between them, which would make the
// line depend on which one was picked.
func ambiguityDetail(group []mapEntry) string {
	fromA := 0
	for _, e := range group {
		if e.fromA {
			fromA++
		}
	}
	fromB := len(group) - fromA
	head := fmt.Sprintf("%d entries indistinguishable within the diagnostic budget", len(group))
	switch {
	case fromB == 0:
		return fmt.Sprintf("%s: all %d missing in actual", head, fromA)
	case fromA == 0:
		return fmt.Sprintf("%s: all %d unexpected in actual", head, fromB)
	}
	return fmt.Sprintf("%s: %d missing in actual, %d unexpected in actual", head, fromA, fromB)
}

// mapEntry is one key of the union of two maps with its value, and whether the first (expected) map
// holds it. The value travels with the key because a non-reflexive key cannot be looked up again.
type mapEntry struct {
	key, val reflect.Value
	fromA    bool
	fp       *valueFingerprint // computed on first need, only for entries whose key ties
	// ambiguous is set by a renderer's selection when another entry with the same key has a
	// value it cannot tell apart from this one, so this entry's value is not printed.
	ambiguous bool
}

// fingerprint returns the entry's value fingerprint, computing it once.
func (e *mapEntry) fingerprint() *valueFingerprint {
	if e.fp == nil {
		e.fp = newValueFingerprint(e.val)
	}
	return e.fp
}

// indistinguishable reports two entries with the same key whose values agree on everything the
// fingerprint budget could see, without being known to be equal.
func indistinguishable(x, y *mapEntry) bool {
	return compareMapKeys(x.key, y.key) == 0 && fingerprintsAmbiguous(x.fingerprint(), y.fingerprint())
}

// rangeMapEntries appends every entry of m, in iteration order, using MapRange so no value is fetched
// through a key.
func rangeMapEntries(m reflect.Value, fromA bool, dst []mapEntry) []mapEntry {
	it := m.MapRange()
	for it.Next() {
		dst = append(dst, mapEntry{key: it.Key(), val: it.Value(), fromA: fromA})
	}
	return dst
}

// compareMapEntries orders entries by key, then, for keys that still tie (NaNs with the same bit
// pattern), by the fingerprint of their values (see tiebreak.go), then expected before actual. The
// fingerprint is computed once per entry and the comparison reads nothing else, so it is a consistent
// strict weak order. Entries that tie on all three are identical in key and value, or, when their
// fingerprints are truncated, indistinguishable within the budget: renderers then print no value.
func compareMapEntries(x, y *mapEntry) int {
	if c := compareMapKeys(x.key, y.key); c != 0 {
		return c
	}
	if c := compareFingerprints(x.fingerprint(), y.fingerprint()); c != 0 {
		return c
	}
	return cmpBool(!x.fromA, !y.fromA)
}

// disambiguatedKeyLabels returns the bounded display label of each key, in order. A label shared by
// two or more distinct keys (long strings with a common prefix, structs differing past the rendered
// fields) gets its 1-based ordinal among the colliding keys appended after a space, inside the
// brackets: `["kkkk… #1]`, `["kkkk… #2]`. Labels that are unique are left exactly as rendered.
func disambiguatedKeyLabels(keys []reflect.Value, methods bool) []string {
	labels := make([]string, len(keys))
	count := make(map[string]int, len(keys))
	for i, k := range keys {
		labels[i] = renderDiffValue(k, methods)
		count[labels[i]]++
	}
	seen := make(map[string]int)
	for i, l := range labels {
		if count[l] > 1 {
			seen[l]++
			labels[i] = fmt.Sprintf("%s #%d", l, seen[l])
		}
	}
	return labels
}

// smallestMapEntries returns the k first entries of m in compareMapEntries order, and the number of
// entries m holds. It makes one MapRange pass and keeps a sorted window of k, so a huge map costs one
// comparison per entry (plus a few for the entries that enter the window) instead of a full sort, and
// the same two maps always give the same window whatever order the runtime iterates them in.
func smallestMapEntries(m reflect.Value, k int) (top []mapEntry, total int) {
	top = make([]mapEntry, 0, min(m.Len(), k))
	// Iterating with it.Key and it.Value copies every entry, so a huge map would still allocate in
	// proportion to its size. When the map may be read through Set (it was not reached through an
	// unexported field), each key and value is loaded into one reusable slot instead, and only the
	// entries that enter the window are copied out.
	reuse := m.CanInterface()
	var keySlot, valSlot reflect.Value
	if reuse {
		keySlot = reflect.New(m.Type().Key()).Elem()
		valSlot = reflect.New(m.Type().Elem()).Elem()
	}
	// Ties on the key are broken by the fingerprint of the value (tiebreak.go). It is built in one
	// reusable scratch buffer, so an entry that ties but stays out of the window allocates nothing;
	// only an entry that enters the window keeps a copy. An entry that ties a window entry while
	// truncated cannot be told apart from it: both are flagged ambiguous and print no value.
	var scratch fpBuilder
	var scratchFP valueFingerprint
	for it := m.MapRange(); it.Next(); {
		total++
		key, valLoaded := keySlot, false
		if reuse {
			keySlot.SetIterKey(it)
		} else {
			key = it.Key()
		}
		value := func() reflect.Value {
			if !reuse {
				return it.Value()
			}
			if !valLoaded {
				valSlot.SetIterValue(it)
				valLoaded = true
			}
			return valSlot
		}
		var fp *valueFingerprint
		ambiguous := false
		// The value is only fetched when the key ties or the entry enters the window, so an entry
		// that is clearly out costs one key comparison and no value copy.
		pos := len(top)
		for pos > 0 {
			c := compareMapKeys(key, top[pos-1].key)
			if c == 0 {
				if fp == nil {
					scratchFP = scratch.compute(value())
					fp = &scratchFP
				}
				c = compareFingerprints(fp, top[pos-1].fingerprint())
				if c == 0 && !fp.complete {
					top[pos-1].ambiguous, ambiguous = true, true
				}
			}
			if c >= 0 {
				break
			}
			pos--
		}
		if pos >= k {
			continue
		}
		if len(top) < k {
			top = append(top, mapEntry{})
		}
		copy(top[pos+1:], top[pos:])
		top[pos] = mapEntry{key: detached(key, reuse), val: detached(value(), reuse), fromA: true, ambiguous: ambiguous}
		if fp != nil {
			top[pos].fp = &valueFingerprint{data: bytes.Clone(fp.data), complete: fp.complete}
		}
	}
	return top, total
}

// detached returns v itself, or a copy of it when v is a reusable slot that the next iteration
// overwrites.
func detached(v reflect.Value, slot bool) reflect.Value {
	if !slot {
		return v
	}
	c := reflect.New(v.Type()).Elem()
	c.Set(v)
	return c
}

// compareMapKeys is a total order over every comparable key kind, reading only through reflect (never
// Interface, so unexported fields are fine). It compares full values, not the bounded rendering, so
// keys that render alike still order deterministically. Pointers, channels and unsafe pointers order
// by address and are never dereferenced, so they cannot recurse into a cycle; that order is stable
// within a run but not across runs. NaN sorts before every number (NaNs among themselves by bit pattern) and -0 equals +0.
func compareMapKeys(a, b reflect.Value) int {
	if !a.IsValid() || !b.IsValid() {
		return cmpBool(a.IsValid(), b.IsValid())
	}
	if a.Kind() != b.Kind() {
		return cmpInt(int64(a.Kind()), int64(b.Kind()))
	}
	switch a.Kind() {
	case reflect.Bool:
		return cmpBool(a.Bool(), b.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return cmpInt(a.Int(), b.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return cmpUint(a.Uint(), b.Uint())
	case reflect.Float32, reflect.Float64:
		return cmpFloat(a.Float(), b.Float())
	case reflect.Complex64, reflect.Complex128:
		x, y := a.Complex(), b.Complex()
		if c := cmpFloat(real(x), real(y)); c != 0 {
			return c
		}
		return cmpFloat(imag(x), imag(y))
	case reflect.String:
		return strings.Compare(a.String(), b.String())
	case reflect.Pointer, reflect.Chan, reflect.UnsafePointer:
		return cmpUint(uint64(a.Pointer()), uint64(b.Pointer()))
	case reflect.Array:
		for i := 0; i < a.Len(); i++ {
			if c := compareMapKeys(a.Index(i), b.Index(i)); c != 0 {
				return c
			}
		}
		return 0
	case reflect.Struct:
		for i := 0; i < a.NumField(); i++ {
			if c := compareMapKeys(a.Field(i), b.Field(i)); c != 0 {
				return c
			}
		}
		return 0
	case reflect.Interface:
		if a.IsNil() || b.IsNil() {
			return cmpBool(!a.IsNil(), !b.IsNil())
		}
		ea, eb := a.Elem(), b.Elem()
		if ta, tb := ea.Type(), eb.Type(); ta != tb {
			if c := strings.Compare(ta.String(), tb.String()); c != 0 {
				return c
			}
			// Same spelling, different types (same name in different packages or scopes).
			if c := strings.Compare(ta.PkgPath(), tb.PkgPath()); c != 0 {
				return c
			}
		}
		return compareMapKeys(ea, eb)
	}
	return 0
}

func cmpBool(a, b bool) int {
	switch {
	case a == b:
		return 0
	case !a:
		return -1
	}
	return 1
}

func cmpInt(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func cmpUint(a, b uint64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func cmpFloat(a, b float64) int {
	an, bn := a != a, b != b
	switch {
	case an && bn:
		return cmpUint(math.Float64bits(a), math.Float64bits(b))
	case an:
		return -1
	case bn:
		return 1
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// describeNilness says which side of a nil-versus-non-nil slice or map difference a value is on,
// because reflect.DeepEqual treats nil and empty as different and the reader needs to see why.
func describeNilness(v reflect.Value, methods bool) string {
	kind := "slice"
	if v.Kind() == reflect.Map {
		kind = "map"
	}
	switch {
	case v.IsNil():
		return "nil " + kind
	case v.Len() == 0:
		return "empty non-nil " + kind
	default:
		return "non-nil " + kind + " " + renderDiffValue(v, methods)
	}
}

// diffScalarEqual is reflect.DeepEqual's verdict for the kinds that hold no other values.
func diffScalarEqual(a, b reflect.Value) bool {
	switch a.Kind() {
	case reflect.Bool:
		return a.Bool() == b.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return a.Int() == b.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return a.Uint() == b.Uint()
	case reflect.Float32, reflect.Float64:
		return a.Float() == b.Float()
	case reflect.Complex64, reflect.Complex128:
		return a.Complex() == b.Complex()
	case reflect.String:
		return a.String() == b.String()
	case reflect.Chan, reflect.UnsafePointer:
		return a.Pointer() == b.Pointer()
	}
	return true
}

// renderDiffValue renders v for one diff line: bounded in depth, in element count, in node count and
// in length, and safe on unexported fields and cyclic values. It reads by kind and never calls
// Interface except for a value that may render itself: methods says the value passed safeForFmt, so a
// named scalar or an opaque struct may use its own String or Error text, exactly as in an ordinary
// message. With methods false (a cyclic or oversized value) no user method is ever called.
func renderDiffValue(v reflect.Value, methods bool) string {
	return renderDiffLimited(v, diffRenderLimits, methods)
}

func renderDiffLimited(v reflect.Value, lim renderLimits, methods bool) string {
	r := diffRenderer{lim: lim, methods: methods, nodes: nodeBudget{left: lim.nodeBudget}}
	s := r.render(v, 0)
	if r := []rune(s); len(r) > diffMaxValueRunes {
		return string(r[:diffMaxValueRunes]) + "…"
	}
	return s
}

// diffRenderer holds the state of one renderDiffValue call: its limits, whether user methods may be
// used, and the node budget shared by every value the call visits.
type diffRenderer struct {
	lim     renderLimits
	methods bool
	nodes   nodeBudget
}

// usesMethods reports whether v renders itself through fmt: only on the ordinary path, and only for a
// value fmt is allowed to call methods on.
func (r *diffRenderer) usesMethods(v reflect.Value) bool {
	return r.methods && v.CanInterface() && v.Type().NumMethod() > 0
}

func (r *diffRenderer) render(v reflect.Value, depth int) string {
	if !v.IsValid() {
		return "nil"
	}
	if !r.nodes.take() {
		return truncationMarker
	}
	switch v.Kind() {
	case reflect.String:
		if r.usesMethods(v) {
			return fmt.Sprintf("%q", v)
		}
		// Cut before quoting so a huge string is never copied whole; the caller cuts the quoted text
		// to the same number of runes, and quoting never shortens a rune, so the result is identical.
		return strconv.Quote(cutRunes(v.String(), diffMaxValueRunes))
	case reflect.Interface:
		if v.IsNil() {
			return "nil"
		}
		return r.render(v.Elem(), depth)
	case reflect.Pointer:
		if v.IsNil() {
			return "nil"
		}
		if depth >= r.lim.maxDepth {
			return "&…"
		}
		return "&" + r.render(v.Elem(), depth+1)
	case reflect.Struct:
		if depth >= r.lim.maxDepth {
			return "{…}"
		}
		if r.methods && v.CanInterface() && isOpaqueStruct(v) {
			return fmt.Sprint(v.Interface())
		}
		t := v.Type()
		parts := make([]string, 0, min(v.NumField(), r.lim.maxElems)+1)
		for i := 0; i < v.NumField(); i++ {
			if i == r.lim.maxElems {
				parts = append(parts, "…")
				break
			}
			parts = append(parts, t.Field(i).Name+": "+r.render(v.Field(i), depth+1))
			if r.nodes.hit {
				break
			}
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case reflect.Slice, reflect.Array:
		if depth >= r.lim.maxDepth {
			return "[…]"
		}
		n := v.Len()
		parts := make([]string, 0, min(n, r.lim.maxElems)+1)
		for i := 0; i < n && i < r.lim.maxElems; i++ {
			parts = append(parts, r.render(v.Index(i), depth+1))
			if r.nodes.hit {
				return "[" + strings.Join(parts, ", ") + "]"
			}
		}
		if n > r.lim.maxElems {
			parts = append(parts, fmt.Sprintf("… +%d more", n-r.lim.maxElems))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case reflect.Map:
		if depth >= r.lim.maxDepth {
			return "map[…]"
		}
		entries, total := smallestMapEntries(v, r.lim.maxElems)
		parts := make([]string, 0, len(entries)+1)
		for _, e := range entries {
			parts = append(parts, r.render(e.key, depth+1)+": "+r.renderEntryValue(e, depth+1))
			if r.nodes.hit {
				return "map[" + strings.Join(parts, ", ") + "]"
			}
		}
		if total > r.lim.maxElems {
			parts = append(parts, fmt.Sprintf("… +%d more", total-r.lim.maxElems))
		}
		return "map[" + strings.Join(parts, ", ") + "]"
	case reflect.Func, reflect.Chan, reflect.UnsafePointer:
		if v.IsNil() {
			return "nil " + v.Type().String()
		}
		return v.Type().String()
	default:
		return r.leaf(v)
	}
}

// renderEntryValue renders the value of a map entry, or the ambiguity marker when another entry with
// the same key cannot be told apart from it within the fingerprint budget.
func (r *diffRenderer) renderEntryValue(e mapEntry, depth int) string {
	if e.ambiguous {
		return ambiguousValueMarker
	}
	return r.render(e.val, depth)
}

// leaf renders a scalar. A named scalar with methods keeps its String text on the ordinary path (a
// time.Duration reads 1s); otherwise the text is built from the kind alone, which is what fmt prints
// for a value without methods.
func (r *diffRenderer) leaf(v reflect.Value) string {
	if r.usesMethods(v) {
		return fmt.Sprintf("%v", v)
	}
	switch v.Kind() {
	case reflect.Bool:
		return strconv.FormatBool(v.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return strconv.FormatUint(v.Uint(), 10)
	case reflect.Float32:
		return strconv.FormatFloat(v.Float(), 'g', -1, 32)
	case reflect.Float64:
		return strconv.FormatFloat(v.Float(), 'g', -1, 64)
	case reflect.Complex64:
		return strconv.FormatComplex(v.Complex(), 'g', -1, 64)
	case reflect.Complex128:
		return strconv.FormatComplex(v.Complex(), 'g', -1, 128)
	}
	return "<" + v.Type().String() + ">"
}
