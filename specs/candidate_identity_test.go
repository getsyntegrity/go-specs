package specs

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"unsafe"
)

// cartesianGen builds a CartesianMode generator over the named string dimensions, in declaration
// order, so the tests below exercise the same g.vars ordering production naming relies on.
func cartesianGen(t *testing.T, names ...string) *PathGenerator {
	t.Helper()
	vars := make([]PathVar, 0, len(names))
	for _, name := range names {
		vars = append(vars, PathVar{Name: name, Values: []any{"seed"}})
	}
	return newPathGenerator(vars, nil, 0, 0, false, 0, 0, 0)
}

// candidateSegmentOf returns the last "/"-separated element of name — the candidate segment, which
// is exactly the element `go test -run` compiles as its own regexp.
func candidateSegmentOf(name string) string {
	if idx := strings.LastIndexByte(name, '/'); idx >= 0 {
		return name[idx+1:]
	}
	return name
}

func TestGeneratedCaseNameCarriesSpecIdentityAndOrdinal(t *testing.T) {
	gen := cartesianGen(t, "tier")
	candidate := proposalCandidate{
		Values:        gen.PathValuesWith(map[string]any{"tier": "basic"}),
		AttemptIndex:  1,
		AcceptedIndex: 1,
		Accepted:      true,
	}
	got := generatedCaseName(gen, "Root/Mid/does a thing", candidate)
	want := "Root/Mid/does a thing/case-1-tier=basic"
	if got != want {
		t.Fatalf("generatedCaseName = %q, want %q", got, want)
	}
}

func TestGeneratedCaseNameFallsBackToAttemptIndex(t *testing.T) {
	gen := cartesianGen(t, "tier")
	candidate := proposalCandidate{
		Values:       gen.PathValuesWith(map[string]any{"tier": "basic"}),
		AttemptIndex: 4,
	}
	if got, want := generatedCaseName(gen, "spec", candidate), "spec/case-4-tier=basic"; got != want {
		t.Fatalf("generatedCaseName = %q, want %q", got, want)
	}
}

func TestGeneratedCaseNameWithoutSpecIdentityIsJustTheSegment(t *testing.T) {
	gen := cartesianGen(t, "tier")
	candidate := proposalCandidate{Values: gen.PathValuesWith(map[string]any{"tier": "pro"}), AcceptedIndex: 2}
	if got, want := generatedCaseName(gen, "", candidate), "case-2-tier=pro"; got != want {
		t.Fatalf("generatedCaseName = %q, want %q", got, want)
	}
}

// Two candidates that carry byte-identical values still have to be addressable separately; the
// ordinal is the part of the contract that guarantees it.
func TestGeneratedCaseNameDisambiguatesIdenticalValues(t *testing.T) {
	gen := cartesianGen(t, "tier")
	values := gen.PathValuesWith(map[string]any{"tier": "basic"})
	first := generatedCaseName(gen, "spec", proposalCandidate{Values: values, AcceptedIndex: 1})
	second := generatedCaseName(gen, "spec", proposalCandidate{Values: values, AcceptedIndex: 2})
	if first == second {
		t.Fatalf("identical values produced the same name twice: %q", first)
	}
}

func TestGeneratedCaseNameSeedPerStrategy(t *testing.T) {
	vars := []PathVar{{Name: "value", Values: []any{1}}}
	cases := []struct {
		name     string
		gen      *PathGenerator
		wantSeed bool
	}{
		{"cartesian", newPathGenerator(vars, nil, 0, 7, true, 0, 0, 0), false},
		{"sample", newPathGenerator(vars, nil, 3, 7, true, 0, 0, 0), true},
		{"explore", newPathGenerator(vars, nil, 0, 7, true, 3, 0, 0), true},
		{"exploreCoverage", newPathGenerator(vars, nil, 0, 7, true, 0, 3, 0), true},
		{"exploreSmart", newPathGenerator(vars, nil, 0, 7, true, 0, 0, 3), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidate := proposalCandidate{
				Values:        tc.gen.PathValuesWith(map[string]any{"value": 1}),
				AcceptedIndex: 1,
			}
			got := generatedCaseName(tc.gen, "spec", candidate)
			if has := strings.Contains(got, "-seed7"); has != tc.wantSeed {
				t.Fatalf("name %q carries seed = %v, want %v", got, has, tc.wantSeed)
			}
		})
	}
}

func TestGeneratedCaseNameUsesDefaultSeedWhenUnset(t *testing.T) {
	gen := newPathGenerator([]PathVar{{Name: "value", Values: []any{1}}}, nil, 2, 0, false, 0, 0, 0)
	candidate := proposalCandidate{Values: gen.PathValuesWith(map[string]any{"value": 1}), AcceptedIndex: 1}
	if got := generatedCaseName(gen, "spec", candidate); !strings.Contains(got, "-seed1") {
		t.Fatalf("name %q, want the default sample seed 1", got)
	}
}

func TestGeneratedCaseNameSanitizesUnsafeRunes(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  string
	}{
		{"space", "ba sic", "spec/case-1-tier=ba_sic"},
		{"slash", "a/b", "spec/case-1-tier=a_b"},
		{"brackets", "a[b]c", "spec/case-1-tier=a_b_c"},
		{"star", "a*b", "spec/case-1-tier=a_b"},
		{"pipe", "a|b", "spec/case-1-tier=a_b"},
		{"parens", "a(b)c", "spec/case-1-tier=a_b_c"},
		{"control", "a\tb\nc", "spec/case-1-tier=a_b_c"},
		{"nonASCII", "café", "spec/case-1-tier=caf_"},
		{"collapsedRun", "a ** b", "spec/case-1-tier=a_b"},
		{"allowedPunctuation", "v1.2-beta", "spec/case-1-tier=v1.2-beta"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gen := cartesianGen(t, "tier")
			candidate := proposalCandidate{
				Values:        gen.PathValuesWith(map[string]any{"tier": tc.value}),
				AcceptedIndex: 1,
			}
			if got := generatedCaseName(gen, "spec", candidate); got != tc.want {
				t.Fatalf("generatedCaseName = %q, want %q", got, tc.want)
			}
		})
	}
}

// The candidate segment must be copy-pasteable straight from `go test -v` into `go test -run`, so it
// may not contain any regexp metacharacter other than "." (which still matches itself literally).
func TestGeneratedCaseSegmentIsRegexpSafe(t *testing.T) {
	hostile := []any{
		"a b", "a/b", "[x]", "(x)", "x*", "x+", "x?", "^x", "x$", `a\b`, "x{2}", "a|b",
		"日本語", "\x00\x01", "--", "tab\there", strings.Repeat("Q*", 40),
	}
	gen := cartesianGen(t, "tier")
	for i, value := range hostile {
		candidate := proposalCandidate{
			Values:        gen.PathValuesWith(map[string]any{"tier": value}),
			AcceptedIndex: i + 1,
		}
		name := generatedCaseName(gen, "Root/leaf", candidate)
		segment := candidateSegmentOf(name)
		for _, r := range segment {
			if strings.ContainsRune(`\+*?()|[]{}^$/`, r) {
				t.Fatalf("segment %q (from value %v) contains regexp metacharacter %q", segment, value, r)
			}
		}
		re, err := regexp.Compile("^" + segment + "$")
		if err != nil {
			t.Fatalf("segment %q is not a valid regexp: %v", segment, err)
		}
		if !re.MatchString(segment) {
			t.Fatalf("segment %q does not match itself as a -run pattern", segment)
		}
	}
}

func TestGeneratedCaseNameBoundsLongValues(t *testing.T) {
	gen := cartesianGen(t, "tier")
	long := strings.Repeat("x", 500)
	candidate := proposalCandidate{
		Values:        gen.PathValuesWith(map[string]any{"tier": long}),
		AcceptedIndex: 1,
	}
	name := generatedCaseName(gen, "spec", candidate)
	segment := candidateSegmentOf(name)
	if !strings.Contains(segment, "~") {
		t.Fatalf("truncated segment %q carries no ~hash suffix", segment)
	}
	if got := len([]rune(segment)); got > 128 {
		t.Fatalf("segment length = %d runes, want a bounded name", got)
	}
	if !strings.Contains(segment, strings.Repeat("x", candidateValueRuneCap)) {
		t.Fatalf("segment %q dropped the capped value prefix", segment)
	}
	if strings.Contains(segment, strings.Repeat("x", candidateValueRuneCap+1)) {
		t.Fatalf("segment %q exceeded the per-value cap", segment)
	}

	// Distinct long values must stay visibly distinct even though both are truncated.
	other := proposalCandidate{
		Values:        gen.PathValuesWith(map[string]any{"tier": strings.Repeat("x", 499) + "y"}),
		AcceptedIndex: 1,
	}
	if otherName := generatedCaseName(gen, "spec", other); otherName == name {
		t.Fatalf("two distinct long values produced the same name %q", name)
	}
}

func TestGeneratedCaseNameBoundsTheWholeValuesBlock(t *testing.T) {
	gen := cartesianGen(t, "a", "b", "c", "d", "e", "f", "g", "h")
	values := map[string]any{}
	for _, k := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		values[k] = strings.Repeat(k, 12)
	}
	candidate := proposalCandidate{Values: gen.PathValuesWith(values), AcceptedIndex: 1}
	segment := candidateSegmentOf(generatedCaseName(gen, "spec", candidate))
	if !strings.Contains(segment, "~") {
		t.Fatalf("over-budget segment %q carries no ~hash suffix", segment)
	}
	// "case-1-" + at most the block cap + "~" + 8 hex digits.
	if got, max := len([]rune(segment)), len("case-1-")+candidateValuesRuneCap+9; got > max {
		t.Fatalf("segment length = %d runes, want <= %d (%q)", got, max, segment)
	}
}

func TestGeneratedCaseNameIsDeterministic(t *testing.T) {
	gen := cartesianGen(t, "cfg", "list")
	candidate := proposalCandidate{
		Values: gen.PathValuesWith(map[string]any{
			"cfg":  map[string]int{"zulu": 3, "alpha": 1, "mike": 2},
			"list": []int{3, 1, 2},
		}),
		AcceptedIndex: 1,
	}
	first := generatedCaseName(gen, "spec", candidate)
	for i := 0; i < 32; i++ {
		if got := generatedCaseName(gen, "spec", candidate); got != first {
			t.Fatalf("name is not deterministic: %q then %q", first, got)
		}
	}
	if !strings.Contains(first, "alpha") {
		t.Fatalf("map value was not rendered in the name: %q", first)
	}
}

// TestGeneratedCaseNameMapOrderIsProcessIndependent proves the fix for candidateUnstableKind's map
// walk (PR #107 review): a map holding two different unstable-kind values (a pointer and a channel)
// used to bubble up whichever one Go's randomized MapRange visited first, so the SAME map value could
// render as "ptr" in one run and "chan" in another. That randomization is reseeded per process, so an
// in-process loop cannot exercise it — this spawns real subprocesses, the way
// TestGeneratedCandidateIsSelectableWithRun does, because the property under test is reproducibility
// across process boundaries: a -run pattern captured from one `go test` invocation has to keep
// matching in the next one.
func TestGeneratedCaseNameMapOrderIsProcessIndependent(t *testing.T) {
	if os.Getenv("GO_SPECS_MAP_ORDER_HELPER") == "1" {
		gen := newPathGenerator([]PathVar{{Name: "v", Values: []any{1}}}, nil, 0, 0, false, 0, 0, 0)
		values := map[string]any{"a": &struct{ N int }{N: 1}, "b": make(chan int)}
		candidate := proposalCandidate{
			Values:        gen.PathValuesWith(map[string]any{"v": values}),
			AcceptedIndex: 1,
		}
		fmt.Println(generatedCaseName(gen, "spec", candidate))
		return
	}

	const runs = 5
	names := make([]string, runs)
	for i := 0; i < runs; i++ {
		cmd := exec.Command(os.Args[0], "-test.run=^TestGeneratedCaseNameMapOrderIsProcessIndependent$", "-test.v")
		cmd.Env = append(os.Environ(), "GO_SPECS_MAP_ORDER_HELPER=1")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("helper process %d failed: %v\n%s", i, err, output)
		}
		name, ok := candidateNamePrintedBy(string(output))
		if !ok {
			t.Fatalf("helper process %d printed no candidate name:\n%s", i, output)
		}
		names[i] = name
	}
	for i := 1; i < runs; i++ {
		if names[i] != names[0] {
			t.Fatalf("candidate name is not process-independent: run 0 = %q, run %d = %q", names[0], i, names[i])
		}
	}
	if want := "spec/case-1-v=map"; names[0] != want {
		t.Fatalf("candidate name = %q, want %q (a map must never bubble up an entry's own kind word)", names[0], want)
	}
}

// TestGeneratedCaseNameSliceOverflowNeverFallsBackToAddress proves the fix for candidateUnstableKind's
// slice/array walk (PR #107 review): a pointer sitting past candidateWalkMaxElems was never inspected,
// so the walk reported the value as safe and it fell through to fmt's %v — which embeds the pointer's
// heap address. Two runs of the "same" candidate get different addresses from their own heaps, so the
// name changed between processes even though nothing about the candidate did. Run in real subprocesses
// because that is the only place a leaked address is visible: a single process cannot observe its
// pointers changing.
func TestGeneratedCaseNameSliceOverflowNeverFallsBackToAddress(t *testing.T) {
	if os.Getenv("GO_SPECS_SLICE_OVERFLOW_HELPER") == "1" {
		gen := newPathGenerator([]PathVar{{Name: "v", Values: []any{1}}}, nil, 0, 0, false, 0, 0, 0)
		values := []any{0, 1, 2, 3, 4, 5, 6, 7, &struct{ N int }{N: 1}}
		candidate := proposalCandidate{
			Values:        gen.PathValuesWith(map[string]any{"v": values}),
			AcceptedIndex: 1,
		}
		fmt.Println(generatedCaseName(gen, "spec", candidate))
		return
	}

	const runs = 2
	names := make([]string, runs)
	for i := 0; i < runs; i++ {
		cmd := exec.Command(os.Args[0], "-test.run=^TestGeneratedCaseNameSliceOverflowNeverFallsBackToAddress$", "-test.v")
		cmd.Env = append(os.Environ(), "GO_SPECS_SLICE_OVERFLOW_HELPER=1")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("helper process %d failed: %v\n%s", i, err, output)
		}
		name, ok := candidateNamePrintedBy(string(output))
		if !ok {
			t.Fatalf("helper process %d printed no candidate name:\n%s", i, output)
		}
		if strings.Contains(name, "0x") {
			t.Fatalf("name %q leaked a pointer address for a value past the walk budget", name)
		}
		names[i] = name
	}
	for i := 1; i < runs; i++ {
		if names[i] != names[0] {
			t.Fatalf("candidate name is not process-independent: run 0 = %q, run %d = %q", names[0], i, names[i])
		}
	}
	if want := "spec/case-1-v=slice"; names[0] != want {
		t.Fatalf("candidate name = %q, want %q (an overflowed slice must never fall back to %%v)", names[0], want)
	}
}

// candidateNamePrintedBy finds the line a helper process printed with fmt.Println(generatedCaseName(...))
// among go test's own "=== RUN"/"--- PASS" noise: every candidate name here starts with "spec/", which
// none of testing's own output lines do.
func candidateNamePrintedBy(output string) (string, bool) {
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "spec/") {
			return line, true
		}
	}
	return "", false
}

type stringerValue struct{ secret string }

func (stringerValue) String() string { return "REDACTED" }

type errorValue struct{}

func (errorValue) Error() string { return "boom" }

func TestGeneratedCaseNameRendersUnstableKindsAsKindWords(t *testing.T) {
	ptr := &struct{ N int }{N: 1}
	ch := make(chan int)
	cases := []struct {
		name  string
		value any
		want  string
	}{
		{"pointer", ptr, "ptr"},
		{"func", func() {}, "func"},
		{"chan", ch, "chan"},
		{"uintptr", uintptr(0xdeadbeef), "uintptr"},
		{"unsafePointer", unsafe.Pointer(ptr), "unsafeptr"},
		{"pointerInsideSlice", []any{1, ptr}, "ptr"},
		// A map never bubbles up an entry's own kind word (see candidateUnstableKind): its
		// marker is always "map", so it cannot depend on which entry MapRange visits first.
		{"pointerInsideMap", map[string]any{"k": ptr}, "map"},
		{"pointerInsideStruct", struct{ P *int }{P: new(int)}, "ptr"},
		{"nil", nil, "nil"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gen := cartesianGen(t, "v")
			candidate := proposalCandidate{
				Values:        gen.PathValuesWith(map[string]any{"v": tc.value}),
				AcceptedIndex: 1,
			}
			got := generatedCaseName(gen, "spec", candidate)
			if want := "spec/case-1-v=" + tc.want; got != want {
				t.Fatalf("generatedCaseName = %q, want %q", got, want)
			}
			if strings.Contains(got, "0x") {
				t.Fatalf("name %q leaked a pointer address", got)
			}
		})
	}
}

// A value type redacts itself by implementing fmt.Stringer; that hook wins even for a pointer
// receiver, because the type — not the framework — owns how it appears in a test name.
func TestGeneratedCaseNameHonorsStringerAndError(t *testing.T) {
	gen := cartesianGen(t, "creds", "err")
	candidate := proposalCandidate{
		Values: gen.PathValuesWith(map[string]any{
			"creds": &stringerValue{secret: "hunter2"},
			"err":   errors.New("bad input"),
		}),
		AcceptedIndex: 1,
	}
	got := generatedCaseName(gen, "spec", candidate)
	if strings.Contains(got, "hunter2") {
		t.Fatalf("name %q leaked the redacted value", got)
	}
	if !strings.Contains(got, "REDACTED") {
		t.Fatalf("name %q did not use the fmt.Stringer rendering", got)
	}
	if !strings.Contains(got, "bad_input") {
		t.Fatalf("name %q did not use the error rendering", got)
	}
	if _, isError := any(errorValue{}).(error); !isError {
		t.Fatal("errorValue must implement error")
	}
}

func TestGeneratedCaseNameOmitsValuesBlockWhenEmpty(t *testing.T) {
	gen := cartesianGen(t, "tier")
	candidate := proposalCandidate{AcceptedIndex: 3}
	if got, want := generatedCaseName(gen, "spec", candidate), "spec/case-3"; got != want {
		t.Fatalf("generatedCaseName = %q, want %q", got, want)
	}
	if got, want := generatedCaseName(nil, "spec", candidate), "spec/case-3"; got != want {
		t.Fatalf("generatedCaseName with nil generator = %q, want %q", got, want)
	}
}

func TestGeneratedCaseReportNameKeepsFrameworkFormatting(t *testing.T) {
	gen := cartesianGen(t, "tier")
	candidate := proposalCandidate{
		Values:        gen.PathValuesWith(map[string]any{"tier": "ba sic"}),
		AcceptedIndex: 2,
	}
	got := generatedCaseReportName(gen, "includes tier", candidate)
	want := "includes tier [tier=ba sic] #2"
	if got != want {
		t.Fatalf("generatedCaseReportName = %q, want %q", got, want)
	}
	if got := generatedCaseReportName(nil, "includes tier", candidate); got != "includes tier #2" {
		t.Fatalf("nil generator report name = %q", got)
	}
}

// BenchmarkGeneratedCaseName measures naming in isolation, because the end-to-end path benchmarks
// in benchmarks/ run against a *testing.B, where backendNamesSubtests is false and no name is built
// at all. This is the real per-candidate cost paid under `go test`, and the allocation count is the
// number that matters: it must stay at one (the name string itself).
func BenchmarkGeneratedCaseName(b *testing.B) {
	gen := newPathGenerator([]PathVar{
		{Name: "tier", Values: []any{"basic"}},
		{Name: "price", Values: []any{10}},
		{Name: "vip", Values: []any{true}},
	}, nil, 0, 0, false, 0, 0, 0)
	candidate := proposalCandidate{
		Values:        gen.PathValuesWith(map[string]any{"tier": "basic", "price": 10, "vip": true}),
		AcceptedIndex: 7,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sinkCandidateName = generatedCaseName(gen, "Root/Mid/leaf", candidate)
	}
}

// sinkCandidateName keeps the benchmarked name from being optimized away.
var sinkCandidateName string

func TestSpecIdentityNamePrefersFullName(t *testing.T) {
	plan := &ExecutionPlan{Names: []string{"leaf"}, FullNames: []string{"Root/Mid/leaf"}}
	if got, want := specIdentityName(plan, 0), "Root/Mid/leaf"; got != want {
		t.Fatalf("specIdentityName = %q, want %q", got, want)
	}
	bare := &ExecutionPlan{Names: []string{"leaf"}, FullNames: []string{""}}
	if got, want := specIdentityName(bare, 0), "leaf"; got != want {
		t.Fatalf("specIdentityName fallback = %q, want %q", got, want)
	}
	empty := &ExecutionPlan{}
	if got := specIdentityName(empty, 0); got != "" {
		t.Fatalf("specIdentityName on a bare plan = %q, want \"\"", got)
	}
}
