package assert

import "reflect"

// Shared infrastructure for rendering user values in assertion diagnostics.
//
// A failure message prints arbitrary user data, and fmt has no cycle protection: a map or slice that
// contains itself overflows the stack, which no recover can catch. Everything that decides whether a
// value is safe to hand to fmt, and everything that recognises a reference met twice, lives in this
// file so the structural diff (structural_diff.go), the poll and Satisfy renderer (poll_render.go)
// and the matcher messages agree on one definition of "the same reference".

// refKey identifies one reference: the same type and address is the same pointer or map, and the same
// type, address and length is the same slice. The length matters because a short, acyclic view of a
// backing array and a longer view that reaches itself share a pointer and a type.
type refKey struct {
	typ reflect.Type
	ptr uintptr
	n   int
}

// refKeyOf is the identity of a pointer, map or slice value. The caller has already ruled out nil.
func refKeyOf(v reflect.Value) refKey {
	k := refKey{typ: v.Type(), ptr: v.Pointer()}
	if v.Kind() == reflect.Slice {
		k.n = v.Len()
	}
	return k
}

// refIdentity reports the identity of v when v is a reference worth tracking for cycles: a non-nil
// pointer or map, or a non-empty slice. Empty slices are excluded because they can not contain
// anything and may all share one address.
func refIdentity(v reflect.Value) (refKey, bool) {
	switch v.Kind() {
	case reflect.Pointer, reflect.Map:
		if v.IsNil() {
			return refKey{}, false
		}
		return refKeyOf(v), true
	case reflect.Slice:
		if v.IsNil() || v.Len() == 0 {
			return refKey{}, false
		}
		return refKeyOf(v), true
	}
	return refKey{}, false
}

// refPair identifies a pair of references under comparison, as reflect.DeepEqual does: a pair met
// again while it is being compared is treated as equal, which is how a cycle terminates.
type refPair struct{ a, b refKey }

func refPairOf(a, b reflect.Value) refPair { return refPair{refKeyOf(a), refKeyOf(b)} }

// safeForFmt reports whether v can be handed to fmt: no reference cycle, no map key that is not equal
// to itself (fmt orders such keys by iteration, so the text would change from run to run), and at
// most boundedNodeBudget nodes, so the message stays a bounded size. Nodes are counted per value
// visited; a slice or array of scalars costs its length, because fmt prints every element.
func safeForFmt(v reflect.Value) bool {
	w := fmtWalker{budget: boundedNodeBudget}
	return w.safe(v)
}

type fmtWalker struct {
	onPath map[refKey]bool
	budget int
}

func (w *fmtWalker) safe(v reflect.Value) bool {
	if !v.IsValid() {
		return true
	}
	if w.budget--; w.budget < 0 {
		return false
	}
	if key, ok := refIdentity(v); ok {
		if w.onPath[key] {
			return false
		}
		if w.onPath == nil {
			w.onPath = map[refKey]bool{}
		}
		w.onPath[key] = true
		defer delete(w.onPath, key)
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		return v.IsNil() || w.safe(v.Elem())
	case reflect.Map:
		iter := v.MapRange()
		for iter.Next() {
			if containsNaN(iter.Key()) || !w.safe(iter.Key()) || !w.safe(iter.Value()) {
				return false
			}
		}
	case reflect.Slice, reflect.Array:
		if isScalarKind(v.Type().Elem().Kind()) {
			w.budget -= v.Len()
			return w.budget >= 0
		}
		for i := 0; i < v.Len(); i++ {
			if !w.safe(v.Index(i)) {
				return false
			}
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if !w.safe(v.Field(i)) {
				return false
			}
		}
	}
	return true
}

// containsNaN reports whether a map key is, or holds by value, a floating-point or complex NaN. Such a
// key is not equal to itself. Pointers are not followed: they compare by address.
func containsNaN(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Float32, reflect.Float64:
		return v.Float() != v.Float()
	case reflect.Complex64, reflect.Complex128:
		c := v.Complex()
		return real(c) != real(c) || imag(c) != imag(c)
	case reflect.Interface:
		return !v.IsNil() && containsNaN(v.Elem())
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if containsNaN(v.Index(i)) {
				return true
			}
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if containsNaN(v.Field(i)) {
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

// truncationMarker is what a fallback renderer prints where it ran out of node budget. A diagnostic
// never drops content silently.
const truncationMarker = "<truncated>"

// renderLimits are the bounds of one bounded renderer: how deep it descends, how many elements of a
// collection it prints, and how many values (nodes) it visits in total. The structural diff and the
// poll and matcher renderer share the machinery but keep their own limits and formats.
type renderLimits struct{ maxDepth, maxElems, nodeBudget int }

// diffRenderLimits bound one value in a structural diff line. Depth and element limits already cap a
// rendering at a few hundred nodes; the budget is the backstop that keeps the guarantee explicit.
var diffRenderLimits = renderLimits{maxDepth: renderMaxDepth, maxElems: renderMaxElems, nodeBudget: 1024}

// pollRenderLimits bound a value in a poll result or a matcher message.
var pollRenderLimits = renderLimits{maxDepth: boundedMaxDepth, maxElems: boundedMaxElems, nodeBudget: boundedNodeBudget}

// nodeBudget counts the values one render visits. Once take reports false, hit stays true and the
// renderer stops descending.
type nodeBudget struct {
	left int
	hit  bool
}

func (b *nodeBudget) take() bool {
	if b.left <= 0 {
		b.hit = true
		return false
	}
	b.left--
	return true
}

// cutRunes returns the first n runes of s, without copying the rest.
func cutRunes(s string, n int) string {
	for i := range s {
		if n == 0 {
			return s[:i]
		}
		n--
	}
	return s
}
