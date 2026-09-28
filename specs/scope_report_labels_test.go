package specs

import "testing"

// TestDisambiguateSiblingNames pins the exact format docs/DSL.md documents (issue #275): the first
// occurrence of a name keeps it, each later occurrence gets "name#k" for the smallest k>=2 that
// collides with neither a literal sibling name nor an already-assigned label, and a suite with no
// duplicate name is untouched (nil, meaning "no changes").
func TestDisambiguateSiblingNames(t *testing.T) {
	cases := []struct {
		name  string
		names []string
		want  []string // nil means disambiguateSiblingNames must return nil (no duplicates)
	}{
		{name: "no duplicates", names: []string{"A", "B", "C"}, want: nil},
		{name: "single sibling", names: []string{"D"}, want: nil},
		{name: "two duplicates", names: []string{"D", "D"}, want: []string{"D", "D#2"}},
		{name: "three duplicates", names: []string{"D", "D", "D"}, want: []string{"D", "D#2", "D#3"}},
		{
			name:  "literal collision declared after the duplicate (docs/DSL.md example)",
			names: []string{"D", "D", "D#2"},
			want:  []string{"D", "D#3", "D#2"},
		},
		{
			name:  "literal collision declared before the duplicate",
			names: []string{"D", "D#2", "D"},
			want:  []string{"D", "D#2", "D#3"},
		},
		{
			name:  "two duplicate groups with different base names interleaved",
			names: []string{"A", "B", "A", "B", "A"},
			want:  []string{"A", "B", "A#2", "B#2", "A#3"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := disambiguateSiblingNames(tc.names)
			if !stringSlicesEqual(got, tc.want) {
				t.Fatalf("disambiguateSiblingNames(%q) = %q, want %q", tc.names, got, tc.want)
			}
		})
	}
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestComputeScopeLabelsNesting proves computeScopeLabels disambiguates duplicate sibling names
// independently per parent: the same name repeated in two different parents stays unsuffixed in
// both, and only siblings of the same parent compete for a suffix.
func TestComputeScopeLabelsNesting(t *testing.T) {
	// Scope tree:
	// 0 root (parent -1)
	//   1 "D" (parent 0)
	//   2 "D" (parent 0)
	//     3 "X" (parent 2)
	//     4 "X" (parent 2)
	//   5 "X" (parent 0) -- same literal name as 3/4 but a different parent
	parent := []int{-1, 0, 0, 2, 2, 0}
	name := []string{"root", "D", "D", "X", "X", "X"}
	labels := computeScopeLabels(parent, name)
	want := []string{"root", "D", "D#2", "X", "X#2", "X"}
	if !stringSlicesEqual(labels, want) {
		t.Fatalf("computeScopeLabels = %q, want %q", labels, want)
	}
}
