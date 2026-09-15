package specs

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

const (
	// candidateValueRuneCap bounds one rendered path value inside a subtest name. A generated
	// candidate may carry an arbitrarily large value (a 4KB payload string, a big struct); a test
	// name that serialized it verbatim would be unreadable in -v output and useless as a -run
	// pattern, so each value contributes at most this many runes.
	candidateValueRuneCap = 16

	// candidateValuesRuneCap bounds the whole "k=v,k=v" block. The per-value cap alone does not
	// bound the name, because a spec may declare many variables; this is the hard ceiling that
	// keeps the segment scannable no matter how wide the path is.
	candidateValuesRuneCap = 64

	// candidateHashRuneLen is "~" plus the 8 hex digits of the truncation hash (see
	// generatedCaseName). Used only to pre-size the name builder.
	candidateHashRuneLen = 9

	// candidateWalkMaxDepth and candidateWalkMaxElems bound the reflective search for
	// unstable-formatting values (see candidateUnstableKind). Naming runs once per executed
	// candidate, so the walk must be cheap and must terminate on recursive or huge structures;
	// these limits trade an exhaustive search for a predictable constant cost. A pointer buried
	// deeper than this renders through fmt, which is the pre-existing behavior.
	candidateWalkMaxDepth = 4
	candidateWalkMaxElems = 8

	// candidateFNVOffset/candidateFNVPrime are the 32-bit FNV-1a parameters. 32 bits is exactly the
	// 8 hex digits the truncation suffix carries, and hashing inline (rather than via hash/fnv)
	// keeps naming allocation-free apart from the name itself.
	candidateFNVOffset uint32 = 2166136261
	candidateFNVPrime  uint32 = 16777619
)

// generatedCaseName maps one executed generated candidate to the name its Go subtest runs under.
// It is the single definition of generated-candidate identity; runIsolatedCase never invents one.
//
// Shape:
//
//	<spec identity>/case-<n>[-seed<s>][-<k=v,k=v...>]
//
// The contract this name guarantees:
//
//  1. Every executed candidate is identified by its owning spec plus a 1-based executed ordinal
//     (candidate.AcceptedIndex, falling back to AttemptIndex). Before this, every candidate ran as
//     the literal subtest "generated" and Go disambiguated them as "generated#01" — a name that
//     identified neither the spec, the values, the ordinal, nor the seed.
//  2. Cartesian and Sample candidates carry enough stable information to reproduce the case: the
//     values themselves, plus the seed for Sample, whose stream depends on it. CartesianMode omits
//     the seed because its stream is fully determined by the declared variables, so printing one
//     would suggest a knob that does not exist.
//  3. Adaptive strategies (ExplorationGuided: Explore, ExploreCoverage, ExploreSmart) carry the seed
//     and the ordinal, but selecting a single one of their candidates with `go test -run` is NOT
//     supported: testing skips the bodies of the subtests that do not match, while the explorer
//     derives each later candidate from the feedback of the earlier ones it no longer gets. The
//     safety property is that the values are part of the name, so a candidate that diverges under
//     the filtered run simply does not match the pattern and does not run. A -run pattern can
//     therefore under-select, but it can never silently execute a semantically different candidate
//     under the name you asked for.
//  4. Candidates with byte-identical values cannot collide: the ordinal alone is unique across a
//     spec's executed candidates, which is also why truncating the values block (below) can never
//     create an ambiguous name.
//  5. Awkward values have defined, safe behavior — see candidateRuneAllowed (sanitization),
//     the two rune caps (bounding), and candidateUnstableKind (values whose %v would embed a
//     pointer address). A value type that implements fmt.Stringer controls its own rendering; that
//     is the documented redaction hook, because path values appear verbatim in test names.
//  6. `go test -v`, test2json and the reporter identify the same logical candidate by the same
//     ordinal. They are deliberately not the same string: the reporter keeps the framework's own
//     unsanitized values (see generatedCaseReportName), the subtest name keeps the -run-safe one.
//
// specIdentity is passed through unsanitized on purpose. It is the framework's own declared spec
// name, and testing applies its own rewrite (spaces to "_") to it exactly as it already does for
// the sequential path; only the candidate segment this function appends is sanitized, because only
// that segment is generated from user data.
//
// Truncation: when any value hits candidateValueRuneCap or the block hits candidateValuesRuneCap,
// the segment ends with "~" plus 8 hex digits of an FNV-1a hash over the full, untruncated
// encoding. Uniqueness is already guaranteed by the ordinal; the hash is there so a human reading
// -v output can still tell two long, common-prefixed candidates apart.
func generatedCaseName(gen *PathGenerator, specIdentity string, candidate proposalCandidate) string {
	var b strings.Builder
	// One Grow for the whole name: this runs once per executed candidate, on the hot path.
	b.Grow(len(specIdentity) + len("/case-") + 20 + len("-seed") + 20 + candidateValuesRuneCap + candidateHashRuneLen)
	if specIdentity != "" {
		b.WriteString(specIdentity)
		b.WriteByte('/')
	}
	b.WriteString("case-")
	b.WriteString(strconv.Itoa(candidateOrdinal(candidate)))
	// Only the seeded strategies get "-seed": for CartesianMode the seed is not an input to the
	// candidate stream at all, so including it would imply a reproducibility knob that isn't one.
	if gen != nil && (gen.mode == SamplingMode || gen.mode == ExplorationGuided) {
		b.WriteString("-seed")
		b.WriteString(strconv.FormatInt(gen.explorationSeed, 10))
	}
	appendCandidateValues(&b, gen, candidate.Values)
	return b.String()
}

// generatedCaseReportName is generatedCaseName's reporter-side counterpart: it keeps the
// framework's own "base [k=v k=v]" rendering (PathGenerator.FormatName, a public formatting
// contract this must not change) and appends the same 1-based ordinal the subtest name uses, so a
// reader can line a report entry up with a `go test -v` subtest. It is deliberately NOT t.Name():
// reported identity must never inherit testing's space-to-underscore rewrite or its "#01"
// duplicate suffixes, exactly as documented for the sequential path in runSpecRecovered.
func generatedCaseReportName(gen *PathGenerator, base string, candidate proposalCandidate) string {
	name := gen.FormatName(base, candidate.Values)
	var b strings.Builder
	b.Grow(len(name) + 4)
	b.WriteString(name)
	b.WriteString(" #")
	b.WriteString(strconv.Itoa(candidateOrdinal(candidate)))
	return b.String()
}

// candidateOrdinal returns the 1-based executed ordinal. Execute is only ever called for accepted
// candidates, so AcceptedIndex is always set in production; the AttemptIndex fallback keeps the
// name meaningful for a hand-built candidate (tests) rather than silently emitting "case-0".
func candidateOrdinal(candidate proposalCandidate) int {
	if candidate.AcceptedIndex > 0 {
		return candidate.AcceptedIndex
	}
	return candidate.AttemptIndex
}

// specIdentityName returns the breadcrumb that identifies spec i inside its plan: the slash-joined
// full name when the plan carries one, otherwise the leaf name specEventName reports. Generated
// candidates use the full breadcrumb rather than the leaf because all of a plan's candidates share
// one parent *testing.T, so the leaf alone would not tell two same-named specs apart in -v output.
func specIdentityName(plan *ExecutionPlan, i int) string {
	if plan != nil && i >= 0 && i < len(plan.FullNames) && plan.FullNames[i] != "" {
		return plan.FullNames[i]
	}
	return specEventName(plan, i)
}

// appendCandidateValues writes "-k=v,k=v..." for the candidate's present values in declaration
// order, sanitized and bounded, followed by the truncation hash when anything was cut. It writes
// nothing at all when the candidate carries no values, so a valueless candidate's name stops at its
// ordinal instead of trailing a bare separator.
//
// The FNV-1a hash is folded in over the FULL untruncated encoding while the bounded one is being
// written, so a candidate whose values overflow the budget still hashes over everything that made
// it distinct — including the values that never reached the builder at all — without materializing
// a second string.
func appendCandidateValues(b *strings.Builder, gen *PathGenerator, values PathValues) {
	if gen == nil || values.len() == 0 {
		return
	}
	w := candidateWriter{b: b, remaining: candidateValuesRuneCap, hash: candidateFNVOffset}
	first := true
	for _, v := range gen.vars {
		val, ok := values.lookup(v.Name)
		if !ok {
			continue
		}
		if first {
			// Written lazily: only a candidate that actually has a value gets the separator.
			b.WriteByte('-')
			first = false
		} else {
			w.write(",")
		}
		w.write(v.Name)
		w.write("=")
		w.writeValue(renderCandidateValue(val))
	}
	if first {
		return
	}
	if !w.truncated {
		return
	}
	b.WriteByte('~')
	// %08x, hand-rolled to avoid dragging fmt into the common path's allocation profile.
	const hexDigits = "0123456789abcdef"
	for shift := 28; shift >= 0; shift -= 4 {
		b.WriteByte(hexDigits[(w.hash>>uint(shift))&0xf])
	}
}

// candidateWriter emits the sanitized, rune-bounded values block while folding every byte of the
// unbounded encoding into an FNV-1a hash. Keeping both in one pass is what lets the name stay
// bounded without building the full string first.
type candidateWriter struct {
	b *strings.Builder
	// remaining is the output rune budget left for the whole values block.
	remaining int
	// lastReplaced collapses a run of disallowed runes into a single "_", so "a ** b" reads
	// "a_b" instead of "a____b".
	lastReplaced bool
	truncated    bool
	hash         uint32
}

// write hashes s in full and appends its sanitized form while the block budget lasts.
func (w *candidateWriter) write(s string) {
	w.hashString(s)
	w.appendSanitized(s)
}

// writeValue is write with the per-value rune cap applied. The cap is on runes, not bytes, so a
// multi-byte value is never cut mid-rune.
func (w *candidateWriter) writeValue(s string) {
	w.hashString(s)
	capped := s
	runes := 0
	for i := range s {
		runes++
		if runes > candidateValueRuneCap {
			// i is the byte offset of the first rune past the cap, so s[:i] is exactly
			// candidateValueRuneCap runes and never a partial one.
			capped = s[:i]
			w.truncated = true
			break
		}
	}
	w.appendSanitized(capped)
}

func (w *candidateWriter) hashString(s string) {
	h := w.hash
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= candidateFNVPrime
	}
	w.hash = h
}

// appendSanitized walks s byte by byte rather than rune by rune. That is not a shortcut that
// mangles UTF-8: every allowed rune is ASCII, so every byte of a multi-byte rune is >= 0x80 and
// therefore disallowed, and the run-collapsing below turns the whole sequence — or a whole run of
// such runes — into the single "_" a rune-wise walk would produce. Skipping UTF-8 decoding keeps
// naming cheap on a per-candidate hot path.
func (w *candidateWriter) appendSanitized(s string) {
	for i := 0; i < len(s); i++ {
		if w.remaining <= 0 {
			w.truncated = true
			return
		}
		if c := s[i]; candidateRuneAllowed(rune(c)) {
			w.b.WriteByte(c)
			w.lastReplaced = false
			w.remaining--
			continue
		}
		if w.lastReplaced {
			continue
		}
		w.b.WriteByte('_')
		w.lastReplaced = true
		w.remaining--
	}
}

// candidateRuneAllowed reports whether r may appear verbatim in a candidate segment.
//
// `go test -run` compiles each "/"-separated element of the pattern as its own regexp, so a name
// copied out of `go test -v` is only usable as a -run pattern if it contains no regexp
// metacharacter. Everything else — spaces (which testing would rewrite anyway), "/" (which would
// fake a subtest level), control characters and all non-ASCII — is replaced by "_".
//
// "." is the one permitted rune that is also a metacharacter. It is allowed because version-like
// and float-like values ("v1.2", "3.5") are common and stay far more readable with it; as a pattern
// it still matches itself literally, so a "." can only ever over-match (also matching some other
// rune in that position), never under-match the candidate it came from.
func candidateRuneAllowed(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z':
		return true
	case r >= 'A' && r <= 'Z':
		return true
	case r >= '0' && r <= '9':
		return true
	}
	switch r {
	case '_', '-', '.', '=', ',':
		return true
	}
	return false
}

// renderCandidateValue turns one path value into the text the name carries.
//
// The primitive cases come first and answer without reflection, because naming runs once per
// executed candidate and the overwhelming majority of path values are strings, ints and bools.
//
// fmt.Stringer and error win over everything below: a type that defines its own rendering owns it,
// and that is the documented way to keep a sensitive value out of test names — implement String()
// and return a redacted form.
func renderCandidateValue(v any) string {
	if v == nil {
		return "nil"
	}
	switch value := v.(type) {
	case string:
		return value
	case bool:
		return strconv.FormatBool(value)
	case int:
		return strconv.Itoa(value)
	case int8:
		return strconv.FormatInt(int64(value), 10)
	case int16:
		return strconv.FormatInt(int64(value), 10)
	case int32:
		return strconv.FormatInt(int64(value), 10)
	case int64:
		return strconv.FormatInt(value, 10)
	case uint:
		return strconv.FormatUint(uint64(value), 10)
	case uint8:
		return strconv.FormatUint(uint64(value), 10)
	case uint16:
		return strconv.FormatUint(uint64(value), 10)
	case uint32:
		return strconv.FormatUint(uint64(value), 10)
	case uint64:
		return strconv.FormatUint(value, 10)
	case float32:
		return strconv.FormatFloat(float64(value), 'g', -1, 32)
	case float64:
		return strconv.FormatFloat(value, 'g', -1, 64)
	case fmt.Stringer:
		return toString(value)
	case error:
		return toString(value)
	}
	if kind, unstable := candidateUnstableKind(reflect.ValueOf(v), 0); unstable {
		return kind
	}
	// Maps and slices of ordinary values are safe here: fmt prints map keys in sorted order
	// (Go 1.12+), so their %v rendering is stable across runs of the same values.
	return toString(v)
}

// candidateUnstableKind reports the kind word to use instead of fmt's rendering when rv is, or
// transitively contains, a value whose %v embeds a pointer address.
//
// A test name must be the same string on every run of the same candidate — otherwise -run patterns
// rot between runs and diffs of -v output are pure noise. Addresses are not; so a pointer, unsafe
// pointer, func, chan or uintptr collapses to a fixed word ("ptr", "func", ...) that says what the
// value was without pretending to identify it. The walk is bounded (see candidateWalkMaxDepth /
// candidateWalkMaxElems) because it runs per executed candidate and must terminate on cyclic or
// very wide data.
func candidateUnstableKind(rv reflect.Value, depth int) (string, bool) {
	if depth > candidateWalkMaxDepth || !rv.IsValid() {
		return "", false
	}
	switch rv.Kind() {
	case reflect.Pointer:
		return "ptr", true
	case reflect.UnsafePointer:
		return "unsafeptr", true
	case reflect.Func:
		return "func", true
	case reflect.Chan:
		return "chan", true
	case reflect.Uintptr:
		return "uintptr", true
	case reflect.Interface:
		if rv.IsNil() {
			return "", false
		}
		return candidateUnstableKind(rv.Elem(), depth+1)
	case reflect.Slice, reflect.Array:
		for i := 0; i < rv.Len() && i < candidateWalkMaxElems; i++ {
			if kind, unstable := candidateUnstableKind(rv.Index(i), depth+1); unstable {
				return kind, true
			}
		}
	case reflect.Map:
		seen := 0
		for iter := rv.MapRange(); iter.Next() && seen < candidateWalkMaxElems; seen++ {
			if kind, unstable := candidateUnstableKind(iter.Key(), depth+1); unstable {
				return kind, true
			}
			if kind, unstable := candidateUnstableKind(iter.Value(), depth+1); unstable {
				return kind, true
			}
		}
	case reflect.Struct:
		for i := 0; i < rv.NumField() && i < candidateWalkMaxElems; i++ {
			if kind, unstable := candidateUnstableKind(rv.Field(i), depth+1); unstable {
				return kind, true
			}
		}
	}
	return "", false
}
