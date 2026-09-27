// Package shared is a small dependency imported by both e2efixture producer packages, so that
// `-coverpkg` instruments its blocks from two independent test binaries at once — the overlap
// scenario contract v1.2.8 §9 (finding F7) requires the finalizer to deduplicate.
package shared

// Compute returns n doubled when n is positive, and zero otherwise. The two branches are
// deliberately exercised by different producer packages (producera calls it with a positive n,
// producerb with a non-positive one), so every block here is linked and instrumented into both
// test binaries regardless of which branch a given binary actually took.
func Compute(n int) int {
	if n > 0 {
		return n * 2
	}
	return 0
}
