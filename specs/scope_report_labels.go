// scope_report_labels.go computes the report-Path label of every declared Describe/When scope
// (issue #275): distinct identity for sibling groups that share a literal name, so two specs
// declared under different same-named scopes never share a report Path.
//
// The exact format is documented in docs/DSL.md ("Duplicate sibling group names") — this file is
// its implementation, shared by all three build paths (bytecode compiler, arena/registry, Builder).
// Only the report Path changes: Go subtest identity (joinSubtestPath, FullNames, fullName) is
// computed from the literal declared names elsewhere and never touches this file.
package specs

import "strconv"

// disambiguateSiblingNames returns the report-Path label for each name in names — the literal
// declared names of one parent scope's sibling Describe/When groups, in declaration order — or nil
// when no name repeats, meaning every label is exactly the corresponding entry of names unchanged.
//
// The first group with a given name keeps that name. Each later group with the same name gets
// "name#k", the smallest integer k>=2 whose result is neither the literal name of another sibling in
// names nor a label this call already produced — so a literal Describe("D#2") never collides with
// the disambiguated label of a second Describe("D"), and a literal duplicate declared after the
// group it would otherwise collide with is still respected (docs/DSL.md's collision example).
func disambiguateSiblingNames(names []string) []string {
	if len(names) < 2 {
		return nil
	}
	counts := make(map[string]int, len(names))
	dup := false
	for _, n := range names {
		if n == "" {
			continue // an empty scope adds no segment, so it never collides (see joinSubtestPath)
		}
		counts[n]++
		if counts[n] > 1 {
			dup = true
		}
	}
	if !dup {
		return nil
	}
	literal := make(map[string]struct{}, len(names))
	for _, n := range names {
		literal[n] = struct{}{}
	}
	seen := make(map[string]int, len(names))
	assigned := make(map[string]struct{}, len(names))
	out := make([]string, len(names))
	for i, n := range names {
		if n == "" {
			continue
		}
		seen[n]++
		if seen[n] == 1 {
			out[i] = n
			assigned[n] = struct{}{}
			continue
		}
		for k := 2; ; k++ {
			cand := n + "#" + strconv.Itoa(k)
			_, isLiteral := literal[cand]
			_, isAssigned := assigned[cand]
			if !isLiteral && !isAssigned {
				out[i] = cand
				assigned[cand] = struct{}{}
				break
			}
		}
	}
	return out
}

// computeScopeLabels resolves the report-Path label of every scope a bytecode-compiler or Builder
// build recorded, in declaration order, indexed by scope-record id (see bytecodeCompiler.scopeParent/
// scopeDeclaredName and Builder.scopeParent/scopeDeclaredName). scopeParent[id] is the enclosing
// scope's id, or -1 for a scope with no enclosing scope in this compile (the compiler/builder's own
// root); scopeName[id] is that scope's literal declared name. Both build one entry per PushScope/
// Describe call, in declaration order, and are never truncated — unlike the stacks that describe only
// currently-open scopes — so every sibling of a parent is known here, however deeply the suite nests.
func computeScopeLabels(scopeParent []int, scopeName []string) []string {
	labels := append([]string(nil), scopeName...)
	childrenByParent := make(map[int][]int, len(scopeName))
	for id, p := range scopeParent {
		childrenByParent[p] = append(childrenByParent[p], id)
	}
	for _, ids := range childrenByParent {
		applySiblingLabels(labels, scopeName, ids)
	}
	return labels
}

// computeArenaGroupLabels is computeScopeLabels for the arena/registry build path, scoped to the
// subtree rooted at rootID: describeTopLevel compiles and runs each top-level Describe as soon as its
// own callback returns (see describeTopLevel), so a sibling top-level Describe declared afterward —
// sharing the arena's synthetic suite root — is not yet present when this one compiles and must never
// affect its labels; scoping to the subtree also means two unrelated top-level suites that happen to
// share a name are never treated as each other's siblings. Only Describe/When nodes participate: an
// It leaf's name is never disambiguated (issue #275 is scoped to groups, not specs — see docs/DSL.md).
func computeArenaGroupLabels(arena *NodeArena, rootID int) []string {
	if arena == nil || rootID < 0 || rootID >= len(arena.Nodes) {
		return nil
	}
	labels := make([]string, len(arena.Nodes))
	names := make([]string, len(arena.Nodes))
	for i := range arena.Nodes {
		labels[i] = arena.Nodes[i].Name
		names[i] = arena.Nodes[i].Name
	}
	childrenByParent := map[int][]int{}
	var visit func(id int)
	visit = func(id int) {
		for _, cid := range arena.Children[id] {
			if t := arena.Nodes[cid].Type; t == DescribeNode || t == WhenNode {
				childrenByParent[id] = append(childrenByParent[id], cid)
			}
			visit(cid)
		}
	}
	visit(rootID)
	for _, ids := range childrenByParent {
		applySiblingLabels(labels, names, ids)
	}
	return labels
}

// applySiblingLabels disambiguates one parent scope's children in place: ids are the sibling
// group-node/scope-record ids sharing one parent, in declaration order. A parent with fewer than two
// children (or whose children's names never repeat) is left untouched by disambiguateSiblingNames.
func applySiblingLabels(labels []string, names []string, ids []int) {
	if len(ids) < 2 {
		return
	}
	siblingNames := make([]string, len(ids))
	for i, id := range ids {
		siblingNames[i] = names[id]
	}
	resolved := disambiguateSiblingNames(siblingNames)
	if resolved == nil {
		return
	}
	for i, id := range ids {
		labels[id] = resolved[i]
	}
}

// resolveScopeIDs maps ids (a scope-record identity chain, outermost first — see
// bytecodeCompiler.scopeIDStack, Builder.scopeIDStack, hookGroup.ScopeIDs, specMark.scopeIDs) through
// labels (as returned by computeScopeLabels), returning the resolved report-Path segments. Returns
// nil for an empty chain (a spec, mark or group declared with no enclosing scope).
func resolveScopeIDs(ids []int, labels []string) []string {
	if len(ids) == 0 {
		return nil
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		// An empty scope adds no path element, matching its absence from the subtest name.
		if id >= 0 && id < len(labels) && labels[id] != "" {
			out = append(out, labels[id])
		}
	}
	return out
}
