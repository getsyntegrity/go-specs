package specs

import "strings"

// SuiteTree represents an analyzed spec tree (arena-based).
type SuiteTree struct {
	Arena  *NodeArena
	RootID int
}

// Walk visits every node in depth-first order. The callback receives the node index;
// use s.Arena.Nodes[id] and s.Arena.Children[id] to read node data.
func (s *SuiteTree) Walk(fn func(id int)) {
	if s == nil || s.Arena == nil || fn == nil {
		return
	}
	walkArena(s.Arena, s.RootID, fn)
}

func walkArena(arena *NodeArena, id int, fn func(int)) {
	if id < 0 || id >= len(arena.Nodes) {
		return
	}
	fn(id)
	for _, cid := range arena.Children[id] {
		walkArena(arena, cid, fn)
	}
}

// Tree returns a human-readable representation of the suite.
func (s *SuiteTree) Tree() string {
	if s == nil || s.Arena == nil {
		return ""
	}
	var b strings.Builder
	rootID := s.RootID
	if rootID >= 0 && rootID < len(s.Arena.Nodes) && s.Arena.Nodes[rootID].Type == SuiteNode {
		for _, cid := range s.Arena.Children[rootID] {
			writeSuiteTreeArena(&b, s.Arena, cid, 0)
		}
	} else {
		writeSuiteTreeArena(&b, s.Arena, rootID, 0)
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func writeSuiteTreeArena(b *strings.Builder, arena *NodeArena, id int, depth int) {
	if id < 0 || id >= len(arena.Nodes) {
		return
	}
	for i := 0; i < depth; i++ {
		b.WriteString("  ")
	}
	b.WriteString(arena.Nodes[id].Name)
	b.WriteByte('\n')
	for _, cid := range arena.Children[id] {
		writeSuiteTreeArena(b, arena, cid, depth+1)
	}
}
