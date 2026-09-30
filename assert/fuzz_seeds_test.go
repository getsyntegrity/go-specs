package assert

// The seed recipes of every fuzz target. They are also the dangerous shapes the subprocess runner
// replays (fuzz_subprocess_test.go): each one is a value that used to, or could, overflow the stack,
// loop, or print unstably if a renderer forgot a cycle, a limit or a tie.

type fzSeed struct {
	name string
	data []byte
}

func fzSeeds() []fzSeed {
	return []fzSeed{
		{"empty", []byte{}},
		{"self-map", fzRecipeSelfMap()},
		{"nan-ties", fzRecipeNaNTies()},
		{"slice-views", fzRecipeViews()},
		{"pointer-key", fzRecipePtrKey()},
		{"wide-map", fzRecipeLimits(fkWideMap, 200)},
		{"big-ints", fzRecipeLimits(fkBigInts, 255)},
		{"long-string", fzRecipeLimits(fkLongStr, 255)},
		{"long-string-multibyte", fzRecipeLimits(fkLongStr, 201)},
		{"deep-chain-cycle", fzRecipeLimits(fkChain, 255)},
		{"deep-chain-acyclic", fzRecipeDeepChain()},
		{"wide-slice", fzRecipeWideSlice(false)},
		{"wide-slice-cycle", fzRecipeWideSlice(true)},
		{"self-slice", fzRecipeSelfSlice()},
		{"struct-cycle", fzRecipeStructCycle()},
		{"stringer-cycle", fzRecipeStringerCycle()},
		{"error-wrapping-cycle", fzRecipeErrorCycle()},
		{"tree-cycle", fzRecipeTreeCycle()},
		{"array-holding-cycle", fzRecipeArrayCycle()},
		{"nested-heterogeneous-maps", fzRecipeNestedMaps()},
		{"actual-and-expected-differ-by-cycle", fzRecipeTwoCycles()},
		{"wide-map-int-keys", fzRecipeLimits(fkWideMap, 201)},
		{"twin-struct-cycle", fzWithTail(fzRecipeStructCycle(), 1, 0, 2)},
		{"twin-self-slice", fzWithTail(fzRecipeSelfSlice(), 1, 1, 6)},
		{"twin-nested-maps", fzWithTail(fzRecipeNestedMaps(), 1, 4, 2)},
		{"twin-nan-ties", fzWithTail(fzRecipeNaNTies(), 2, 1, 4)},
		{"twin-tree-cycle", fzWithTail(fzRecipeTreeCycle(), 1, 1, 8)},
		{"twin-of-a-map-that-holds-itself", fzWithTail(fzRecipeSelfMap(), 1, 1, 2)},
		{"twin-array-holding-cycle", fzWithTail(fzRecipeArrayCycle(), 1, 2, 3)},
		{"poll-eventually-times-out", fzRecipeSelfMapPoll(0, 1, 1, 6, 0, 0)},
		{"poll-consistently-mismatch", fzRecipeSelfMapPoll(1, 2, 0, 9, 3, 0)},
		{"poll-callback-panics-with-a-cycle", fzRecipeSelfMapPoll(0, 1, 2, 11, 0, 2)},
		{"poll-eventually-matches", fzRecipeSelfMapPoll(0, 1, 0, 8, 3, 0)},
	}
}

// fzRecipeDeepChain: 200 chained pointers ending in an int: acyclic, far past the depth limits.
func fzRecipeDeepChain() []byte {
	r := &fzRB{}
	r.node(fkChain, 199, fzKid(0, 1))
	r.node(fkInt, 3)
	return r.bytes()
}

// fzRecipeWideSlice: a []any of 17..64 elements that are all one child, the child being either a
// string or the slice itself.
func fzRecipeWideSlice(cyclic bool) []byte {
	r := &fzRB{}
	target := byte(1)
	if cyclic {
		target = 0
	}
	r.node(fkWideSlice, 47, fzKid(0, target))
	r.node(fkStr, 2)
	return r.bytes()
}

// fzRecipeSelfSlice: a []any whose elements are itself, an int and itself again.
func fzRecipeSelfSlice() []byte {
	r := &fzRB{}
	r.node(fkSlice, 2, fzKid(0, 0), fzKid(0, 1), fzKid(0, 0))
	r.node(fkInt, 4)
	return r.bytes()
}

// fzRecipeStructCycle: an exported struct whose fields hold itself and a struct with an unexported
// field that points back at it.
func fzRecipeStructCycle() []byte {
	r := &fzRB{}
	r.node(fkPub, 5, fzKid(0, 0), fzKid(0, 1))
	r.node(fkMix, 0, fzKid(0, 0), fzKid(0, 0))
	return r.bytes()
}

// fzRecipeStringerCycle: a pointer to a value whose type has a String method and that holds the pointer.
func fzRecipeStringerCycle() []byte {
	r := &fzRB{}
	r.node(fkPtr, 0, fzKid(0, 1))
	r.node(fkStringer, 0, fzKid(0, 0))
	return r.bytes()
}

// fzRecipeErrorCycle: an error that wraps a map that holds the error. Operand A is the error, B the map.
func fzRecipeErrorCycle() []byte {
	r := &fzRB{}
	r.node(fkErr, 0, fzKid(0, 1))
	r.node(fkMapStr, 0, fzKid(0, 0))
	return r.operands(0, 1).bytes()
}

// fzRecipeTreeCycle: a tree node that is its own left and right child.
func fzRecipeTreeCycle() []byte {
	r := &fzRB{}
	r.node(fkTree, 0, fzKid(0, 0), fzKid(0, 0), fzKid(0, 1))
	r.node(fkInt, 1)
	return r.bytes()
}

// fzRecipeArrayCycle: a slice whose element is an array that holds the slice.
func fzRecipeArrayCycle() []byte {
	r := &fzRB{}
	r.node(fkSlice, 0, fzKid(0, 1))
	r.node(fkArray, 0, fzKid(0, 0), fzKid(0, 0), fzKid(0, 2))
	r.node(fkInt, 9)
	return r.bytes()
}

// fzRecipeNestedMaps: a map[any]any with int, string, NaN, struct-with-NaN, array-with-NaN and bool
// keys, whose values are nested maps (one with two same-payload NaN keys) that reach back to the root.
func fzRecipeNestedMaps() []byte {
	r := &fzRB{}
	r.node(fkMapAny, 5,
		fzKid(0, 1), fzKid(9, 2), fzKid(18, 1), fzKid(27, 2), fzKid(36, 3), fzKid(5, 0))
	r.node(fkMapStr, 1, fzKid(0, 2), fzKid(1, 0))
	r.node(fkMapFloat, 1, fzKid(0, 4), fzKid(0, 5))
	r.node(fkMapInt, 2, fzKid(0, 0), fzKid(1, 1), fzKid(2, 2))
	r.node(fkInt, 1)
	r.node(fkStr, 3)
	return r.bytes()
}

// fzRecipeTwoCycles: A and B are two different self-containing maps, so an equality failure walks
// two cyclic operands at once.
func fzRecipeTwoCycles() []byte {
	r := &fzRB{}
	r.node(fkMapStr, 1, fzKid(0, 0), fzKid(1, 2))
	r.node(fkMapStr, 1, fzKid(0, 1), fzKid(1, 2))
	r.node(fkInt, 6)
	return r.operands(0, 1).bytes()
}

// fzRecipeSelfMapPoll: a self-containing map as the polled value (and, for a panic, the panic value),
// with the tail read by FuzzPollMessage: mode, interval, advance, timeout, match/fail attempt, panic attempt.
func fzRecipeSelfMapPoll(mode, interval, advance, timeout, at, panicAt byte) []byte {
	r := &fzRB{}
	r.node(fkMapStr, 1, fzKid(0, 0), fzKid(1, 1))
	r.node(fkInt, 7)
	return r.withTail(mode, interval, advance, timeout, at, panicAt).bytes()
}

// fzWithTail appends tail bytes to a recipe. A diagnostic that compares two values reads, first, the
// twin selector: 0 for node b of the same graph, 1 or 2 for node a or b of a twin followed by the
// node to change and how (see fzGraph.twin).
func fzWithTail(recipe []byte, tail ...byte) []byte { return append(recipe, tail...) }
