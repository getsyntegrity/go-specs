package assert

import (
	"fmt"
	"reflect"
	"sort"
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
	visited map[diffVisit]bool
}

// diffVisit identifies a pointer-like pair already under comparison, as reflect.DeepEqual does.
type diffVisit struct {
	a, b   uintptr
	na, nb int // slice lengths, so slices sharing a backing array but not a length stay distinct
	typ    reflect.Type
}

// structuralDiff returns the "differences:" section for expected versus actual, or "" when the diff
// would add nothing: neither value is a composite, or the walk finds no difference (an oriented error
// comparison, for example, is decided elsewhere and has no structural story).
func structuralDiff(expected, actual any) string {
	ev, av := reflect.ValueOf(expected), reflect.ValueOf(actual)
	if !isDiffComposite(ev) && !isDiffComposite(av) {
		return ""
	}
	w := &diffWalker{limit: diffMaxEntries, depth: diffMaxDepth, visited: map[diffVisit]bool{}}
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
	sub := &diffWalker{limit: 0, depth: 1 << 30, opaque: true, visited: map[diffVisit]bool{}}
	sub.walk(a, b, "", 0)
	return len(sub.entries) > 0
}

func (w *diffWalker) walk(a, b reflect.Value, path string, depth int) {
	if w.full() {
		return
	}
	if !a.IsValid() || !b.IsValid() {
		if a.IsValid() != b.IsValid() {
			w.add(path, fmt.Sprintf("expected %s, actual %s", renderDiffValue(a, 0), renderDiffValue(b, 0)))
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
				w.add(path, fmt.Sprintf("expected %s, actual %s", renderDiffValue(a, 0), renderDiffValue(b, 0)))
			}
			return
		}
		w.walkElemTypes(a.Elem(), b.Elem(), path, depth)
	case reflect.Pointer:
		if a.IsNil() || b.IsNil() {
			if a.IsNil() != b.IsNil() {
				w.add(path, fmt.Sprintf("expected %s, actual %s", renderDiffValue(a, 0), renderDiffValue(b, 0)))
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
			w.add(path, fmt.Sprintf("expected %s, actual %s", renderDiffValue(a, 0), renderDiffValue(b, 0)))
		}
	}
}

// walkElemTypes compares the dynamic values held by two interfaces, which may have different types.
func (w *diffWalker) walkElemTypes(a, b reflect.Value, path string, depth int) {
	if a.Type() != b.Type() {
		w.add(path, fmt.Sprintf("type mismatch: expected %s (%s), actual %s (%s)",
			a.Type(), renderDiffValue(a, 0), b.Type(), renderDiffValue(b, 0)))
		return
	}
	w.walk(a, b, path, depth)
}

// seen records a pointer-like pair and reports whether it was already being compared.
func (w *diffWalker) seen(a, b reflect.Value) bool {
	key := diffVisit{a: a.Pointer(), b: b.Pointer(), typ: a.Type()}
	if a.Kind() == reflect.Slice {
		key.na, key.nb = a.Len(), b.Len()
	}
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
			w.add(path, fmt.Sprintf("expected %s, actual %s", renderDiffValue(a, 0), renderDiffValue(b, 0)))
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
			w.add(path, fmt.Sprintf("expected %s, actual %s", describeNilness(a), describeNilness(b)))
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
		w.add(fmt.Sprintf("%s[%d]", path, i), fmt.Sprintf("missing in actual (expected %s)", renderDiffValue(a.Index(i), 0)))
	}
	for i := common; i < lb; i++ {
		w.add(fmt.Sprintf("%s[%d]", path, i), fmt.Sprintf("unexpected in actual (%s)", renderDiffValue(b.Index(i), 0)))
	}
}

func (w *diffWalker) walkMap(a, b reflect.Value, path string, depth int) {
	if a.IsNil() != b.IsNil() {
		w.add(path, fmt.Sprintf("expected %s, actual %s", describeNilness(a), describeNilness(b)))
		return
	}
	if a.Pointer() == b.Pointer() || w.seen(a, b) {
		return
	}
	for _, k := range sortedMapKeys(a) {
		keyPath := fmt.Sprintf("%s[%s]", path, renderDiffValue(k, 0))
		bv := b.MapIndex(k)
		if !bv.IsValid() {
			w.add(keyPath, fmt.Sprintf("missing in actual (expected %s)", renderDiffValue(a.MapIndex(k), 0)))
			continue
		}
		w.walk(a.MapIndex(k), bv, keyPath, depth+1)
	}
	for _, k := range sortedMapKeys(b) {
		if !a.MapIndex(k).IsValid() {
			w.add(fmt.Sprintf("%s[%s]", path, renderDiffValue(k, 0)),
				fmt.Sprintf("unexpected in actual (%s)", renderDiffValue(b.MapIndex(k), 0)))
		}
	}
}

// sortedMapKeys orders keys by their full rendering, so the same two maps always produce the same
// diff whatever order the runtime iterates them in. The sort key is deliberately not the bounded
// display form: long keys sharing a prefix longer than diffMaxValueRunes would tie there and fall
// back to map iteration order.
func sortedMapKeys(m reflect.Value) []reflect.Value {
	keys := m.MapKeys()
	rendered := make([]string, len(keys))
	for i, k := range keys {
		rendered[i] = renderValue(k, 0)
	}
	sort.Sort(&keySorter{keys, rendered})
	return keys
}

type keySorter struct {
	keys     []reflect.Value
	rendered []string
}

func (s *keySorter) Len() int           { return len(s.keys) }
func (s *keySorter) Less(i, j int) bool { return s.rendered[i] < s.rendered[j] }
func (s *keySorter) Swap(i, j int) {
	s.keys[i], s.keys[j] = s.keys[j], s.keys[i]
	s.rendered[i], s.rendered[j] = s.rendered[j], s.rendered[i]
}

// describeNilness says which side of a nil-versus-non-nil slice or map difference a value is on,
// because reflect.DeepEqual treats nil and empty as different and the reader needs to see why.
func describeNilness(v reflect.Value) string {
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
		return "non-nil " + kind + " " + renderDiffValue(v, 0)
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

// renderDiffValue renders v for one diff line: bounded in depth, in element count and in length,
// and safe on unexported fields and cyclic values. It never calls Interface, so it cannot panic on a
// value reached through an unexported field.
func renderDiffValue(v reflect.Value, depth int) string {
	s := renderValue(v, depth)
	if r := []rune(s); len(r) > diffMaxValueRunes {
		return string(r[:diffMaxValueRunes]) + "…"
	}
	return s
}

func renderValue(v reflect.Value, depth int) string {
	if !v.IsValid() {
		return "nil"
	}
	switch v.Kind() {
	case reflect.String:
		return fmt.Sprintf("%q", v.String())
	case reflect.Interface:
		if v.IsNil() {
			return "nil"
		}
		return renderValue(v.Elem(), depth)
	case reflect.Pointer:
		if v.IsNil() {
			return "nil"
		}
		if depth >= renderMaxDepth {
			return "&…"
		}
		return "&" + renderValue(v.Elem(), depth+1)
	case reflect.Struct:
		if depth >= renderMaxDepth {
			return "{…}"
		}
		if v.CanInterface() && isOpaqueStruct(v) {
			return fmt.Sprint(v.Interface())
		}
		t := v.Type()
		parts := make([]string, 0, min(v.NumField(), renderMaxElems)+1)
		for i := 0; i < v.NumField(); i++ {
			if i == renderMaxElems {
				parts = append(parts, "…")
				break
			}
			parts = append(parts, t.Field(i).Name+": "+renderValue(v.Field(i), depth+1))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case reflect.Slice, reflect.Array:
		if depth >= renderMaxDepth {
			return "[…]"
		}
		n := v.Len()
		parts := make([]string, 0, min(n, renderMaxElems)+1)
		for i := 0; i < n && i < renderMaxElems; i++ {
			parts = append(parts, renderValue(v.Index(i), depth+1))
		}
		if n > renderMaxElems {
			parts = append(parts, fmt.Sprintf("… +%d more", n-renderMaxElems))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case reflect.Map:
		if depth >= renderMaxDepth {
			return "map[…]"
		}
		keys := sortedMapKeys(v)
		parts := make([]string, 0, min(len(keys), renderMaxElems)+1)
		for i, k := range keys {
			if i == renderMaxElems {
				parts = append(parts, fmt.Sprintf("… +%d more", len(keys)-renderMaxElems))
				break
			}
			parts = append(parts, renderValue(k, depth+1)+": "+renderValue(v.MapIndex(k), depth+1))
		}
		return "map[" + strings.Join(parts, ", ") + "]"
	case reflect.Func, reflect.Chan, reflect.UnsafePointer:
		if v.IsNil() {
			return "nil " + v.Type().String()
		}
		return v.Type().String()
	default:
		return fmt.Sprintf("%v", v)
	}
}

// containsCycle reports whether v reaches itself through pointers, maps or slices. fmt's %v follows
// maps and slices without a cycle check (it prints only nested pointers as addresses), so a
// self-containing map would overflow the stack while the failure header renders it.
func containsCycle(v reflect.Value) bool {
	return cycleWalk(v, map[diffVisit]bool{})
}

// cycleWalk marks a value true while it is on the current path, so meeting it again is a cycle, and
// leaves it false-but-present afterwards so a shared (acyclic) reference is not walked twice.
func cycleWalk(v reflect.Value, onPath map[diffVisit]bool) bool {
	if !v.IsValid() {
		return false
	}
	switch v.Kind() {
	case reflect.Interface:
		return !v.IsNil() && cycleWalk(v.Elem(), onPath)
	case reflect.Pointer, reflect.Map, reflect.Slice:
		if v.IsNil() || (v.Kind() != reflect.Pointer && v.Len() == 0) {
			return false
		}
		if v.Kind() == reflect.Slice && isScalarKind(v.Type().Elem().Kind()) {
			return false
		}
		key := diffVisit{a: v.Pointer(), typ: v.Type()}
		if inProgress, seen := onPath[key]; seen {
			return inProgress
		}
		onPath[key] = true
		defer func() { onPath[key] = false }()
		switch v.Kind() {
		case reflect.Pointer:
			return cycleWalk(v.Elem(), onPath)
		case reflect.Map:
			for _, k := range v.MapKeys() {
				if cycleWalk(k, onPath) || cycleWalk(v.MapIndex(k), onPath) {
					return true
				}
			}
		default:
			for i := 0; i < v.Len(); i++ {
				if cycleWalk(v.Index(i), onPath) {
					return true
				}
			}
		}
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if cycleWalk(v.Index(i), onPath) {
				return true
			}
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if cycleWalk(v.Field(i), onPath) {
				return true
			}
		}
	}
	return false
}

func isScalarKind(k reflect.Kind) bool {
	switch k {
	case reflect.Bool, reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128:
		return true
	}
	return false
}
