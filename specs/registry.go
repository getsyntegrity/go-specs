package specs

import (
	"bytes"
	"fmt"
	"io"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

type NodeType int

const (
	SuiteNode NodeType = iota
	DescribeNode
	WhenNode
	ItNode
)

// Node is the legacy pointer-based node type. The registry now uses NodeArena (index-based);
// Node is retained for reference and for any external use of SuiteTree that may still expect it.
type Node struct {
	Name        string
	Type        NodeType
	Children    []*Node
	Parent      *Node
	Fn          func(*Context)
	File        string
	Line        int
	PathGen     *PathGenerator
	BeforeHooks []func(*Context)
	AfterHooks  []func(*Context)
	BeforeAll   []func(*Context)
	AfterAll    []func(*Context)
}

// registry holds an arena and a stack of node indices. All nodes are stored in the arena.
type registry struct {
	mu    sync.Mutex
	arena *NodeArena
	stack []int
}

// registryStack holds one Analyze/Describe registry stack per goroutine. A flat, ungoroutine-scoped
// stack would let two goroutines building unrelated suites concurrently (e.g. two t.Parallel() tests
// each calling Analyze or top-level Describe) observe and mutate each other's top-of-stack registry —
// mutex-protected against low-level data races, but still logically wrong, since "current registry"
// is meant to track a single call chain, not whichever goroutine pushed most recently process-wide.
type registryStack struct {
	mu     sync.Mutex
	stacks map[int64][]*registry
}

var activeRegistries = registryStack{stacks: map[int64][]*registry{}}

// goroutineID extracts the numeric goroutine id from runtime.Stack's leading "goroutine N [state]:"
// line. Used only at Analyze/Describe/BuildSuite entry points to key the per-goroutine registry
// stack — never in the hot per-spec execution path — so its cost is one Analyze/Describe call, not
// one per It/BeforeEach/AfterEach.
func goroutineID() int64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	b := buf[:n]
	const prefix = "goroutine "
	if !bytes.HasPrefix(b, []byte(prefix)) {
		return 0
	}
	b = b[len(prefix):]
	if i := bytes.IndexByte(b, ' '); i >= 0 {
		b = b[:i]
	}
	id, err := strconv.ParseInt(string(b), 10, 64)
	if err != nil {
		return 0
	}
	return id
}

// initialArenaCap pre-sizes arena slices to avoid reallocations in large suites (e.g. 2000 specs).
const initialArenaCap = 4096

func newRegistry() *registry {
	arena := &NodeArena{
		Nodes:       make([]ArenaNode, 0, initialArenaCap),
		Children:    make([][]int, 0, initialArenaCap),
		BeforeHooks: make([][]func(*Context), 0, initialArenaCap),
		AfterHooks:  make([][]func(*Context), 0, initialArenaCap),
	}
	// Root node: suite, index 0
	arena.Nodes = append(arena.Nodes, ArenaNode{Name: "suite", Type: SuiteNode, Parent: -1})
	arena.Children = append(arena.Children, nil)
	arena.BeforeHooks = append(arena.BeforeHooks, nil)
	arena.AfterHooks = append(arena.AfterHooks, nil)
	return &registry{arena: arena, stack: []int{0}}
}

func (r *registry) currentSuite() *SuiteTree {
	r.mu.Lock()
	defer r.mu.Unlock()
	return &SuiteTree{Arena: r.arena, RootID: 0}
}

func (r *registry) enterNode(nodeType NodeType, name, file string, line int, fn func(*Context)) (int, func()) {
	r.mu.Lock()
	parentID := r.stack[len(r.stack)-1]
	id := len(r.arena.Nodes)
	r.arena.Nodes = append(r.arena.Nodes, ArenaNode{
		Name: name, Parent: parentID, Type: nodeType, Fn: fn,
		File: file, Line: line,
	})
	r.arena.Children = append(r.arena.Children, nil)
	r.arena.BeforeHooks = append(r.arena.BeforeHooks, nil)
	r.arena.AfterHooks = append(r.arena.AfterHooks, nil)
	r.arena.Children[parentID] = append(r.arena.Children[parentID], id)
	r.stack = append(r.stack, id)
	r.mu.Unlock()
	return id, func() {
		r.mu.Lock()
		if len(r.stack) > 1 {
			r.stack = r.stack[:len(r.stack)-1]
		}
		r.mu.Unlock()
	}
}

func (r *registry) appendBeforeHook(fn func(*Context)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.stack) == 0 {
		return
	}
	id := r.stack[len(r.stack)-1]
	r.arena.BeforeHooks[id] = append(r.arena.BeforeHooks[id], fn)
}

func (r *registry) appendAfterHook(fn func(*Context)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.stack) == 0 {
		return
	}
	id := r.stack[len(r.stack)-1]
	r.arena.AfterHooks[id] = append(r.arena.AfterHooks[id], fn)
}

func (r *registry) setPathGen(gen *PathGenerator) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.stack) == 0 {
		return
	}
	id := r.stack[len(r.stack)-1]
	if id < len(r.arena.Nodes) {
		r.arena.Nodes[id].PathGen = gen
	}
}

func pushRegistry(r *registry) func() {
	gid := goroutineID()
	activeRegistries.mu.Lock()
	activeRegistries.stacks[gid] = append(activeRegistries.stacks[gid], r)
	activeRegistries.mu.Unlock()
	return func() {
		activeRegistries.mu.Lock()
		if s := activeRegistries.stacks[gid]; len(s) > 0 {
			if len(s) == 1 {
				delete(activeRegistries.stacks, gid)
			} else {
				activeRegistries.stacks[gid] = s[:len(s)-1]
			}
		}
		activeRegistries.mu.Unlock()
	}
}

func currentRegistry() *registry {
	gid := goroutineID()
	activeRegistries.mu.Lock()
	defer activeRegistries.mu.Unlock()
	s := activeRegistries.stacks[gid]
	if len(s) == 0 {
		return nil
	}
	return s[len(s)-1]
}

// ensureRegistry pushes a new registry if none is active; call the returned func to pop.
// Used by Describe when called without Analyze() so the tree has a registry to build into.
func ensureRegistry() func() {
	if currentRegistry() != nil {
		return func() {}
	}
	return pushRegistry(newRegistry())
}

// CurrentArena returns the current registry's arena, or nil if none is active.
func CurrentArena() *NodeArena {
	reg := currentRegistry()
	if reg == nil {
		return nil
	}
	reg.mu.Lock()
	a := reg.arena
	reg.mu.Unlock()
	return a
}

// AppendBeforeHook appends a before-each hook to the current node (stack top). No-op if no registry.
func AppendBeforeHook(fn func(*Context)) {
	reg := currentRegistry()
	if reg != nil {
		reg.appendBeforeHook(fn)
	}
}

// AppendAfterHook appends an after-each hook to the current node. No-op if no registry.
func AppendAfterHook(fn func(*Context)) {
	reg := currentRegistry()
	if reg != nil {
		reg.appendAfterHook(fn)
	}
}

// SetPathGen sets the PathGenerator on the current node. Used by path specs.
func SetPathGen(gen *PathGenerator) {
	reg := currentRegistry()
	if reg != nil {
		reg.setPathGen(gen)
	}
}

// Analyze builds a suite tree by running fn with a fresh registry pushed for the calling goroutine.
// Safe to call concurrently from multiple goroutines (e.g. from several t.Parallel() tests): each
// call gets its own registry, scoped to the goroutine that called Analyze, so concurrent calls never
// observe each other's tree.
func Analyze(fn func()) *SuiteTree {
	reg := newRegistry()
	pop := pushRegistry(reg)
	defer pop()
	if fn != nil {
		fn()
	}
	return reg.currentSuite()
}

func CurrentSuite() *SuiteTree {
	it := currentRegistry()
	if it == nil {
		return nil
	}
	return it.currentSuite()
}

func enterAnalyzeNode(nodeType NodeType, name, file string, line int, fn func(*Context)) (int, func()) {
	reg := currentRegistry()
	if reg == nil {
		return -1, func() {}
	}
	return reg.enterNode(nodeType, name, file, line, fn)
}

// PrintTree prints the pointer-based node tree (legacy).
func PrintTree(node *Node, depth int, w io.Writer) {
	if node == nil {
		return
	}
	indent := strings.Repeat("  ", depth)
	if w == nil {
		w = io.Discard
	}
	_, _ = fmt.Fprintf(w, "%s%s\n", indent, node.Name)
	for _, child := range node.Children {
		PrintTree(child, depth+1, w)
	}
}

// PrintTreeArena prints the arena-based tree from rootID. Skips suite root (id 0) children when rootID is 0.
func PrintTreeArena(arena *NodeArena, rootID int, depth int, w io.Writer) {
	if arena == nil || rootID < 0 || rootID >= len(arena.Nodes) {
		return
	}
	if w == nil {
		w = io.Discard
	}
	indent := strings.Repeat("  ", depth)
	_, _ = fmt.Fprintf(w, "%s%s\n", indent, arena.Nodes[rootID].Name)
	for _, cid := range arena.Children[rootID] {
		PrintTreeArena(arena, cid, depth+1, w)
	}
}

func Walk(node *Node, fn func(*Node)) {
	if node == nil || fn == nil {
		return
	}
	fn(node)
	for _, child := range node.Children {
		Walk(child, fn)
	}
}

// CaptureCallerLocation controls whether file/line are captured for nodes (Describe, When, It).
// When false (default), callerLocation returns "", 0 without calling runtime.Caller, saving
// ~21% of runner allocations. Set to true when locations are needed (e.g. IDE, tree printing).
var CaptureCallerLocation bool

func callerLocation(skip int) (string, int) {
	if !CaptureCallerLocation {
		return "", 0
	}
	_, file, line, ok := runtime.Caller(skip)
	if !ok {
		return "", 0
	}
	return file, line
}
