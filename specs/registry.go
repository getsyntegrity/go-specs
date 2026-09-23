package specs

import (
	"bytes"
	"fmt"
	"io"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

type NodeType int

const (
	SuiteNode NodeType = iota
	DescribeNode
	WhenNode
	ItNode
)

// registry holds an arena and a stack of node indices. All nodes are stored in the arena.
type registry struct {
	mu    sync.Mutex
	arena *NodeArena
	stack []int
	// groupHooks holds BeforeAll/AfterAll registrations (issue #207), nil until the first one. It
	// lives here rather than on NodeArena so a suite without group hooks allocates nothing for it
	// (docs/SUITE_HOOKS_CONTRACT.md H10); registry had 8 bytes of slack in its allocation size class
	// on develop, so this pointer costs nothing either (group_hook_cost_test.go).
	groupHooks *arenaGroupHooks
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

// currentNodeIDLocked returns the node the DSL is currently building into (the stack top). Callers
// must hold r.mu.
//
// Stack invariant: r.stack is never empty. newRegistry seeds it with the suite root (index 0), the
// pop closure returned by enterNode only shrinks it while len(r.stack) > 1, and registry is
// unexported and constructed only by newRegistry, so no zero-value registry can reach this method.
// The panic is therefore unreachable in correct code. It exists because the alternative the hook and
// path-generator writers previously used — returning silently on an empty stack — would turn a
// broken invariant into a discarded registration and a suite that reports green having registered
// nothing, which is the failure this file is meant to make impossible (issue #151). enterNode
// indexed the stack top unguarded while the other three guarded it; routing all four through here
// makes the invariant one statement instead of four inconsistent ones.
func (r *registry) currentNodeIDLocked() int {
	if len(r.stack) == 0 {
		panic("specs: registry node stack is empty; registry invariant violated")
	}
	return r.stack[len(r.stack)-1]
}

func (r *registry) enterNode(nodeType NodeType, name, file string, line int, fn func(*Context)) (int, func()) {
	r.mu.Lock()
	parentID := r.currentNodeIDLocked()
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
	id := r.currentNodeIDLocked()
	r.arena.BeforeHooks[id] = append(r.arena.BeforeHooks[id], fn)
}

func (r *registry) appendAfterHook(fn func(*Context)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := r.currentNodeIDLocked()
	r.arena.AfterHooks[id] = append(r.arena.AfterHooks[id], fn)
}

// appendBeforeAllHook adds a once-per-group setup hook to the current node (issue #207). Unlike
// appendBeforeHook, this hook is never flattened onto descendant It nodes — see arenaGroupHooks.
func (r *registry) appendBeforeAllHook(fn func(*Context)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	appendArenaGroupHook(&r.groupHooksLocked().before, r.currentNodeIDLocked(), fn)
}

// appendAfterAllHook adds a once-per-group teardown hook to the current node (issue #207).
func (r *registry) appendAfterAllHook(fn func(*Context)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	appendArenaGroupHook(&r.groupHooksLocked().after, r.currentNodeIDLocked(), fn)
}

// groupHooksLocked returns r.groupHooks, allocating it on first use. r.mu must be held.
func (r *registry) groupHooksLocked() *arenaGroupHooks {
	if r.groupHooks == nil {
		r.groupHooks = &arenaGroupHooks{}
	}
	return r.groupHooks
}

// groupHooksOf returns r's group hooks, or nil for a nil registry or one without any.
func (r *registry) groupHooksOf() *arenaGroupHooks {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.groupHooks
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

// The Analyze extension surface
//
// Analyze, CurrentSuite, CurrentArena, AppendBeforeHook and AppendAfterHook are a
// deliberate, supported extension API, not legacy residue: they let a custom DSL build a SuiteTree
// against the registry without going through Describe. Analyze establishes the build context;
// everything else reads or mutates the registry Analyze pushed for the calling goroutine.
//
// Their contract is fail-closed. A mutating helper called with no active registry has nowhere to
// write, so it panics instead of discarding the registration and letting a suite that registered
// nothing report green (issue #151). The read-only accessors (CurrentSuite, CurrentArena) return
// nil outside Analyze, because "no suite is being built" is a legitimate answer to a question, not
// a lost write.
//
// Valid context means "inside the fn passed to Analyze, or inside a Describe/BuildSuite nested in
// one", on the same goroutine: the registry stack is keyed per goroutine, so a helper called from a
// goroutine started inside Analyze sees no registry and panics.

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

// requireRegistry returns the registry the calling goroutine is building into, and panics naming
// the helper when there is none. A mutating extension helper has nowhere to record its argument
// outside Analyze, so failing here is the only way to keep a suite that registered nothing from
// reporting green.
func requireRegistry(helper string) *registry {
	reg := currentRegistry()
	if reg == nil {
		panic("specs: " + helper + " called with no active registry; call it inside Analyze(fn) on the same goroutine")
	}
	return reg
}

// AppendBeforeHook appends a before-each hook to the current node (stack top).
// Panics when called outside Analyze: see "The Analyze extension surface" above.
func AppendBeforeHook(fn func(*Context)) {
	requireRegistry("AppendBeforeHook").appendBeforeHook(fn)
}

// AppendAfterHook appends an after-each hook to the current node.
// Panics when called outside Analyze: see "The Analyze extension surface" above.
func AppendAfterHook(fn func(*Context)) {
	requireRegistry("AppendAfterHook").appendAfterHook(fn)
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

// CurrentSuite returns a SuiteTree view over the registry active on the calling goroutine, or nil
// when none is active. With CurrentArena, AppendBeforeHook and AppendAfterHook it forms
// the supported extension surface for code that builds into the registry Analyze or Describe pushed
// — an external DSL or a generator — without needing the unexported registry type.
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

// PrintTreeArena writes the arena-backed declaration tree rooted at rootID to w, one node name per
// line, indented two spaces per level. It is the supported way to inspect a built suite's shape from
// outside the package — debugging a Describe/When/It nesting, or rendering the tree in tooling — and
// it is the only tree printer, because the arena is the live node representation. Pass rootID 0 for
// the suite root. A nil arena, an out-of-range rootID, and a nil w (treated as io.Discard) are all
// no-ops rather than panics, so a possibly-absent tree can be printed unguarded.
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

// captureCallerLocation controls whether file/line are captured for nodes (Describe, When, It).
// It is atomic because suite construction is safe across concurrent goroutines: a consumer may
// flip the toggle while another goroutine is declaring specs.
var captureCallerLocation atomic.Bool

// SetCaptureCallerLocation enables or disables caller-location capture for declaration nodes
// (Describe, When, It). When disabled (the default), callerLocation returns "", 0 without
// calling runtime.Caller, saving ~21% of runner allocations. Enable it when ArenaNode.File and
// ArenaNode.Line are needed (e.g. IDE integration, tree printing). It is safe to call from any
// goroutine, including while suites are being declared.
func SetCaptureCallerLocation(enabled bool) {
	captureCallerLocation.Store(enabled)
}

// CaptureCallerLocationEnabled reports whether caller-location capture is currently enabled.
// It is safe to call from any goroutine.
func CaptureCallerLocationEnabled() bool {
	return captureCallerLocation.Load()
}

func callerLocation(skip int) (string, int) {
	if !captureCallerLocation.Load() {
		return "", 0
	}
	_, file, line, ok := runtime.Caller(skip)
	if !ok {
		return "", 0
	}
	return file, line
}
