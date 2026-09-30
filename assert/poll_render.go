package assert

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// The bounded fallback renderer behind formatUserValue (render_safe.go): the value a poll last
// observed, the value a poll callback panicked with (PollResult.Message), the actual of Satisfy and the
// operands of every matcher message.
//
// These values are arbitrary user data, and fmt has no cycle protection: a map that contains itself
// overflows the stack, which no recover can catch. formatUserValue therefore checks the value first. An
// acyclic value of ordinary size is printed with the caller's fmt verb, so common failures read as
// before. A cyclic or oversized one is printed by this renderer, which marks a cycle as "<cycle>",
// cuts off at a depth, element and node limit, and says so with "<truncated>" when the budget runs out.

const (
	cycleMarker       = "<cycle>"
	boundedMaxDepth   = 8
	boundedMaxElems   = 16
	boundedNodeBudget = 10_000
)

// renderObserved renders the value a poll last observed, with %#v when that is safe.
func renderObserved(v any) string { return formatUserValue(v, "%#v") }

// renderFallback prints v with the bounded renderer: depth, element and node limits, a marker on a
// cycle, and no user method ever called.
func renderFallback(v reflect.Value) string {
	var b strings.Builder
	r := renderer{path: map[refKey]bool{}, b: &b, nodes: &nodeBudget{left: pollRenderLimits.nodeBudget}}
	r.write(v, 0)
	return b.String()
}

// renderer prints a value with a depth limit, an element limit, a node budget and cycle detection. It
// reads values by kind, never through Interface, so it also works on unexported struct fields and never
// runs a String, Error, Format or GoString method.
type renderer struct {
	path  map[refKey]bool
	b     *strings.Builder
	nodes *nodeBudget
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
	if !r.nodes.take() {
		r.b.WriteString(truncationMarker)
		return
	}
	if key, ok := refIdentity(v); ok {
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
			if r.nodes.hit {
				break
			}
		}
		r.b.WriteString("}")
	case reflect.Struct:
		t := v.Type()
		r.b.WriteString(t.String() + "{")
		for i := 0; i < v.NumField(); i++ {
			if i > 0 {
				r.b.WriteString(", ")
			}
			if i == boundedMaxElems {
				fmt.Fprintf(r.b, "... %d more", v.NumField()-i)
				break
			}
			r.b.WriteString(t.Field(i).Name + ":")
			r.write(v.Field(i), depth+1)
			if r.nodes.hit {
				break
			}
		}
		r.b.WriteString("}")
	default:
		r.writeLeaf(v)
	}
}

// mapSortCap is the largest map whose keys are all rendered and sorted by their text. A larger map
// prints only its boundedMaxElems smallest keys in the total order of compareMapKeys, so the work stays
// bounded and the choice does not depend on map iteration order.
const mapSortCap = 1024

type renderedEntry struct {
	key  string
	pair mapEntry
}

func (r *renderer) writeMap(v reflect.Value, depth int) {
	r.b.WriteString(v.Type().String() + "{")
	var picked []mapEntry
	total := v.Len()
	if total > mapSortCap {
		picked, _ = smallestMapEntries(v, boundedMaxElems)
	} else {
		picked = rangeMapEntries(v, true, make([]mapEntry, 0, total))
	}
	entries := make([]renderedEntry, len(picked))
	for i, e := range picked {
		var kb strings.Builder
		kr := renderer{path: r.path, b: &kb, nodes: r.nodes}
		kr.write(e.key, depth+1)
		entries[i] = renderedEntry{kb.String(), e}
	}
	// Ties in the rendered key (NaN keys, keys that render alike) fall back to the shared key and value
	// order, so the output never depends on the order the runtime iterates the map.
	sort.SliceStable(entries, func(i, j int) bool {
		if c := strings.Compare(entries[i].key, entries[j].key); c != 0 {
			return c < 0
		}
		return compareMapEntries(entries[i].pair, entries[j].pair) < 0
	})
	for i, e := range entries {
		if i > 0 {
			r.b.WriteString(", ")
		}
		if i == boundedMaxElems {
			fmt.Fprintf(r.b, "... %d more", total-i)
			break
		}
		r.b.WriteString(e.key + ":")
		r.write(e.pair.val, depth+1)
		if r.nodes.hit {
			r.b.WriteString("}")
			return
		}
	}
	if len(entries) <= boundedMaxElems && total > len(entries) {
		fmt.Fprintf(r.b, ", ... %d more", total-len(entries))
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
