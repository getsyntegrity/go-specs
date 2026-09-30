package assert

import (
	"fmt"
	"math"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
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
	meter   *fpMeter // the work budget of the message this walk belongs to
	entries []diffEntry
	limit   int  // stop once more than limit entries exist
	depth   int  // paths deeper than this are summarised
	opaque  bool // when set, Stringer structs with hidden fields are walked, not treated as values
	probe   bool // only whether a difference exists matters: no paths, no ordering, stop at the first
	methods bool // both values passed safeForFmt, so rendering may use their String and Error text
	visited map[refPair]bool
	probed  map[refPair]bool // differs answers by reference pair, see differs
}

// structuralDiff returns the "differences:" section for expected versus actual, or "" when the diff
// would add nothing: neither value is a composite, or the walk finds no difference (an oriented error
// comparison, for example, is decided elsewhere and has no structural story).
func structuralDiff(expected, actual any) string {
	return structuralDiffWith(expected, actual, newFPMeter())
}

// structuralDiffWith is structuralDiff spending the fingerprint budget of meter, which the caller
// shares with every other part of the same message.
func structuralDiffWith(expected, actual any, meter *fpMeter) string {
	ev, av := reflect.ValueOf(expected), reflect.ValueOf(actual)
	if !isDiffComposite(ev) && !isDiffComposite(av) {
		return ""
	}
	w := &diffWalker{meter: meter, limit: diffMaxEntries, depth: diffMaxDepth, visited: map[refPair]bool{},
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
func (w *diffWalker) render(v reflect.Value) string {
	return renderDiffValueWith(v, w.methods, w.meter)
}

func (w *diffWalker) full() bool { return len(w.entries) > w.limit }

func (w *diffWalker) add(path, detail string) {
	if !w.full() {
		w.entries = append(w.entries, diffEntry{path, detail})
	}
}

// probeWalks counts the subtrees differs walked rather than answered from its memo: tests read it to
// prove that a shared subtree is walked once per message.
var probeWalks atomic.Int64

// differs reports whether a and b differ at all, stopping at the first difference. It is how the
// depth limit and the opaque-struct rule decide whether a subtree needs an entry, without walking
// or rendering it in full.
func (w *diffWalker) differs(a, b reflect.Value) bool {
	// A probe starts from an empty visited set, so its answer is a pure function of the two values
	// and a reference pair asked about again (a shared subtree met under several paths) is answered
	// from the first probe instead of walked again.
	var key refPair
	memo := false
	switch a.Kind() {
	case reflect.Map, reflect.Slice, reflect.Pointer:
		key, memo = refPairOf(a, b), true
		if d, ok := w.probed[key]; ok {
			return d
		}
	}
	probeWalks.Add(1)
	sub := &diffWalker{meter: w.meter, limit: 0, depth: 1 << 30, opaque: true, probe: true, visited: map[refPair]bool{}}
	sub.walk(a, b, "", 0)
	d := len(sub.entries) > 0
	if memo {
		if w.probed == nil {
			w.probed = map[refPair]bool{}
		}
		w.probed[key] = d
	}
	return d
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
			w.add(path, fmt.Sprintf("expected %s, actual %s", describeNilness(a, w.methods, w.meter), describeNilness(b, w.methods, w.meter)))
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
		w.add(path, fmt.Sprintf("expected %s, actual %s", describeNilness(a, w.methods, w.meter), describeNilness(b, w.methods, w.meter)))
		return
	}
	if a.Pointer() == b.Pointer() || w.seen(a, b) {
		return
	}
	if w.probe {
		w.probeMap(a, b, depth)
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
	w.meter.sortEntries(entries)
	units := tiedUnits(entries)
	keys := make([]reflect.Value, len(units))
	for i, u := range units {
		keys[i] = entries[u.first].key
	}
	labels := disambiguatedKeyLabels(keys, w.methods, w.meter)
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

// probeMap is walkMap for a probe, which only needs to know whether the maps differ. The maps differ
// exactly when their lengths differ or some key of a has no entry in b (a NaN key never has one) or
// some pair of values differs, so nothing is sorted and nothing is labelled, and the walk stops at the
// first difference.
func (w *diffWalker) probeMap(a, b reflect.Value, depth int) {
	if a.Len() != b.Len() {
		w.add("", "length")
		return
	}
	for it := a.MapRange(); it.Next() && !w.full(); {
		bv := b.MapIndex(it.Key())
		if !bv.IsValid() {
			w.add("", "missing in actual")
			return
		}
		w.walk(it.Value(), bv, "", depth+1)
	}
}

// tiedUnit is a run of sorted entries reported as one line: a single entry, or an ambiguity group.
type tiedUnit struct{ first, n int }

// tiedUnits splits sorted entries (fpMeter.sortEntries) into units. Entries with the same key whose value fingerprints tie
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
	fp       *valueFingerprint // set by fpMeter for entries whose key ties; built on first need otherwise
	// ambiguous is set by a renderer's selection when another entry with the same key has a
	// value it cannot tell apart from this one, so this entry's value is not printed.
	ambiguous bool
}

// unmeteredFingerprints counts the fingerprints built by mapEntry.fingerprint outside any message
// meter. Production code never does: tests read it to prove that every path resolves tied entries
// through an fpMeter first.
var unmeteredFingerprints atomic.Int64

// fingerprint returns the entry's value fingerprint. Every renderer and walker resolves the entries
// of a map through an fpMeter first, which bounds the work of the whole message; building one here,
// with the per-entry allowance and no meter, is only the fallback for an entry nobody resolved.
func (e *mapEntry) fingerprint() *valueFingerprint {
	if e.fp == nil {
		unmeteredFingerprints.Add(1)
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
func disambiguatedKeyLabels(keys []reflect.Value, methods bool, meter *fpMeter) []string {
	labels := make([]string, len(keys))
	count := make(map[string]int, len(keys))
	for i, k := range keys {
		labels[i] = renderDiffValueWith(k, methods, meter)
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
// entries m holds, with a fingerprint budget of its own.
func smallestMapEntries(m reflect.Value, k int) (top []mapEntry, total int) {
	return smallestMapEntriesWith(m, k, newFPMeter())
}

// windowMemoMinLen is the smallest map whose window is remembered: below it a scan costs about what
// remembering does.
const windowMemoMinLen = 64

// windowKey identifies the window of the k smallest entries of one map.
type windowKey struct {
	ref refKey
	k   int
}

type mapWindow struct {
	top   []mapEntry
	total int
}

func windowKeyOf(m reflect.Value, k int) (windowKey, bool) {
	if m.Len() < windowMemoMinLen {
		return windowKey{}, false
	}
	ref, ok := refIdentity(m)
	return windowKey{ref: ref, k: k}, ok
}

// mapScan walks a map with MapRange. Iterating with it.Key and it.Value copies every entry, so a huge
// map would still allocate in proportion to its size. When the map may be read through Set (it was
// not reached through an unexported field), each key and value is loaded into one reusable slot
// instead, and only the entries that are kept are copied out with detach.
type mapScan struct {
	it               *reflect.MapIter
	reuse            bool
	keySlot, valSlot reflect.Value
	key, val         reflect.Value
	valLoaded        bool
}

func newMapScan(m reflect.Value) *mapScan {
	s := &mapScan{it: m.MapRange(), reuse: m.CanInterface()}
	if s.reuse {
		s.keySlot = reflect.New(m.Type().Key()).Elem()
		s.valSlot = reflect.New(m.Type().Elem()).Elem()
	}
	return s
}

func (s *mapScan) next() bool {
	if !s.it.Next() {
		return false
	}
	s.valLoaded = false
	if s.reuse {
		s.keySlot.SetIterKey(s.it)
		s.key = s.keySlot
	} else {
		s.key = s.it.Key()
	}
	return true
}

// value loads the current value on first use, so an entry that is clearly out costs one key comparison
// and no value copy.
func (s *mapScan) value() reflect.Value {
	if !s.valLoaded {
		if s.reuse {
			s.valSlot.SetIterValue(s.it)
			s.val = s.valSlot
		} else {
			s.val = s.it.Value()
		}
		s.valLoaded = true
	}
	return s.val
}

func (s *mapScan) entry() mapEntry {
	return mapEntry{key: detached(s.key, s.reuse), val: detached(s.value(), s.reuse), fromA: true}
}

// smallestMapEntriesWith returns the k first entries of m in compareMapEntries order, and the number
// of entries m holds. It never sorts the map and never keeps more than k entries, so a huge map costs
// a few key comparisons per entry instead of a full sort, and the same two maps always give the same
// window whatever order the runtime iterates them in.
//
// Pass one keeps the k smallest distinct keys, each with the number of entries that share it: the k
// first entries in order all belong to those classes, and the class sizes do not depend on iteration
// order. When no class of the window ties, that window is the answer. Otherwise the entries of the
// tied classes are fingerprinted in a second pass, all with the same allowance (fpMeter.tieAllowance
// over the tied entries of the window), and each class keeps its first entries by fingerprint, only
// as many as the window still has room for. An entry that ties a kept entry while truncated cannot be
// told apart from it: both are flagged ambiguous and print no value.
func smallestMapEntriesWith(m reflect.Value, k int, meter *fpMeter) (top []mapEntry, total int) {
	// A big map met again in the same message (a shared or self-containing map is rendered once per
	// path that reaches it) is scanned once: the window found the first time is the window every
	// later render of it gets, which also makes the map print the same everywhere in the message.
	key, memo := windowKeyOf(m, k)
	if memo {
		if w, ok := meter.windows[key]; ok {
			return slices.Clone(w.top), w.total
		}
	}
	meter.scans++
	top, total = selectSmallestMapEntries(m, k, meter)
	if memo {
		if meter.windows == nil {
			meter.windows = map[windowKey]mapWindow{}
		}
		meter.windows[key] = mapWindow{top: slices.Clone(top), total: total}
	}
	return top, total
}

func selectSmallestMapEntries(m reflect.Value, k int, meter *fpMeter) (top []mapEntry, total int) {
	top = make([]mapEntry, 0, min(m.Len(), k))
	counts := make([]int, 0, cap(top))
	scan := newMapScan(m)
	for scan.next() {
		total++
		pos, seen := len(top), false
		for pos > 0 {
			c := compareMapKeys(scan.key, top[pos-1].key)
			if c == 0 {
				counts[pos-1]++
				seen = true
				break
			}
			if c > 0 {
				break
			}
			pos--
		}
		if seen || pos >= k {
			continue
		}
		if len(top) < k {
			top, counts = append(top, mapEntry{}), append(counts, 0)
		}
		copy(top[pos+1:], top[pos:])
		copy(counts[pos+1:], counts[pos:])
		top[pos], counts[pos] = scan.entry(), 1
	}
	// need[i] is how many entries of tie class i the window can still hold, before, tied the entries
	// that get a fingerprint.
	need := make([]int, len(top))
	before, tied := 0, 0
	for i, n := range counts {
		if n >= 2 && before < k {
			need[i] = min(n, k-before)
			tied += n
		}
		before += n
	}
	if tied == 0 {
		return top, total
	}
	allowance := meter.tieAllowance(tied)
	kept := make([][]mapEntry, len(top))
	var scratch fpBuilder
	for scan := newMapScan(m); scan.next(); {
		if len(top) == k && compareMapKeys(scan.key, top[len(top)-1].key) > 0 {
			continue // past the last class of the window: one comparison for most of a big map
		}
		ci := sort.Search(len(top), func(i int) bool { return compareMapKeys(top[i].key, scan.key) >= 0 })
		if ci == len(top) || need[ci] == 0 || compareMapKeys(top[ci].key, scan.key) != 0 {
			continue
		}
		var val reflect.Value
		if allowance > 0 {
			val = scan.value()
		}
		fp, shared := meter.tieFingerprint(&scratch, val, allowance)
		w, pos, ambiguous := kept[ci], len(kept[ci]), false
		for pos > 0 {
			c := compareFingerprints(&fp, w[pos-1].fp)
			if c == 0 && !fp.complete {
				w[pos-1].ambiguous, ambiguous = true, true
			}
			if c >= 0 {
				break
			}
			pos--
		}
		if pos >= need[ci] {
			continue
		}
		if len(w) < need[ci] {
			w = append(w, mapEntry{})
		}
		copy(w[pos+1:], w[pos:])
		w[pos] = scan.entry()
		w[pos].ambiguous, w[pos].fp = ambiguous, ownedFingerprint(fp, shared)
		kept[ci] = w
	}
	out := make([]mapEntry, 0, k)
	for i := range top {
		if need[i] == 0 {
			out = append(out, top[i])
		} else {
			out = append(out, kept[i]...)
		}
		if len(out) >= k {
			return out[:k], total
		}
	}
	return out, total
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
func describeNilness(v reflect.Value, methods bool, meter *fpMeter) string {
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
		return "non-nil " + kind + " " + renderDiffValueWith(v, methods, meter)
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
	return renderDiffValueWith(v, methods, newFPMeter())
}

// renderDiffValueWith is renderDiffValue spending the fingerprint budget of meter, shared with the
// rest of the message.
func renderDiffValueWith(v reflect.Value, methods bool, meter *fpMeter) string {
	return renderDiffLimitedWith(v, diffRenderLimits, methods, meter)
}

func renderDiffLimited(v reflect.Value, lim renderLimits, methods bool) string {
	return renderDiffLimitedWith(v, lim, methods, newFPMeter())
}

func renderDiffLimitedWith(v reflect.Value, lim renderLimits, methods bool, meter *fpMeter) string {
	r := diffRenderer{lim: lim, methods: methods, nodes: nodeBudget{left: lim.nodeBudget}, meter: meter}
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
	meter   *fpMeter
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
		entries, total := smallestMapEntriesWith(v, r.lim.maxElems, r.meter)
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
