package specs

import (
	"strings"
	"sync"
	"testing"
	"unsafe"
)

// group_hook_cost_test.go pins docs/SUITE_HOOKS_CONTRACT.md H10 at the struct level: a suite that
// registers no BeforeAll/AfterAll must not allocate a single extra byte for the feature.
//
// testing.AllocsPerRun counts allocations, not bytes, so it cannot see a struct that merely grew:
// the first draft of #207 added three slice headers to ExecutionPlan and two to NodeArena, kept the
// allocation count at exactly 426 in BenchmarkDescribeVariant_Describe, and still cost every suite
// ~96 B/op, because ExecutionPlan grew from 192 to 264 bytes and so moved from the 192-byte to the
// 288-byte allocation size class.
//
// The invariant pinned here is therefore "no always-allocated struct changes allocation size class":
//
//   - ExecutionPlan and NodeArena keep their exact develop sizes (192 and 96 bytes). Both already sit
//     exactly on a size-class boundary, so even one extra pointer (+8) would move them up a class
//     and cost every suite 16 bytes. They carry no group-hook field at all.
//   - Group-hook storage lives behind one pointer on CompiledSuite and one on registry, both of which
//     had 8 bytes of slack in their size class on develop (56 of 64, 40 of 48). That pointer stays
//     nil — nothing behind it is allocated — until a BeforeAll/AfterAll is actually registered.
//
// The sizes are the 64-bit ones; on a 32-bit platform the test is skipped rather than re-derived.
func TestGroupHookStorageAddsNoBytesToAlwaysAllocatedStructs(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("sizes are pinned for 64-bit platforms")
	}
	exact := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"ExecutionPlan", unsafe.Sizeof(ExecutionPlan{}), 192},
		{"NodeArena", unsafe.Sizeof(NodeArena{}), 96},
	}
	for _, c := range exact {
		if c.got != c.want {
			t.Errorf("unsafe.Sizeof(%s{}) = %d, want %d (its develop size): group-hook storage must not live on it (H10)", c.name, c.got, c.want)
		}
	}
	sameClass := []struct {
		name string
		got  uintptr
		max  uintptr
	}{
		{"CompiledSuite", unsafe.Sizeof(CompiledSuite{}), 64},
		{"registry", unsafe.Sizeof(registry{}), 48},
	}
	for _, c := range sameClass {
		if c.got > c.max {
			t.Errorf("unsafe.Sizeof(%s{}) = %d, want <= %d (the allocation size class it had on develop)", c.name, c.got, c.max)
		}
	}
}

// TestSuiteWithoutGroupHooksAllocatesNoGroupStorage proves the lazily allocated pointers really stay
// nil for a suite that registers neither hook, on both compile paths.
func TestSuiteWithoutGroupHooksAllocatesNoGroupStorage(t *testing.T) {
	body := func(s *Spec) {
		s.BeforeEach(func(*Context) {})
		s.When("nested", func(w *Spec) { w.It("spec", func(*Context) {}) })
	}
	if got := BuildSuite(nil, "compiler path", body); got.groups != nil {
		t.Fatalf("bytecode-compiler suite without group hooks has group storage %+v, want nil", got.groups)
	}
	var analyzed *CompiledSuite
	Analyze(func() { analyzed = BuildSuite(nil, "registry path", body) })
	if analyzed == nil || analyzed.groups != nil {
		t.Fatalf("registry suite without group hooks has group storage, want nil: %+v", analyzed)
	}

	hooked := BuildSuite(nil, "hooked", func(s *Spec) {
		s.BeforeAll(func(*Context) {})
		s.It("spec", func(*Context) {})
	})
	if hooked.groups == nil {
		t.Fatal("a suite that registers a BeforeAll has no group storage")
	}
}

// TestDescribeEntryPointsAllocateNoGroupStorageWithoutHooks extends the H10 guard above to the entry
// points that build and run a suite in one call (Describe, DescribeWithReporter, and Describe inside
// Analyze), which never hand the CompiledSuite back: suiteRunObserver sees it right before it runs.
func TestDescribeEntryPointsAllocateNoGroupStorageWithoutHooks(t *testing.T) {
	seen := map[string]*CompiledSuite{}
	var mu sync.Mutex
	observe := func(s *CompiledSuite) {
		mu.Lock()
		defer mu.Unlock()
		if strings.HasPrefix(s.Name, "h10 ") {
			seen[s.Name] = s
		}
	}
	suiteRunObserver.Store(&observe)
	defer suiteRunObserver.Store(nil)

	plain := func(s *Spec) {
		s.BeforeEach(func(*Context) {})
		s.When("nested", func(w *Spec) { w.It("spec", func(*Context) {}) })
	}
	hooked := func(s *Spec) {
		s.BeforeAll(func(*Context) {})
		s.It("spec", func(*Context) {})
	}
	Describe(t, "h10 describe", plain)
	DescribeWithReporter(t, "h10 reporter", &recordingReporter{}, plain)
	Analyze(func() { Describe(t, "h10 analyze", plain) })
	Describe(t, "h10 hooked describe", hooked)
	Analyze(func() { Describe(t, "h10 hooked analyze", hooked) })

	for _, name := range []string{"h10 describe", "h10 reporter", "h10 analyze"} {
		s, ok := seen[name]
		if !ok {
			t.Fatalf("suite %q never ran", name)
		}
		if s.groups != nil {
			t.Errorf("suite %q without group hooks has group storage %+v, want nil (H10)", name, s.groups)
		}
	}
	for _, name := range []string{"h10 hooked describe", "h10 hooked analyze"} {
		if s, ok := seen[name]; !ok || s.groups == nil {
			t.Errorf("suite %q registers a BeforeAll but ran without group storage", name)
		}
	}
}
