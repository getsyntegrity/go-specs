package assert

import (
	"math"
	"reflect"
	"strconv"
	"strings"
)

// The recipe interpreter behind the diagnostic fuzz targets (fuzz_diagnostics_test.go).
//
// A fuzz input is a byte string, the recipe, and fzBuild turns it into a small graph of Go values.
// The graph is a pure function of the bytes, so a failing input reproduces the exact same shapes, and
// it can be rebuilt to check that a diagnostic does not depend on addresses or on map iteration order.
//
// Recipe format (every read past the end of the data yields 0, so any input decodes):
//
//	byte 0      n, the node count: 1 + b % 64
//	byte 1      operand A, "actual":   node b % n
//	byte 2      operand B, "expected": node b % n
//	then n x 2  one (kind, param) pair per node; the kind is b % fkKinds
//	then, for each node in order, fzKidCount(kind, param) child records of 2 bytes: (key, target)
//	tail        free parameters the fuzz targets read with g.rd (lengths, poll timing, ...)
//
// A child's target is node b % n, and nothing stops it from naming the node itself or an earlier one,
// which is how a recipe builds cycles and shared references. The key byte picks a map key (see fzKey)
// and is ignored by slices. The decoder never allocates more than the limits below: at most 64
// nodes, and the widest kinds are bounded (a 2045 entry map, a 25,500 element []int, a 1,024 byte
// string, a 256 deep pointer chain), so one iteration stays fast. Callers skip data longer than
// fzMaxData.
const (
	fzMaxData  = 512
	fzMaxNodes = 64
)

type fzKind byte

const (
	fkNil fzKind = iota
	fkInt
	fkStr
	fkLongStr   // 4..1024 bytes, multi-byte runes on odd params
	fkNaN       // float64 NaN with a payload taken from the param
	fkFloat     // ordinary float64, including -0
	fkBool      // bool
	fkErr       // *fzErr, an error that wraps a value
	fkStringer  // fzStringer value, a struct with a String method
	fkPub       // *fzPub, exported fields
	fkMix       // *fzMix, an unexported field
	fkMapAny    // map[any]any with heterogeneous keys
	fkMapStr    // map[string]any
	fkMapFloat  // map[float64]any, NaN keys allowed
	fkMapInt    // map[int]any
	fkSlice     // []any with spare capacity
	fkView      // a view of another slice node: same backing array, another length
	fkPtr       // *any
	fkWideMap   // map[int]any over the sorted-render cap
	fkBigInts   // []int over the node budget
	fkChain     // a chain of up to 256 *any that ends in a child
	fkWideSlice // []any wider than the element limit, every element the same child
	fkArray     // [3]any
	fkTree      // *fzTree
	fkKinds
)

// Fixed test struct types. fzMix has an unexported field, fzStringer a String method, fzErr an Error
// method; none of the methods reads the value it wraps, so a user method can never be the reason a
// diagnostic recursed.
type (
	fzPub struct {
		A, B any
		N    int
	}
	fzMix struct {
		Pub  any
		priv any
		S    string
	}
	fzStringer struct{ V any }
	fzErr      struct{ v any }
	fzTree     struct {
		L, R *fzTree
		V    any
	}
	fzKeyStruct struct {
		F float64
		S string
	}
)

func (fzStringer) String() string { return "fzStringer" }
func (*fzErr) Error() string      { return "fzErr" }

type fzReader struct {
	data []byte
	pos  int
}

func (r *fzReader) next() byte {
	if r.pos >= len(r.data) {
		return 0
	}
	b := r.data[r.pos]
	r.pos++
	return b
}

// fzKidCount is how many (key, target) child records a node of this kind and param reads.
func fzKidCount(k fzKind, p byte) int {
	switch k {
	case fkErr, fkStringer, fkPtr, fkChain, fkView, fkWideMap, fkWideSlice:
		return 1
	case fkPub, fkMix:
		return 2
	case fkTree, fkArray:
		return 3
	case fkMapAny, fkMapStr, fkMapFloat, fkMapInt, fkSlice:
		return 1 + int(p)%24
	}
	return 0
}

type fzNode struct {
	kind  fzKind
	p     byte
	kids  [][2]byte
	val   any // set for scalars and reference kinds; other kinds are built on demand by fzGraph.value
	set   bool
	count int // entries of a wide map

	mAny   map[any]any
	mStr   map[string]any
	mFloat map[float64]any
	mInt   map[int]any
	arr    []any
	ptr    *any
	chain  []*any
	pub    *fzPub
	mix    *fzMix
	tree   *fzTree
	err    *fzErr
}

// fzGraph is a decoded recipe: the nodes, the two operands and the reader positioned at the tail.
type fzGraph struct {
	n, a, b  int
	nodes    []fzNode
	rd       *fzReader // positioned at the tail; use tail() for a fresh reader
	addrKeys bool      // some map is keyed by a pointer: its text may legitimately depend on addresses
	data     []byte    // the recipe
	twins    map[[2]byte]*fzGraph
	heavy    int // elements the wide kinds may still allocate in this graph
}

// fzHeavyBudget bounds the elements of every wide map and big slice in one graph, so a recipe that
// names many of them still builds in milliseconds; the first ones get their full size.
const fzHeavyBudget = 30_000

func (g *fzGraph) spend(n int) int {
	n = min(n, g.heavy)
	g.heavy -= n
	return n
}

func (g *fzGraph) actual() any   { return g.value(g.a, 0) }
func (g *fzGraph) expected() any { return g.value(g.b, 0) }

func (g *fzGraph) target(b byte) int { return int(b) % g.n }

// nanFromByte returns a NaN whose payload and sign come from x, so recipes can hold several NaNs that
// differ only in their bit pattern.
func nanFromByte(x byte) float64 {
	bits := uint64(0x7ff0000000000001) + uint64(x&0x3f)*0x101
	if x&0x40 != 0 {
		bits |= 1 << 63
	}
	return math.Float64frombits(bits)
}

func fzBuild(data []byte) *fzGraph {
	rd := &fzReader{data: data}
	n := 1 + int(rd.next())%fzMaxNodes
	g := &fzGraph{n: n, rd: rd, data: data, nodes: make([]fzNode, n), heavy: fzHeavyBudget}
	g.a, g.b = g.target(rd.next()), g.target(rd.next())
	for i := range g.nodes {
		g.nodes[i].kind = fzKind(rd.next()) % fkKinds
		g.nodes[i].p = rd.next()
	}
	for i := range g.nodes {
		nd := &g.nodes[i]
		for j := 0; j < fzKidCount(nd.kind, nd.p); j++ {
			nd.kids = append(nd.kids, [2]byte{rd.next(), rd.next()})
		}
	}
	for i := range g.nodes {
		g.allocate(&g.nodes[i])
	}
	for i := range g.nodes {
		g.wire(&g.nodes[i])
	}
	return g
}

// allocate creates the node's own storage. A reference kind gets its identity here, before any node
// is wired, so a child may name it whatever its index.
func (g *fzGraph) allocate(nd *fzNode) {
	nd.set = true
	p := int(nd.p)
	switch nd.kind {
	case fkNil:
		nd.val = nil
	case fkInt:
		nd.val = int(int8(nd.p))
	case fkStr:
		nd.val = ""
		if p%9 != 0 {
			nd.val = "s" + strconv.Itoa(p%8)
		}
	case fkLongStr:
		size := (p + 1) * 4
		if p%2 == 1 {
			nd.val = strings.Repeat("é", size/2)
		} else {
			nd.val = strings.Repeat("x", size)
		}
	case fkNaN:
		nd.val = nanFromByte(nd.p)
	case fkFloat:
		if p == 0 {
			nd.val = math.Copysign(0, -1)
		} else {
			nd.val = float64(int8(nd.p)) / 4
		}
	case fkBool:
		nd.val = p%2 == 0
	case fkErr:
		nd.err = &fzErr{}
		nd.val = nd.err
	case fkPub:
		nd.pub = &fzPub{N: p}
		nd.val = nd.pub
	case fkMix:
		nd.mix = &fzMix{S: "mix"}
		nd.val = nd.mix
	case fkTree:
		nd.tree = &fzTree{}
		nd.val = nd.tree
	case fkMapAny:
		nd.mAny = map[any]any{}
		nd.val = nd.mAny
	case fkMapStr:
		nd.mStr = map[string]any{}
		nd.val = nd.mStr
	case fkMapFloat:
		nd.mFloat = map[float64]any{}
		nd.val = nd.mFloat
	case fkMapInt:
		nd.mInt = map[int]any{}
		nd.val = nd.mInt
	case fkSlice:
		c := fzKidCount(nd.kind, nd.p)
		nd.arr = make([]any, c+(p/24)%4)
		nd.val = nd.arr[:c]
	case fkPtr:
		nd.ptr = new(any)
		nd.val = nd.ptr
	case fkWideMap:
		nd.count = g.spend(1025 + p*4)
		if p%2 == 0 {
			nd.mAny = map[any]any{}
			nd.val = nd.mAny
		} else {
			nd.mInt = make(map[int]any)
			nd.val = nd.mInt
		}
	case fkBigInts:
		s := make([]int, g.spend(p*100))
		for i := range s {
			s[i] = i
		}
		nd.val = s
	case fkChain:
		nd.chain = make([]*any, 1+p%256)
		for i := range nd.chain {
			nd.chain[i] = new(any)
		}
		for i := 0; i+1 < len(nd.chain); i++ {
			*nd.chain[i] = nd.chain[i+1]
		}
		nd.val = nd.chain[0]
	case fkWideSlice:
		nd.arr = make([]any, 17+p%48)
		nd.val = nd.arr
	default: // fkStringer, fkView, fkArray: values built on demand
		nd.set = false
	}
}

// value returns the node's value. A value kind (a struct or array copy, a slice view) is built on
// demand from its children; depth stops a chain of value kinds that names itself.
func (g *fzGraph) value(i, depth int) any {
	nd := &g.nodes[i]
	if nd.set {
		return nd.val
	}
	if depth > 6 {
		return nil
	}
	switch nd.kind {
	case fkStringer:
		return fzStringer{V: g.value(g.target(nd.kids[0][1]), depth+1)}
	case fkView:
		t := &g.nodes[g.target(nd.kids[0][1])]
		if t.kind != fkSlice {
			return []any(nil)
		}
		return t.arr[:int(nd.p)%(cap(t.arr)+1)]
	case fkArray:
		var arr [3]any
		for j, k := range nd.kids {
			arr[j] = g.value(g.target(k[1]), depth+1)
		}
		return arr
	}
	return nil
}

func (g *fzGraph) treeOf(b byte) *fzTree {
	if t := &g.nodes[g.target(b)]; t.kind == fkTree {
		return t.tree
	}
	return nil
}

// wire fills the node's contents from its children.
func (g *fzGraph) wire(nd *fzNode) {
	child := func(j int) any { return g.value(g.target(nd.kids[j][1]), 0) }
	switch nd.kind {
	case fkErr:
		nd.err.v = child(0)
	case fkPub:
		nd.pub.A, nd.pub.B = child(0), child(1)
	case fkMix:
		nd.mix.Pub, nd.mix.priv = child(0), child(1)
	case fkTree:
		nd.tree.L, nd.tree.R, nd.tree.V = g.treeOf(nd.kids[0][1]), g.treeOf(nd.kids[1][1]), child(2)
	case fkMapAny:
		for j, k := range nd.kids {
			nd.mAny[g.fzKey(nd.kind, k[0])] = child(j)
		}
	case fkMapStr:
		for j, k := range nd.kids {
			nd.mStr[g.fzKey(nd.kind, k[0]).(string)] = child(j)
		}
	case fkMapFloat:
		for j, k := range nd.kids {
			nd.mFloat[g.fzKey(nd.kind, k[0]).(float64)] = child(j)
		}
	case fkMapInt:
		for j, k := range nd.kids {
			nd.mInt[g.fzKey(nd.kind, k[0]).(int)] = child(j)
		}
	case fkSlice:
		for j := range nd.kids {
			nd.arr[j] = child(j)
		}
	case fkPtr:
		*nd.ptr = child(0)
	case fkWideMap:
		v := child(0)
		for i, count := 0, nd.count; i < count; i++ {
			var val any = i
			if i%3 == 0 {
				val = v
			}
			if nd.mAny == nil {
				nd.mInt[i] = val
				continue
			}
			nd.mAny[wideKey(i)] = val
		}
	case fkChain:
		*nd.chain[len(nd.chain)-1] = child(0)
	case fkWideSlice:
		v := child(0)
		for i := range nd.arr {
			nd.arr[i] = v
		}
	}
}

// wideKey is the key of entry i of an even-param wide map: keys of every ordered kind, so the
// selection of the smallest keys of a large map has to compare across kinds, NaN keys included.
func wideKey(i int) any {
	switch i % 7 {
	case 0:
		return i
	case 1:
		return "s" + strconv.Itoa(i)
	case 2:
		return float64(i) / 2
	case 3:
		return nanFromByte(byte(i))
	case 4:
		return i%2 == 0
	case 5:
		return uint(i)
	}
	return fzKeyStruct{F: float64(i % 9), S: "a"}
}

// fzKey picks a map key from the key byte. Keys are always hashable. NaN keys never equal
// themselves, so every insert adds an entry; equal payloads give entries whose keys tie. A
// map[any]any also takes struct and array keys that hold a NaN, and, for kb%8 == 7, a fresh pointer:
// such a map is ordered by address by design, so the graph is flagged addrKeys and the rebuild
// invariant skips it.
func (g *fzGraph) fzKey(kind fzKind, kb byte) any {
	switch kind {
	case fkMapStr:
		return "k" + strconv.Itoa(int(kb%8))
	case fkMapInt:
		return int(kb % 16)
	case fkMapFloat:
		if kb%4 == 0 {
			return nanFromByte(kb >> 2)
		}
		return float64(kb % 8)
	}
	switch kb % 8 {
	case 0:
		return int(kb >> 3)
	case 1:
		return "k" + strconv.Itoa(int(kb>>3))
	case 2:
		return nanFromByte(kb >> 3)
	case 3:
		return fzKeyStruct{F: nanFromByte(kb >> 3), S: "s"}
	case 4:
		return [1]float64{nanFromByte(kb >> 3)}
	case 5:
		return kb&8 != 0
	case 6:
		return float64(kb>>3) / 2
	}
	g.addrKeys = true
	return new(int)
}

// shape lists the length of every container node, so a test can tell that a diagnostic did not add
// or drop an entry.
func (g *fzGraph) shape() []int {
	out := make([]int, len(g.nodes))
	for i := range g.nodes {
		nd := &g.nodes[i]
		switch {
		case nd.mAny != nil:
			out[i] = len(nd.mAny)
		case nd.mStr != nil:
			out[i] = len(nd.mStr)
		case nd.mFloat != nil:
			out[i] = len(nd.mFloat)
		case nd.mInt != nil:
			out[i] = len(nd.mInt)
		case nd.arr != nil:
			out[i] = len(nd.arr)
			if nd.kind == fkSlice {
				out[i] = len(nd.val.([]any))*1000 + cap(nd.arr)
			}
		case nd.chain != nil:
			out[i] = len(nd.chain)
		}
	}
	return out
}

// --- oracle -----------------------------------------------------------------------------------

// fzUnrolled counts the values a naive traversal visits, following every reference again each time
// it is met and stopping once the count passes limit. It has no cycle detection on purpose: a cycle
// simply exceeds the limit, which is how the tests know a value is infinite. A slice or array of
// scalars costs its length, like the production budget. The count is an oracle written apart from
// safeForFmt, so a mistake in one does not hide a mistake in the other.
func fzUnrolled(v reflect.Value, limit int) int {
	n := 0
	var walk func(v reflect.Value)
	walk = func(v reflect.Value) {
		if n > limit || !v.IsValid() {
			return
		}
		n++
		switch v.Kind() {
		case reflect.Pointer, reflect.Interface:
			if !v.IsNil() {
				walk(v.Elem())
			}
		case reflect.Map:
			for it := v.MapRange(); n <= limit && it.Next(); {
				walk(it.Key())
				walk(it.Value())
			}
		case reflect.Slice, reflect.Array:
			switch v.Type().Elem().Kind() {
			case reflect.Bool, reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
				reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64:
				n += v.Len()
				return
			}
			for i := 0; i < v.Len() && n <= limit; i++ {
				walk(v.Index(i))
			}
		case reflect.Struct:
			for i := 0; i < v.NumField() && n <= limit; i++ {
				walk(v.Field(i))
			}
		}
	}
	walk(v)
	return n
}

// fzHugeNodes is the size above which a value is certainly over every renderer's budget: four times
// the largest node budget, so the oracle and the production count may differ by a factor without
// the test guessing which side of the line a borderline value falls on.
const fzHugeNodes = 4 * boundedNodeBudget

// fzInfinite reports whether v unrolls past fzHugeNodes: a cycle, or a graph too large to print.
func fzInfinite(v any) bool { return fzUnrolled(reflect.ValueOf(v), fzHugeNodes) > fzHugeNodes }

// --- recipe encoder (seeds and regressions) ------------------------------------------------------

// fzRB writes a recipe. Its methods panic on a malformed seed, which fails the test that uses it.
type fzRB struct {
	a, b  byte
	nodes []fzNode
	tail  []byte
}

func fzKid(key, target byte) [2]byte { return [2]byte{key, target} }

// node appends a node and returns its index; the caller must give exactly fzKidCount(kind, p) kids.
func (r *fzRB) node(kind fzKind, p byte, kids ...[2]byte) byte {
	if len(kids) != fzKidCount(kind, p) {
		panic("fzRB: wrong child count for kind " + strconv.Itoa(int(kind)) + " param " + strconv.Itoa(int(p)))
	}
	r.nodes = append(r.nodes, fzNode{kind: kind, p: p, kids: kids})
	return byte(len(r.nodes) - 1)
}

func (r *fzRB) operands(a, b byte) *fzRB { r.a, r.b = a, b; return r }

func (r *fzRB) withTail(tail ...byte) *fzRB { r.tail = tail; return r }

func (r *fzRB) bytes() []byte {
	if len(r.nodes) == 0 || len(r.nodes) > fzMaxNodes {
		panic("fzRB: node count out of range")
	}
	out := []byte{byte(len(r.nodes) - 1), r.a, r.b}
	for _, nd := range r.nodes {
		out = append(out, byte(nd.kind), nd.p)
	}
	for _, nd := range r.nodes {
		for _, k := range nd.kids {
			out = append(out, k[0], k[1])
		}
	}
	return append(out, r.tail...)
}

// Named recipes used by the unit tests and as seeds.

// fzRecipeSelfMap: a map[string]any whose entry "k0" is the map itself and "k1" an int.
func fzRecipeSelfMap() []byte {
	r := &fzRB{}
	r.node(fkMapStr, 1, fzKid(0, 0), fzKid(1, 1))
	r.node(fkInt, 7)
	return r.bytes()
}

// fzRecipeNaNTies: a map[float64]any with two NaN entries of the same payload (their keys tie), one
// of a second and one of a third payload; the values are different ints, and the last is a map that
// holds the root, so the tied entries differ only by a value that reaches a cycle.
func fzRecipeNaNTies() []byte {
	r := &fzRB{}
	r.node(fkMapFloat, 3, fzKid(0, 1), fzKid(0, 3), fzKid(4, 2), fzKid(8, 1))
	r.node(fkInt, 1)
	r.node(fkInt, 2)
	r.node(fkMapFloat, 0, fzKid(0, 0))
	return r.bytes()
}

// fzRecipeManyNaNTies is the shape of finding 2 (TestFuzzFindingTieBreakFingerprintCost): a
// map[float64]any with 24 NaN entries of one bit pattern, every one holding an array that holds the
// map again and two copies of a smaller such array. Fingerprinting those values used to take seconds
// per message; the recipes no longer cap how many entries of a map tie.
func fzRecipeManyNaNTies() []byte {
	r := &fzRB{}
	kids := make([][2]byte, 24)
	for i := range kids {
		kids[i] = fzKid(0, 1)
	}
	r.node(fkMapFloat, 23, kids...)
	r.node(fkArray, 0, fzKid(0, 0), fzKid(0, 2), fzKid(0, 2))
	r.node(fkArray, 0, fzKid(0, 0), fzKid(0, 3), fzKid(0, 3))
	r.node(fkInt, 1)
	return r.bytes()
}

// fzRecipeViews: node 0 is a 4 element slice whose first element is a full-length view of itself;
// nodes 1 and 2 are views of it with 2 and 4 elements: the same address, different lengths.
func fzRecipeViews() []byte {
	r := &fzRB{}
	r.node(fkSlice, 3, fzKid(0, 2), fzKid(0, 3), fzKid(0, 3), fzKid(0, 3))
	r.node(fkView, 2, fzKid(0, 0))
	r.node(fkView, 4, fzKid(0, 0))
	r.node(fkInt, 5)
	return r.bytes()
}

// fzRecipeLimits: one node of the given kind and param whose children all name node 0 itself.
func fzRecipeLimits(kind fzKind, p byte) []byte {
	r := &fzRB{}
	kids := make([][2]byte, fzKidCount(kind, p))
	r.node(kind, p, kids...)
	return r.bytes()
}

// fzRecipePtrKey: a map[any]any with one pointer key.
func fzRecipePtrKey() []byte {
	r := &fzRB{}
	r.node(fkMapAny, 0, fzKid(7, 0))
	return r.bytes()
}

// tail returns a reader over the bytes after the graph. Every run of a diagnostic takes its own, so
// running one twice on the same graph reads the same parameters.
func (g *fzGraph) tail() *fzReader { return &fzReader{data: g.rd.data, pos: g.rd.pos} }

// twin returns the graph of this recipe with one leaf node changed, so its operands have the same
// shape as this graph's and differ in one scalar: what an equality failure between two values of one
// type looks like, and what the structural diff walks field by field. tl supplies the node and the
// change; a node that is not a leaf is left alone and the twin is an exact copy (equal, at other
// addresses). A recipe shorter than its own node table has nothing to change.
func (g *fzGraph) twin(tl *fzReader) *fzGraph {
	i, x := int(tl.next())%g.n, tl.next()|1
	// One twin per change, built once: a diagnostic run again on this graph must see the same twin, at
	// the same addresses, or the relative address order of two pointer keys could change between runs.
	if t, ok := g.twins[[2]byte{byte(i), x}]; ok {
		return t
	}
	data := append([]byte(nil), g.data...)
	kind, param := 3+2*i, 3+2*i+1
	if param < len(data) {
		switch fzKind(data[kind]) % fkKinds {
		case fkInt, fkStr, fkNaN, fkFloat, fkBool, fkPub:
			data[param] ^= x
		}
	}
	t := fzBuild(data)
	if g.twins == nil {
		g.twins = map[[2]byte]*fzGraph{}
	}
	g.twins[[2]byte{byte(i), x}] = t
	// The twin's pointer keys are other objects than this graph's: the order of a key of one against
	// a key of the other is by address, so a graph with a pointer-keyed twin is address-ordered too.
	g.addrKeys = g.addrKeys || t.addrKeys
	return t
}

// operandB is the second operand of a diagnostic, chosen by one tail byte: node b of this graph, or
// node a or b of its twin. It returns the graph the operand lives in (g itself when there is no twin).
func (g *fzGraph) operandB(tl *fzReader) (any, *fzGraph) {
	switch tl.next() % 3 {
	case 1:
		t := g.twin(tl)
		return t.value(g.a, 0), t
	case 2:
		t := g.twin(tl)
		return t.value(g.b, 0), t
	}
	return g.expected(), g
}
