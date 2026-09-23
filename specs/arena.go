// arena.go defines the index-based tree used by the registry for Analyze mode.
package specs

// NodeArena holds all nodes and hooks in flat slices; children are index references.
type NodeArena struct {
	Nodes       []ArenaNode
	Children    [][]int
	BeforeHooks [][]func(*Context)
	AfterHooks  [][]func(*Context)
	// BeforeAllHooks/AfterAllHooks hold a node's own once-per-group hooks (issue #207,
	// docs/SUITE_HOOKS_CONTRACT.md): unlike BeforeHooks/AfterHooks, these are never flattened onto
	// descendant It nodes — they belong to exactly the node they were registered on (H1) — and are
	// read directly by buildExecutionPlanFromArenaRec when it closes a Describe/When node.
	BeforeAllHooks [][]func(*Context)
	AfterAllHooks  [][]func(*Context)
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
