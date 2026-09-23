// arena.go defines the index-based tree used by the registry for Analyze mode.
package specs

// NodeArena holds all nodes and hooks in flat slices; children are index references.
//
// It carries no BeforeAll/AfterAll storage (issue #207): NodeArena is allocated for every
// Analyze-built suite and is exactly 96 bytes, the top of its allocation size class, so a field
// here would cost every suite bytes it never asked for (docs/SUITE_HOOKS_CONTRACT.md H10). A
// node's once-per-group hooks live in the registry's lazily allocated arenaGroupHooks instead.
type NodeArena struct {
	Nodes       []ArenaNode
	Children    [][]int
	BeforeHooks [][]func(*Context)
	AfterHooks  [][]func(*Context)
}

// ArenaNode is one node in the arena (Describe/When/It).
type ArenaNode struct {
	Name   string
	Parent int
	Type   NodeType
	Fn     func(*Context)
	File   string
	Line   int
}

// arenaGroupHooks holds the once-per-group hooks registered on arena nodes, indexed by node ID
// (issue #207). Unlike NodeArena.BeforeHooks/AfterHooks these are never flattened onto descendant
// It nodes — they belong to exactly the node they were registered on (H1) — and are read directly
// by buildExecutionPlanFromArenaRec when it closes a Describe/When node. Both slices are grown
// lazily, only as far as the highest node ID that registered a hook, and the struct itself is
// allocated only by the first BeforeAll/AfterAll a registry sees.
type arenaGroupHooks struct {
	before [][]func(*Context)
	after  [][]func(*Context)
}

// of returns node id's own BeforeAll/AfterAll hooks. A nil receiver — no group hook registered
// anywhere — and an id past either slice both mean "none".
func (h *arenaGroupHooks) of(id int) (before, after []func(*Context)) {
	if h == nil || id < 0 {
		return nil, nil
	}
	if id < len(h.before) {
		before = h.before[id]
	}
	if id < len(h.after) {
		after = h.after[id]
	}
	return before, after
}

// appendArenaGroupHook appends fn to (*hooks)[id], growing *hooks so id is a valid index.
func appendArenaGroupHook(hooks *[][]func(*Context), id int, fn func(*Context)) {
	if id < 0 {
		return
	}
	for len(*hooks) <= id {
		*hooks = append(*hooks, nil)
	}
	(*hooks)[id] = append((*hooks)[id], fn)
}
