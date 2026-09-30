package assert

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// Rendering of arbitrary user values in failure messages: the value a poll last observed and the
// value a poll callback panicked with (PollResult.Message), and the actual of Satisfy.
//
// These values are arbitrary user data, and fmt has no cycle protection: a map that contains itself
// overflows the stack, which no recover can catch. renderBounded therefore checks the value first. An
// acyclic value of ordinary size is printed with the caller's fmt verb, so common failures read as
// before. A cyclic or oversized one is printed by a bounded renderer that marks a cycle as "<cycle>"
// and cuts off at a depth and element limit.

const (
	cycleMarker       = "<cycle>"
	boundedMaxDepth   = 8
	boundedMaxElems   = 16
	boundedNodeBudget = 10_000
)

// renderObserved renders the value a poll last observed, with %#v when that is safe.
func renderObserved(v any) string { return renderBounded(v, "%#v") }

// renderBounded renders v with verb when v is acyclic and of ordinary size, and with the bounded
// renderer otherwise.
func renderBounded(v any, verb string) string {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() {
		return fmt.Sprintf(verb, v) // an untyped nil, spelled by fmt as before
	}
	w := walker{path: map[visit]bool{}, budget: boundedNodeBudget}
	if w.safe(rv) {
		return fmt.Sprintf(verb, v)
	}
	var b strings.Builder
	r := renderer{path: map[visit]bool{}, b: &b}
	r.write(rv, 0)
	return b.String()
}

// visit identifies a reference: the same type, address and length is the same slice, map or pointer.
type visit struct {
	typ reflect.Type
	ptr uintptr
	n   int
}

func refVisit(v reflect.Value) (visit, bool) {
	switch v.Kind() {
	case reflect.Pointer, reflect.Map:
		if v.IsNil() {
			return visit{}, false
		}
		return visit{typ: v.Type(), ptr: v.Pointer()}, true
	case reflect.Slice:
		if v.IsNil() || v.Len() == 0 {
			return visit{}, false
		}
		return visit{typ: v.Type(), ptr: v.Pointer(), n: v.Len()}, true
	}
	return visit{}, false
}

// walker reports whether a value can be handed to fmt: no reference cycle and a bounded size.
type walker struct {
	path   map[visit]bool
	budget int
}

func (w *walker) safe(v reflect.Value) bool {
	if w.budget--; w.budget < 0 {
		return false
	}
	if key, ok := refVisit(v); ok {
		if w.path[key] {
			return false
		}
		w.path[key] = true
		defer delete(w.path, key)
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return true
		}
		return w.safe(v.Elem())
	case reflect.Map:
		iter := v.MapRange()
		for iter.Next() {
			if !w.safe(iter.Key()) || !w.safe(iter.Value()) {
				return false
			}
		}
	case reflect.Slice, reflect.Array:
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

// renderer prints a value with a depth limit, an element limit and cycle detection. It reads values
// by kind, never through Interface, so it also works on unexported struct fields.
type renderer struct {
	path map[visit]bool
	b    *strings.Builder
}

func (r *renderer) write(v reflect.Value, depth int) {
	if !v.IsValid() {
		r.b.WriteString("nil")
		return
	}
	if depth > boundedMaxDepth {
		r.b.WriteString("...")
		return
	}
	if key, ok := refVisit(v); ok {
		if r.path[key] {
			r.b.WriteString(cycleMarker)
			return
		}
		r.path[key] = true
		defer delete(r.path, key)
	}
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			r.b.WriteString("nil")
			return
		}
		r.b.WriteString("&")
		r.write(v.Elem(), depth+1)
	case reflect.Interface:
		if v.IsNil() {
			r.b.WriteString("nil")
			return
		}
		r.write(v.Elem(), depth)
	case reflect.Map:
		r.writeMap(v, depth)
	case reflect.Slice, reflect.Array:
		r.b.WriteString(v.Type().String() + "{")
		for i := 0; i < v.Len(); i++ {
			if i > 0 {
				r.b.WriteString(", ")
			}
			if i == boundedMaxElems {
				fmt.Fprintf(r.b, "... %d more", v.Len()-i)
				break
			}
			r.write(v.Index(i), depth+1)
		}
		r.b.WriteString("}")
	case reflect.Struct:
		r.b.WriteString(v.Type().String() + "{")
		for i := 0; i < v.NumField(); i++ {
			if i > 0 {
				r.b.WriteString(", ")
			}
			r.b.WriteString(v.Type().Field(i).Name + ":")
			r.write(v.Field(i), depth+1)
		}
		r.b.WriteString("}")
	default:
		r.writeLeaf(v)
	}
}

func (r *renderer) writeMap(v reflect.Value, depth int) {
	r.b.WriteString(v.Type().String() + "{")
	type entry struct {
		key string
		val reflect.Value
	}
	var entries []entry
	iter := v.MapRange()
	for iter.Next() {
		var kb strings.Builder
		kr := renderer{path: r.path, b: &kb}
		kr.write(iter.Key(), depth+1)
		entries = append(entries, entry{kb.String(), iter.Value()})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].key < entries[j].key })
	for i, e := range entries {
		if i > 0 {
			r.b.WriteString(", ")
		}
		if i == boundedMaxElems {
			fmt.Fprintf(r.b, "... %d more", len(entries)-i)
			break
		}
		r.b.WriteString(e.key + ":")
		r.write(e.val, depth+1)
	}
	r.b.WriteString("}")
}

func (r *renderer) writeLeaf(v reflect.Value) {
	switch v.Kind() {
	case reflect.Bool:
		r.b.WriteString(strconv.FormatBool(v.Bool()))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		r.b.WriteString(strconv.FormatInt(v.Int(), 10))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		r.b.WriteString(strconv.FormatUint(v.Uint(), 10))
	case reflect.Float32, reflect.Float64:
		r.b.WriteString(strconv.FormatFloat(v.Float(), 'g', -1, 64))
	case reflect.String:
		s := v.String()
		if len(s) > 256 {
			s = s[:256] + "..."
		}
		r.b.WriteString(strconv.Quote(s))
	default:
		r.b.WriteString("<" + v.Type().String() + ">")
	}
}
