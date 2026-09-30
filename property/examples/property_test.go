// property_test.go shows the property package: check an invariant against many generated inputs,
// and when it is false get a small counterexample and an exact way to replay it.
//
// Use a property when the rule you care about is "for every input of this kind" (reversing a list
// twice returns the list, encoding then decoding returns the value) and a handful of hand-picked
// It cases would not convince you.
//
// How it fits together:
//   - Generators are pgregory.net/rapid's own (rapid.Int, rapid.SliceOf, rapid.Custom, ...).
//     property.Draw takes a value from one and records it under a label for the counterexample.
//   - Assertions inside the property use the go-specs matchers: p.Expect(x).To(matcher).
//   - property.Check runs the property inside a test and fails the test with a report.
//     property.Run returns a Result instead, which is how these examples show a failure without
//     failing.
//   - A run has three failure kinds kept apart: Failed (an assertion), Panicked, and Exhausted
//     (too many rejected inputs). Rejecting an input with Assume or Reject is never a failure.
//   - A failing input is shrunk to a small one. WithSeed replays a run exactly; a fail file under
//     testdata/rapid replays a persisted failure before anything else is generated.
//
// Under plain `go test` a property runs 100 valid inputs (20 under -short). Coverage-guided search
// is `go test -fuzz` on a FuzzXxx target, and a longer random search is -rapid.checks=N.
package examples_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/getsyntegrity/go-specs/assert"
	"github.com/getsyntegrity/go-specs/property"
	"github.com/getsyntegrity/go-specs/specs"
	"pgregory.net/rapid"
)

// The smallest useful property: draw an input, assert an invariant. Check fails the test, with the
// counterexample and how to replay it, if the invariant is ever false.
func TestProperty_minimalCheck(t *testing.T) {
	property.Check(t, func(p *property.T) {
		xs := property.Draw(p, rapid.SliceOf(rapid.Int()), "xs")
		sorted := slices.Clone(xs)
		slices.Sort(sorted)
		p.Expect(slices.IsSorted(sorted)).To(assert.BeTrue())
		p.Expect(len(sorted)).ToEqual(len(xs))
	}, property.WithoutFailFile())
}

// A property inside a spec: ctx.Testing() is the *testing.T of the enclosing It, so Check reports
// on that subtest. Use this to keep property checks next to your example-based specs.
func TestProperty_insideASpec(t *testing.T) {
	specs.Describe(t, "reversing", func(s *specs.Spec) {
		s.It("twice returns the original slice", func(ctx *specs.Context) {
			property.Check(ctx.Testing(), func(p *property.T) {
				xs := property.Draw(p, rapid.SliceOf(rapid.Int()), "xs")
				rev := slices.Clone(xs)
				slices.Reverse(rev)
				slices.Reverse(rev)
				p.Expect(rev).To(assert.Equal(xs))
			}, property.WithoutFailFile())
		})
	})
}

// Generators come from rapid and compose. Draw several values, label each, and constrain them with
// generator arguments (IntRange, SliceOfN) rather than by rejecting inputs afterwards.
func TestProperty_generators(t *testing.T) {
	type point struct{ X, Y int }
	pointGen := rapid.Custom(func(t *rapid.T) point {
		return point{X: rapid.IntRange(-100, 100).Draw(t, "x"), Y: rapid.IntRange(-100, 100).Draw(t, "y")}
	})

	property.Check(t, func(p *property.T) {
		name := property.Draw(p, rapid.StringMatching(`[a-z]{1,8}`), "name")
		age := property.Draw(p, rapid.IntRange(0, 120), "age")
		pt := property.Draw(p, pointGen, "point")
		tags := property.Draw(p, rapid.SliceOfN(rapid.SampledFrom([]string{"a", "b", "c"}), 1, 3), "tags")

		p.Expect(name).To(assert.MatchRegex(`^[a-z]+$`))
		p.Expect(age).To(assert.BeBetween(0, 120))
		p.Expect(pt.X).To(assert.BeBetween(-100, 100))
		p.Expect(tags).To(assert.HaveLen(len(tags)))
		p.Expect(len(tags) >= 1 && len(tags) <= 3).To(assert.BeTrue())
	}, property.WithChecks(50), property.WithoutFailFile())
}

// Assume rejects an input that the property does not apply to. A rejected input is counted, is not
// a failure, and does not count toward WithChecks. Prefer a generator that only makes valid inputs;
// use Assume for a condition that is hard to express in the generator.
func Example_propertyAssume() {
	res := property.Run(func(p *property.T) {
		n := property.Draw(p, rapid.IntRange(1, 100), "n")
		p.Assume(n%2 == 0) // the invariant is only about even numbers
		p.Expect(n % 2).ToEqual(0)
	}, property.WithChecks(20), property.WithSeed(7), property.WithoutFailFile())

	fmt.Println(res.Outcome)
	fmt.Println("valid checks:", res.Checks)
	fmt.Println("some rejected:", res.Rejected > 0)
	// Output:
	// passed
	// valid checks: 20
	// some rejected: true
}

// If almost every input is rejected the invariant was never really exercised. That is neither a
// pass nor a failure: the outcome is Exhausted, and the report says to generate only valid inputs.
func Example_propertyExhausted() {
	res := property.Run(func(p *property.T) {
		n := property.Draw(p, rapid.Int(), "n")
		p.Reject("input %d is never applicable", n)
	}, property.WithChecks(10), property.WithSeed(1), property.WithoutFailFile())

	fmt.Println(res.Outcome)
	fmt.Println("valid checks:", res.Checks)
	// Output:
	// exhausted
	// valid checks: 0
}

func sum(xs []int) int {
	total := 0
	for _, x := range xs {
		total += x
	}
	return total
}

// sumBelow100 is deliberately false: a sum of non-negative numbers is not always below 100.
func sumBelow100(p *property.T) {
	xs := property.Draw(p, rapid.SliceOf(rapid.IntRange(0, 1000)), "xs")
	p.Expect(sum(xs)).To(assert.BeLessThan(100))
}

// Run returns the failure instead of failing the test. The engine shrinks the first failing input to
// the smallest one it can find: here a single element that reaches the threshold exactly. The seed
// makes the whole run, and so the counterexample, deterministic.
func Example_propertyShrinking() {
	res := property.Run(sumBelow100, property.WithSeed(42), property.WithoutFailFile())

	fmt.Println(res.Outcome)
	for _, d := range res.Counterexample {
		fmt.Printf("%s = %s\n", d.Label, d.Value)
	}
	fmt.Println("original is larger:", len(res.Original[0].Value) > len(res.Counterexample[0].Value))
	fmt.Println("failures:", len(res.Failures))
	// Output:
	// failed
	// xs = []int{100}
	// original is larger: true
	// failures: 1
}

// A falsified property carries a replay recipe. Result.Seed reproduces the failure with WithSeed
// (or -rapid.seed=N on the command line), and the same seed gives the same counterexample every time.
func Example_propertyReplayBySeed() {
	first := property.Run(sumBelow100, property.WithSeed(42), property.WithoutFailFile())
	again := property.Run(sumBelow100, property.WithSeed(first.Seed), property.WithoutFailFile())

	fmt.Println("same counterexample:", again.Counterexample[0].Value == first.Counterexample[0].Value)
	// Output:
	// same counterexample: true
}

// Without WithoutFailFile a failure is written to testdata/rapid/<name>/. Commit that file and every
// later `go test` replays it first, before generating anything: the counterexample becomes a
// regression test until you delete the file after fixing the bug. WithFailFile replays one file by
// path. This test works in a temporary directory so it leaves nothing behind.
func TestProperty_replayByFailFile(t *testing.T) {
	t.Chdir(t.TempDir())

	first := property.Run(sumBelow100, property.WithName("sum-below-100"), property.WithSeed(42))
	if first.Outcome != property.Failed || first.FailFile == "" {
		t.Fatalf("want a failure with a fail file, got %v %q", first.Outcome, first.FailFile)
	}

	// The next run finds the corpus file by the property name and replays it without any seed.
	replayed := property.Run(sumBelow100, property.WithName("sum-below-100"))
	// WithFailFile names the file explicitly, whatever the property is called.
	explicit := property.Run(sumBelow100, property.WithName("elsewhere"), property.WithFailFile(first.FailFile))

	specs.Describe(t, "fail file replay", func(s *specs.Spec) {
		s.It("fails again from the persisted file", func(ctx *specs.Context) {
			ctx.Expect(replayed.Outcome).ToEqual(property.Failed)
			ctx.Expect(replayed.FailFile).ToEqual(first.FailFile)
			ctx.Expect(replayed.Counterexample).ToEqual(first.Counterexample)
		})
		s.It("replays an explicitly named file", func(ctx *specs.Context) {
			ctx.Expect(explicit.Outcome).ToEqual(property.Failed)
			ctx.Expect(explicit.Counterexample).ToEqual(first.Counterexample)
		})
	})
}

// WithoutFailFile keeps a failing run from writing testdata/rapid. The other examples use it so that
// they never write into the repository.
func TestProperty_withoutFailFileLeavesNothingBehind(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	res := property.Run(sumBelow100, property.WithSeed(42), property.WithoutFailFile())
	_, err := os.Stat(filepath.Join(dir, "testdata"))

	specs.Describe(t, "WithoutFailFile", func(s *specs.Spec) {
		s.It("fails without persisting a corpus file", func(ctx *specs.Context) {
			ctx.Expect(res.Outcome).ToEqual(property.Failed)
			ctx.Expect(res.FailFile).ToEqual("")
			ctx.Expect(os.IsNotExist(err)).ToEqual(true)
		})
	})
}

// The report is what Check prints on a failure: the outcome, the shrunk counterexample and how to
// replay it. Result.Report holds the same text.
func TestProperty_reportShowsCounterexampleAndReplay(t *testing.T) {
	res := property.Run(sumBelow100, property.WithName("sum-below-100"), property.WithSeed(42), property.WithoutFailFile())

	specs.Describe(t, "report", func(s *specs.Spec) {
		s.It("names the property, the counterexample and the seed", func(ctx *specs.Context) {
			ctx.Expect(res.Report).To(specs.Contain("sum-below-100"))
			ctx.Expect(res.Report).To(specs.Contain("xs = []int{100}"))
			ctx.Expect(res.Report).To(specs.Contain("property.WithSeed("))
		})
	})
}

// A panic in the property is reported as Panicked, not as an assertion failure. Result.Panic holds
// the value and the stack, and the input is shrunk like any other failure.
func Example_propertyPanic() {
	res := property.Run(func(p *property.T) {
		n := property.Draw(p, rapid.IntRange(0, 1000), "n")
		if n > 10 {
			panic(fmt.Errorf("n too large: %d", n))
		}
	}, property.WithSeed(5), property.WithoutFailFile())

	fmt.Println(res.Outcome)
	fmt.Println("panic value:", res.Panic.Value)
	fmt.Println("has stack:", res.Panic.Stack != "")
	fmt.Println("assertion failures:", len(res.Failures))
	// Output:
	// panicked
	// panic value: n too large: 11
	// has stack: true
	// assertion failures: 0
}

// reverseTwice is the invariant behind FuzzReverseTwice.
func reverseTwice(p *property.T) {
	s := property.Draw(p, rapid.String(), "s")
	r := []rune(s)
	slices.Reverse(r)
	slices.Reverse(r)
	p.Expect(string(r)).To(assert.Equal(s))
}

// FuzzReverseTwice turns the same property into a native fuzz target. Under plain `go test` it only
// replays the f.Add seeds and testdata/fuzz/FuzzReverseTwice, which is fast and deterministic. The
// coverage-guided search is opt-in:
//
//	go test -run '^$' -fuzz '^FuzzReverseTwice$' -fuzztime 30s ./examples/
func FuzzReverseTwice(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte("seed corpus entry"))
	f.Add([]byte{0xff, 0x00, 0x7f, 0x80})
	f.Fuzz(property.Fuzz(reverseTwice))
}

// Properties compose with the go-specs matchers. EveryElement checks a rule on each item of a
// generated collection, and the failure message points at the element that broke it.
func TestProperty_withEveryElement(t *testing.T) {
	property.Check(t, func(p *property.T) {
		xs := property.Draw(p, rapid.SliceOf(rapid.IntRange(1, 50)), "xs")
		doubled := make([]int, len(xs))
		for i, x := range xs {
			doubled[i] = x * 2
		}
		p.Expect(doubled).To(assert.EveryElement(assert.BeGreaterThan(1)))
	}, property.WithoutFailFile())
}

// A round trip is the classic property: encode then decode gives back the value. MatchJSON compares
// documents by meaning, so key order and whitespace do not matter.
func TestProperty_jsonRoundTrip(t *testing.T) {
	type user struct {
		Name string   `json:"name"`
		Age  int      `json:"age"`
		Tags []string `json:"tags"`
	}
	userGen := rapid.Custom(func(t *rapid.T) user {
		return user{
			Name: rapid.StringMatching(`[a-zA-Z]{0,10}`).Draw(t, "name"),
			Age:  rapid.IntRange(0, 150).Draw(t, "age"),
			Tags: rapid.SliceOfN(rapid.StringMatching(`[a-z]{1,4}`), 0, 3).Draw(t, "tags"),
		}
	})

	property.Check(t, func(p *property.T) {
		u := property.Draw(p, userGen, "user")

		first, err := json.Marshal(u)
		p.Expect(err).To(assert.BeNil())

		var decoded user
		p.Expect(json.Unmarshal(first, &decoded)).To(assert.BeNil())

		second, err := json.Marshal(decoded)
		p.Expect(err).To(assert.BeNil())
		p.Expect(second).To(assert.MatchJSON(first))
	}, property.WithoutFailFile())
}
