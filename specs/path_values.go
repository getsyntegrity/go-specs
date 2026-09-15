package specs

import (
	"math"
	"sync"
)

// pathValuesPool recycles PathValues during path execution to reduce allocations.
// New pre-sizes values/present so FillPathValues(reset) often reuses buffers without allocating.
const pathValuesInitialCap = 16

var pathValuesPool = sync.Pool{
	New: func() any {
		return &PathValues{
			values:  make([]any, 0, pathValuesInitialCap),
			present: make([]bool, 0, pathValuesInitialCap),
		}
	},
}

// PathValues provides helper accessors for filters and ctx.Path().
type PathValues struct {
	values  []any
	present []bool
	index   map[string]int
}

// reset clears the PathValues and sets the index for reuse. Index is read-only and shared.
// values and present slices are reused; they are resized or reallocated if needed for nKeys.
func (pv *PathValues) reset(index map[string]int) {
	if pv == nil {
		return
	}
	pv.index = index
	n := len(index)
	if n == 0 {
		if pv.values != nil {
			pv.values = pv.values[:0]
		}
		if pv.present != nil {
			pv.present = pv.present[:0]
		}
		return
	}
	if cap(pv.values) < n {
		pv.values = make([]any, n)
		pv.present = make([]bool, n)
		return
	}
	pv.values = pv.values[:n]
	pv.present = pv.present[:n]
	for i := 0; i < n; i++ {
		pv.values[i] = nil
		pv.present[i] = false
	}
}

func (pv PathValues) assignTo(dst *PathValues) {
	if dst == nil {
		return
	}
	if len(pv.values) == 0 {
		dst.values = nil
		dst.present = nil
		dst.index = nil
		return
	}
	if len(dst.values) != len(pv.values) {
		dst.values = make([]any, len(pv.values))
		dst.present = make([]bool, len(pv.present))
	} else {
		for i := range dst.values {
			dst.values[i] = nil
		}
		for i := range dst.present {
			dst.present[i] = false
		}
	}
	copy(dst.values, pv.values)
	copy(dst.present, pv.present)
	dst.index = pv.index
}

func (pv PathValues) clone() PathValues {
	if len(pv.values) == 0 {
		return PathValues{}
	}
	values := make([]any, len(pv.values))
	present := make([]bool, len(pv.present))
	copy(values, pv.values)
	copy(present, pv.present)
	return PathValues{
		values:  values,
		present: present,
		index:   pv.index,
	}
}

func (pv PathValues) lookup(name string) (any, bool) {
	if pv.index == nil {
		return nil, false
	}
	i, ok := pv.index[name]
	if !ok || i >= len(pv.present) {
		return nil, false
	}
	if !pv.present[i] {
		return nil, false
	}
	return pv.values[i], true
}

// Bool returns the boolean value for the provided variable name.
func (pv PathValues) Bool(name string) bool {
	val, ok := pv.lookup(name)
	if !ok {
		return false
	}
	if b, ok := val.(bool); ok {
		return b
	}
	return false
}

// Int returns the integer value for the provided variable name.
func (pv PathValues) Int(name string) int {
	val, ok := pv.lookup(name)
	if !ok {
		return 0
	}
	switch v := val.(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	}
	return 0
}

// Value returns the raw value for the variable.
func (pv PathValues) Value(name string) any {
	val, ok := pv.lookup(name)
	if !ok {
		return nil
	}
	return val
}

func (pv PathValues) len() int {
	count := 0
	for _, ok := range pv.present {
		if ok {
			count++
		}
	}
	return count
}

// Hash returns a deterministic hash of the path values for coverage tracking.
func (pv PathValues) Hash() uint64 {
	var h uint64 = 14695981039346656037 // FNV-1a offset
	for i := 0; i < len(pv.values) && i < len(pv.present); i++ {
		if !pv.present[i] {
			continue
		}
		h ^= uint64(i)
		h *= 1099511628211
		v := pv.values[i]
		switch x := v.(type) {
		case int:
			h ^= uint64(x)
		case int64:
			h ^= uint64(x)
		case int32:
			h ^= uint64(x)
		case bool:
			if x {
				h ^= 1
			}
		case uint:
			h ^= uint64(x)
		case uint64:
			h ^= x
		case string:
			for j := 0; j < len(x); j++ {
				h ^= uint64(x[j])
				h *= 1099511628211
			}
		case float64:
			h ^= math.Float64bits(x)
		case float32:
			h ^= uint64(math.Float32bits(x))
		default:
			h ^= uint64(i) * 31
		}
		h *= 1099511628211
	}
	return h
}
