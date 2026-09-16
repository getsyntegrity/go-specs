package specs

// PathBuilder collects named variables for path generation.
type PathBuilder struct {
	vars    []PathVar
	filters []PathFilter
}

// PathVar represents a named variable and its candidate values.
type PathVar struct {
	Name      string
	Values    []any
	rangeSpec *intRange
	shrinker  ValueShrinker
}

// ValueShrinker defines how to reduce a failing value to simpler forms.
type ValueShrinker interface {
	Shrink(value any) []any
}

type intRange struct {
	min int
	max int
}

// PathSpec binds path variables to a spec for runtime generation.
type PathSpec struct {
	spec                      *Spec
	vars                      []PathVar
	filters                   []PathFilter
	samples                   int
	seed                      int64
	hasSeed                   bool
	exploreIterations         int
	exploreCoverageIterations int
	exploreSmartIterations    int
}

// PathFilter filters combinations during generation.
type PathFilter func(PathValues) bool

// Paths defines path variables for subsequent It calls.
func (s *Spec) Paths(build func(*PathBuilder)) *PathSpec {
	if s == nil {
		return nil
	}
	pb := &PathBuilder{}
	if build != nil {
		build(pb)
	}
	return &PathSpec{spec: s, vars: pb.vars, filters: pb.filters}
}

// Bool registers a boolean variable.
func (p *PathBuilder) Bool(name string) {
	p.Values(name, []any{true, false})
}

// Int registers an integer variable with explicit values.
func (p *PathBuilder) Int(name string, values []int) {
	anyVals := make([]any, 0, len(values))
	for _, v := range values {
		anyVals = append(anyVals, v)
	}
	p.Values(name, anyVals)
}

// Values registers arbitrary values for a variable.
func (p *PathBuilder) Values(name string, values []any) {
	if name == "" {
		return
	}
	p.vars = append(p.vars, PathVar{Name: name, Values: append([]any(nil), values...)})
}

// IntRange registers an integer variable that can take values between min and max (inclusive).
func (p *PathBuilder) IntRange(name string, min, max int) {
	if name == "" || max < min {
		return
	}
	rs := intRange{min: min, max: max}
	p.vars = append(p.vars, PathVar{Name: name, rangeSpec: &rs})
}

// WithShrinker attaches a custom shrinker to the most recently added variable.
func (p *PathBuilder) WithShrinker(s ValueShrinker) {
	if len(p.vars) == 0 || s == nil {
		return
	}
	p.vars[len(p.vars)-1].shrinker = s
}

// Filter registers a combination filter.
func (p *PathBuilder) Filter(fn PathFilter) {
	if fn == nil {
		return
	}
	p.filters = append(p.filters, fn)
}

// It registers a path-aware spec that generates combinations at runtime.
func (ps *PathSpec) It(name string, args ...any) {
	if ps == nil || ps.spec == nil {
		return
	}
	ops, fn := parseItArgs(args)
	if fn == nil {
		return
	}
	gen := newPathGenerator(ps.vars, ps.filters, ps.samples, ps.seed, ps.hasSeed, ps.exploreIterations, ps.exploreCoverageIterations, ps.exploreSmartIterations)
	ps.spec.runPathWithContext(name, gen, ops, fn)
}

// Sample enables random sampling rather than full cartesian enumeration.
func (ps *PathSpec) Sample(n int) *PathSpec {
	if ps == nil {
		return ps
	}
	if n < 0 {
		n = 0
	}
	ps.samples = n
	return ps
}

// Seed overrides the sampling seed for deterministic runs.
func (ps *PathSpec) Seed(seed int64) *PathSpec {
	if ps == nil {
		return ps
	}
	ps.seed = seed
	ps.hasSeed = true
	return ps
}

// Explore enables coverage-guided exploration mode.
func (ps *PathSpec) Explore(iterations int) *PathSpec {
	if ps == nil {
		return ps
	}
	if iterations < 0 {
		iterations = 0
	}
	ps.exploreIterations = iterations
	return ps
}

// ExploreCoverage enables coverage-guided exploration mode via CoverageExplorer: NextInput mutates
// from a corpus (via Mutator.Mutate — see its doc comment for exactly which mutations apply) once
// the corpus is non-empty, falling back to fully random input until then.
//
// Corpus growth doesn't use real assertion-level branch coverage yet — that needs ctx.coverage
// wired to the runner per iteration, which isn't done (see PathGenerator.runGuidedExploration's doc
// comment). It uses a call-site-signature novelty heuristic as a proxy instead, so this is closer
// to "mutation-based exploration with a corpus" than genuine coverage-guided fuzzing today.
func (ps *PathSpec) ExploreCoverage(iterations int) *PathSpec {
	if ps == nil {
		return ps
	}
	if iterations < 0 {
		iterations = 0
	}
	ps.exploreCoverageIterations = iterations
	return ps
}

// ExploreSmart enables smart exploration mode via SmartExplorer: a weighted mix of corpus mutation,
// random input, boundary values, and corpus replay (see SmartExplorer.NextInput's doc comment for
// the exact weights). Corpus growth uses the same signature-based novelty proxy as ExploreCoverage
// — see its doc comment for why that isn't genuine coverage-guided selection yet.
func (ps *PathSpec) ExploreSmart(iterations int) *PathSpec {
	if ps == nil {
		return ps
	}
	if iterations < 0 {
		iterations = 0
	}
	ps.exploreSmartIterations = iterations
	return ps
}
