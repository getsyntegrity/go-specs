// builder.go compiles the Describe/BeforeEach/AfterEach/It DSL into a flat Program.
// Hooks are resolved at compile time; the runner only runs steps in order.
package specs

import (
	"fmt"
	"slices"
)

// scope holds hooks for one Describe level. Root scope is at index 0.
//
// id is a monotonically increasing identifier assigned when the scope is pushed (Describe) or
// created (ensureScope) — never the scope's address. b.scopes is a slice with push/pop semantics
// (Describe appends then pops via defer), so sibling Describes at the same nesting depth reuse the
// same backing-array slot: &b.scopes[i] can be identical across two unrelated scopes that are never
// alive at the same time. hookKey used to key on that address, so two sibling scopes with the same
// hook counts collided and were silently coalesced into one group (#109). id is unique per scope for
// the life of the Builder, so it can't alias this way.
type scope struct {
	id         int
	beforeEach []step
	afterEach  []step
}

// specKind describes how a spec was registered (normal, skip, focus, parallel).
type specKind int

const (
	kindNormal specKind = iota
	kindSkip
	kindFocus
	kindParallel
)

// specItem is one registered spec (or skip). Before/after enable coalescing; hookKey identifies the scope set.
type specItem struct {
	kind specKind
	name string // the It/ItParallel name, for SpecStartEvent.Name; "" for skip (its steps are dropped anyway)
	// fullName is the Describe breadcrumb ending in name (see Builder.fullName), used only as this
	// spec's testing.T.Run identity (#102). Reported identity always stays name, never this.
	fullName string
	// scopeNames holds the enclosing Describe names, outermost first, exactly as declared — never
	// joined, unlike fullName. It feeds SpecStartEvent.Path (see group.specPath): a joined breadcrumb
	// cannot be split back into scopes without ambiguity when a declared name itself contains "/"
	// (the same defect #113/#114 fixed for the ExecutionPlan model's Path). Captured at registration
	// time as a copy, since Builder.scopeNames is a mutated stack that unwinds before finalize builds
	// groups — sharing its backing array here would alias sibling Describes' names.
	scopeNames []string
	before     []step
	spec       step // single spec body; nil for skip
	after      []step
	steps      []step // full sequence only for kindParallel (before+fn+after flattened)
	hookKey    string // from b.hookKey() for coalescing without comparing funcs
}

// Builder compiles a DSL into a Program. Use NewBuilder(), then Describe/BeforeEach/AfterEach/It, then Build().
type Builder struct {
	program *Program
	scopes  []scope
	// scopeNames holds the name of every enclosing Describe, outermost first. Only Describe pushes
	// here — ensureScope's implicit root scope is unnamed — so it can be shorter than scopes.
	scopeNames []string
	pending    []specItem
	hasFocus   bool
	// nextScopeID hands out each new scope's id (see scope.id), incremented every time a scope is
	// pushed. Never reused, even after Describe pops the scope back off b.scopes.
	nextScopeID int
}

// NewBuilder creates a builder that will produce a new Program.
// The optional capacity hint is ignored (kept for API compatibility with flat usage).
func NewBuilder(capacity ...int) *Builder {
	return &Builder{
		program:    &Program{Groups: nil},
		scopes:     nil,
		scopeNames: nil,
		pending:    nil,
		hasFocus:   false,
	}
}

// emitBefore returns current scope beforeEach in order (outer → inner).
func (b *Builder) emitBefore() []step {
	var out []step
	for i := range b.scopes {
		out = append(out, b.scopes[i].beforeEach...)
	}
	return out
}

// emitAfter returns afterEach in inner-to-outer order (innermost scope first).
// Within each scope, hooks are in declaration order. The runner iterates the
// result in reverse, so: flat case [after1, after2] => runs after2, after1 (LIFO);
// nested case [afterInner, afterOuter] => runs afterOuter, afterInner.
func (b *Builder) emitAfter() []step {
	var out []step
	for i := len(b.scopes) - 1; i >= 0; i-- {
		ae := b.scopes[i].afterEach
		for j := 0; j < len(ae); j++ {
			out = append(out, ae[j])
		}
	}
	return out
}

// emitSpecSteps returns the full step sequence for one spec (used for parallel runAll).
func (b *Builder) emitSpecSteps(fn func(*Context)) []step {
	before := b.emitBefore()
	after := b.emitAfter()
	steps := make([]step, 0, len(before)+1+len(after))
	steps = append(steps, before...)
	steps = append(steps, step(fn))
	steps = append(steps, after...)
	return steps
}

// newScope returns a scope with a fresh, unique id (see scope.id) and advances nextScopeID.
func (b *Builder) newScope() scope {
	id := b.nextScopeID
	b.nextScopeID++
	return scope{id: id}
}

// Describe opens a scope and runs body. Nested Describes push inner scopes; hooks apply to inner It.
func (b *Builder) Describe(name string, body func()) {
	if body == nil {
		return
	}
	b.scopes = append(b.scopes, b.newScope())
	b.scopeNames = append(b.scopeNames, name)
	// Both stacks unwind through defer so a panic in body cannot leave them out of step with each
	// other. A caller that recovers and keeps declaring would otherwise get breadcrumbs naming
	// scopes that already closed — silently wrong identity rather than a visible failure.
	defer func() {
		b.scopes = b.scopes[:len(b.scopes)-1]
		b.scopeNames = b.scopeNames[:len(b.scopeNames)-1]
	}()
	body()
}

// fullName returns the Describe breadcrumb ending in name — the same mapping the bytecode compiler
// applies (see compiler.fullName), so both sequential execution models give a spec the same Go
// subtest identity (#102). It is captured per spec at registration time, since the scope stack is
// unwound by the time finalize builds groups.
func (b *Builder) fullName(name string) string {
	return joinSubtestPath(b.scopeNames, name)
}

// ensureScope ensures at least one scope exists (for BeforeEach/AfterEach/It used without Describe).
func (b *Builder) ensureScope() {
	if len(b.scopes) == 0 {
		b.scopes = append(b.scopes, b.newScope())
	}
}

// BeforeEach registers a hook to run before each It in this scope (and nested scopes). Prepended before the spec.
func (b *Builder) BeforeEach(fn func(*Context)) {
	if fn == nil {
		return
	}
	b.ensureScope()
	idx := len(b.scopes) - 1
	b.scopes[idx].beforeEach = append(b.scopes[idx].beforeEach, step(fn))
}

// AfterEach registers a hook to run after each It in this scope (and nested scopes). Appended after the spec.
func (b *Builder) AfterEach(fn func(*Context)) {
	if fn == nil {
		return
	}
	b.ensureScope()
	idx := len(b.scopes) - 1
	b.scopes[idx].afterEach = append(b.scopes[idx].afterEach, step(fn))
}

// It compiles one spec: prepend all BeforeEach (outer to inner), append the spec, append all AfterEach (inner to outer).
// Order is deterministic; emitted at build time via pending.
// For a SpecFn produced by Skip or Focus, use ItWith instead.
func (b *Builder) It(name string, fn func(*Context)) {
	if fn == nil {
		return
	}
	b.ensureScope()
	b.pending = append(b.pending, specItem{
		kind:       kindNormal,
		name:       name,
		fullName:   b.fullName(name),
		scopeNames: slices.Clone(b.scopeNames),
		before:     b.emitBefore(),
		spec:       step(fn),
		after:      b.emitAfter(),
		hookKey:    b.hookKey(),
	})
}

// ItWith compiles one spec from a SpecFn, routing to the registration Skip/Focus asked for:
// Skip(fn) behaves like SkipIt(name, fn), Focus(fn) behaves like FIt(name, fn), and a plain SpecFn
// (constructed directly, with neither flag set) behaves like It(name, fn.Fn).
func (b *Builder) ItWith(name string, fn SpecFn) {
	if fn.Skip {
		b.SkipIt(name, fn.Fn)
		return
	}
	if fn.Focus {
		b.FIt(name, fn.Fn)
		return
	}
	b.It(name, fn.Fn)
}

// SkipIt registers a spec that is skipped at compile time: fn is never compiled into any step (it
// never runs, so it doesn't need to be a valid func — see Skip), but name is preserved so the
// compiled Program can still report the spec's identity as skipped. See finalize.
func (b *Builder) SkipIt(name string, fn func(*Context)) {
	b.ensureScope()
	b.pending = append(b.pending, specItem{kind: kindSkip, name: name, scopeNames: slices.Clone(b.scopeNames)})
}

// FIt registers a focused spec. If any spec is focused, only focused specs are compiled into the program.
func (b *Builder) FIt(name string, fn func(*Context)) {
	if fn == nil {
		return
	}
	b.ensureScope()
	b.hasFocus = true
	b.pending = append(b.pending, specItem{
		kind:       kindFocus,
		name:       name,
		fullName:   b.fullName(name),
		scopeNames: slices.Clone(b.scopeNames),
		before:     b.emitBefore(),
		spec:       step(fn),
		after:      b.emitAfter(),
		hookKey:    b.hookKey(),
	})
}

// ItParallel registers a spec to run in parallel with adjacent ItParallel specs; grouped into one step at build time.
// fn runs on its own *Context; ctx.T is nil (exposing the shared *testing.T would not be safe for
// concurrent use), so use ctx.Expect(...) rather than ctx.T directly. A fatal assertion still stops
// the rest of fn, same as a sequential It — it just stops that one goroutine instead of the process.
// Every ItParallel spec in the group always runs to completion; FailFast only takes effect at the
// next group, it cannot cancel a sibling ItParallel spec mid-group.
func (b *Builder) ItParallel(name string, fn func(*Context)) {
	if fn == nil {
		return
	}
	b.ensureScope()
	b.pending = append(b.pending, specItem{kind: kindParallel, name: name, scopeNames: slices.Clone(b.scopeNames), steps: b.emitSpecSteps(fn)})
}

// hookKey returns a key that uniquely identifies the current scope stack and hook set.
// Same scope stack (same Describe path) => same key, so specs in the same Describe coalesce.
//
// Uses each scope's monotonic id (see scope.id), not its address: b.scopes is a slice with
// push/pop semantics, so &b.scopes[i] is reused across sibling Describes at the same depth once an
// earlier sibling has closed — two unrelated scopes could share the same address and, with matching
// hook counts, the same key, silently coalescing their specs into one group (#109). id is unique for
// the life of the Builder, so this can't happen; hook lengths are still folded in only to keep the
// key legible for debugging, not for uniqueness.
func (b *Builder) hookKey() string {
	if len(b.scopes) == 0 {
		return ""
	}
	var buf []byte
	for i := range b.scopes {
		s := &b.scopes[i]
		lb, la := len(s.beforeEach), len(s.afterEach)
		buf = append(buf, fmt.Sprintf("%d,%d,%d;", s.id, lb, la)...)
	}
	return string(buf)
}

// finalize builds program.Groups from pending: focus filter, coalesce same-hook specs, group
// parallel, and attach skip names to whichever group they end up nearest to.
//
// A kindSkip item never becomes a step (it has no before/after/spec — SkipIt never captures them),
// so it can't form its own execution unit the way a real spec does. Instead its name is buffered in
// pendingSkips and attached to the next group finalize creates or appends to (normal/focus/parallel
// alike), then the buffer is cleared. This deliberately does NOT give skip its own group: doing so
// would insert a group boundary between two coalesced same-hookKey specs (e.g. It("a"), SkipIt("b"),
// It("c") in the same Describe), which would make their shared before/after run twice instead of
// once — see TestProgram_SkipRemoval / TestSkipRemovesSpecs, which pin the coalesced-group-count
// invariant this must not break. Any pendingSkips left over once every item is processed (trailing
// skips, or an all-skip Describe) become their own skip-only group at the end.
func (b *Builder) finalize() {
	items := b.pending
	if b.hasFocus {
		filtered := items[:0]
		for i := range items {
			if items[i].kind == kindFocus {
				filtered = append(filtered, items[i])
			}
		}
		items = filtered
	}
	var groups []group
	curIdx := -1
	var pendingSkips []string
	var pendingSkipScopeNames [][]string
	for i := 0; i < len(items); i++ {
		it := items[i]
		if it.kind == kindSkip {
			pendingSkips = append(pendingSkips, it.name)
			pendingSkipScopeNames = append(pendingSkipScopeNames, it.scopeNames)
			continue
		}
		if it.kind == kindParallel {
			curIdx = -1
			parSteps := []step{runAll(it.steps)}
			parNames := []string{it.name}
			parScopeNames := [][]string{it.scopeNames}
			j := i + 1
			for j < len(items) && items[j].kind == kindParallel {
				parSteps = append(parSteps, runAll(items[j].steps))
				parNames = append(parNames, items[j].name)
				parScopeNames = append(parScopeNames, items[j].scopeNames)
				j++
			}
			// names is left nil: this group's single spec is the synthetic parallelStep closure,
			// which reports each real ItParallel spec itself (via ctx.execObserver) as it runs on
			// its own goroutine. Runner.Run's sequential per-spec reporting only fires when names
			// is populated, so it correctly stays out of the way here instead of double-reporting.
			groups = append(groups, group{specs: []step{parallelStep(parSteps, parNames, parScopeNames)}, skipped: pendingSkips, skippedScopeNames: pendingSkipScopeNames})
			pendingSkips = nil
			pendingSkipScopeNames = nil
			i = j - 1
			continue
		}
		// normal or focus: coalesce if same hook set (same scope layout) as current group
		if curIdx >= 0 && groups[curIdx].hookKey == it.hookKey {
			groups[curIdx].specs = append(groups[curIdx].specs, it.spec)
			groups[curIdx].names = append(groups[curIdx].names, it.name)
			groups[curIdx].fullNames = append(groups[curIdx].fullNames, it.fullName)
			groups[curIdx].scopeNames = append(groups[curIdx].scopeNames, it.scopeNames)
			groups[curIdx].skipped = append(groups[curIdx].skipped, pendingSkips...)
			groups[curIdx].skippedScopeNames = append(groups[curIdx].skippedScopeNames, pendingSkipScopeNames...)
			pendingSkips = nil
			pendingSkipScopeNames = nil
		} else {
			groups = append(groups, group{
				before:            it.before,
				specs:             []step{it.spec},
				names:             []string{it.name},
				fullNames:         []string{it.fullName},
				scopeNames:        [][]string{it.scopeNames},
				after:             it.after,
				hookKey:           it.hookKey,
				skipped:           pendingSkips,
				skippedScopeNames: pendingSkipScopeNames,
			})
			pendingSkips = nil
			pendingSkipScopeNames = nil
			curIdx = len(groups) - 1
		}
	}
	if len(pendingSkips) > 0 {
		groups = append(groups, group{skipped: pendingSkips, skippedScopeNames: pendingSkipScopeNames})
	}
	b.program.Groups = groups
}

// Build returns the compiled program. Safe to call multiple times; do not modify the returned Program's Groups.
func (b *Builder) Build() *Program {
	b.finalize()
	return b.program
}
