// arena.go defines the index-based tree used by the registry for Analyze mode.
package specs

// NodeArena holds all nodes and hooks in flat slices; children are index references.
type NodeArena struct {
	Nodes       []ArenaNode
	Children    [][]int
	BeforeHooks [][]func(*Context)
	AfterHooks  [][]func(*Context)
}

// ArenaNode is one node in the arena (Describe/When/It).
type ArenaNode struct {
	Name     string
	Parent   int
	Type     NodeType
	Fn       func(*Context)
	File     string
	Line     int
	PathGen  *PathGenerator
}
