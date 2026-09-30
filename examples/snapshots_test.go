// snapshots_test.go shows snapshot testing with ctx.Snapshot and the snapshots package.
//
// A snapshot stores the JSON form of a value the first time you accept it, and later runs fail
// when the value changes. Use it for output that is large or awkward to write by hand (an API
// response, a computed receipt, a rendered structure), where "did anything change?" is the real
// question. For a few exact facts, ctx.Expect(...).ToEqual(...) is clearer.
//
// How it works:
//
//   - ctx.Snapshot("name", value) marshals value with encoding/json and compares it to the entry
//     "name" in __snapshots__/<test file basename>.snap.json, next to the test file. For this file
//     that is examples/__snapshots__/snapshots_test.snap.json.
//   - The name is the key. Names must be non-empty and unique within one test file: two specs that
//     use the same name share one entry.
//   - Snapshots are created and updated only when GO_SPECS_UPDATE_SNAPSHOTS=1 is set:
//     GO_SPECS_UPDATE_SNAPSHOTS=1 go test ./examples/ -run TestSnapshots
//     Without it, a missing snapshot is a failure ("run with GO_SPECS_UPDATE_SNAPSHOTS=1 to
//     create"), so a forgotten snapshot cannot pass silently. Review the diff of the .snap.json
//     file like any other code change.
//   - The comparison is by JSON value, not by bytes: object key order and whitespace do not
//     matter, and numbers compare by exact value (1, 1.0 and 1e0 are equal, and integers above 2^53
//     keep every digit instead of being rounded through float64).
//   - Anything encoding/json can marshal works: maps, structs (use json tags for stable names),
//     slices. A value that cannot be marshalled fails the snapshot with the marshal error.
//
// Keep snapshotted data deterministic: no timestamps, random ids or map iteration order leaking
// into slices. Normalize those before calling Snapshot.
//
// The Examples at the bottom use snapshots.Evaluate on a throwaway directory to print the failure
// messages without failing anything.
package examples_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getsyntegrity/go-specs/snapshots"
	"github.com/getsyntegrity/go-specs/specs"
)

// The minimal case: one value, one name.
func TestSnapshots_basicMap(t *testing.T) {
	specs.Describe(t, "user service", func(s *specs.Spec) {
		s.It("creates user snapshot", func(ctx *specs.Context) {
			user := map[string]any{
				"id":   123,
				"name": "alice",
			}
			ctx.Snapshot("create-user", user)
		})
	})
}

// snapshotsAddress and snapshotsProfile are ordinary structs. JSON tags decide the field names in
// the snapshot, so renaming a Go field does not change it, and omitempty keeps it small.
type snapshotsAddress struct {
	Street string `json:"street"`
	City   string `json:"city"`
}

type snapshotsProfile struct {
	ID      int              `json:"id"`
	Name    string           `json:"name"`
	Emails  []string         `json:"emails"`
	Address snapshotsAddress `json:"address"`
	Nick    string           `json:"nick,omitempty"`
}

// Structs, nested values and slices are snapshotted as JSON. A slice keeps its order, so sort
// anything that comes from a map before snapshotting it.
func TestSnapshots_structWithNesting(t *testing.T) {
	specs.Describe(t, "profile", func(s *specs.Spec) {
		s.It("snapshots a nested struct", func(ctx *specs.Context) {
			ctx.Snapshot("profile-struct", snapshotsProfile{
				ID:      7,
				Name:    "Ada Lovelace",
				Emails:  []string{"ada@example.com", "ada@work.example"},
				Address: snapshotsAddress{Street: "12 Analytical Way", City: "London"},
			})
		})
	})
}

// One spec can hold several snapshots as long as each has its own name. Use a name that says what
// the value is, so a failure in the diff is easy to place.
func TestSnapshots_severalNamesInOneSpec(t *testing.T) {
	specs.Describe(t, "checkout summary", func(s *specs.Spec) {
		s.It("snapshots the request and the response separately", func(ctx *specs.Context) {
			ctx.Snapshot("checkout-request", map[string]any{"cart": []string{"sku-1", "sku-2"}, "coupon": nil})
			ctx.Snapshot("checkout-response", map[string]any{"status": "accepted", "total": 42.5})
		})
	})
}

// nil and empty are different in JSON: a nil slice is null and an empty one is []. The snapshot
// records the difference, so a change from one to the other is caught.
func TestSnapshots_nilAndEmpty(t *testing.T) {
	specs.Describe(t, "nil versus empty", func(s *specs.Spec) {
		s.It("keeps null and [] apart", func(ctx *specs.Context) {
			var nothing []string
			ctx.Snapshot("empty-and-nil-slices", map[string]any{
				"nil":   nothing,
				"empty": []string{},
			})
		})
	})
}

// Snapshot works in ItParallel too: the snapshot file is guarded by a per-file lock, so parallel
// specs writing under different names cannot clobber each other.
func TestSnapshots_inParallelSpecs(t *testing.T) {
	specs.Describe(t, "parallel snapshots", func(s *specs.Spec) {
		s.ItParallel("first", func(ctx *specs.Context) {
			ctx.Snapshot("parallel-first", map[string]int{"n": 1})
		})
		s.ItParallel("second", func(ctx *specs.Context) {
			ctx.Snapshot("parallel-second", map[string]int{"n": 2})
		})
	})
}

// --- Update mode and diagnostics, on a throwaway directory ------------------------------------

// snapshotsTempTestFile returns a fake "test file" path inside a fresh temporary directory (the
// snapshot lands in its __snapshots__ folder) and a cleanup function.
func snapshotsTempTestFile() (string, func()) {
	dir, err := os.MkdirTemp("", "snapshots-example-")
	if err != nil {
		panic(err)
	}
	return filepath.Join(dir, "demo_test.go"), func() { _ = os.RemoveAll(dir) }
}

// snapshotsUpdateMode switches GO_SPECS_UPDATE_SNAPSHOTS for the duration of fn, restoring the
// previous value afterwards. In a real workflow you set it in the shell instead.
func snapshotsUpdateMode(value string, fn func()) {
	old, had := os.LookupEnv(snapshots.UpdateSnapshotsEnv)
	_ = os.Setenv(snapshots.UpdateSnapshotsEnv, value)
	defer func() {
		if had {
			_ = os.Setenv(snapshots.UpdateSnapshotsEnv, old)
		} else {
			_ = os.Unsetenv(snapshots.UpdateSnapshotsEnv)
		}
	}()
	fn()
}

// snapshots.Evaluate is what ctx.Snapshot calls. It returns the verdict instead of failing, which
// makes the messages easy to show. A snapshot that does not exist yet is a failure with the hint
// to run in update mode.
func Example_snapshotsMissingSnapshot() {
	file, cleanup := snapshotsTempTestFile()
	defer cleanup()

	snapshotsUpdateMode("", func() {
		res := snapshots.Evaluate(nil, file, "receipt", map[string]int{"total": 10})
		fmt.Println(res.Passed)
		fmt.Println(res.Message)
	})
	// Output:
	// false
	// snapshot "receipt" missing; run with GO_SPECS_UPDATE_SNAPSHOTS=1 to create
}

// The full cycle: update mode creates the entry, a normal run passes, and a changed value fails
// with both sides (stored and current JSON) in the message, so the difference can be read without
// opening the file.
func Example_snapshotsUpdateThenCompare() {
	file, cleanup := snapshotsTempTestFile()
	defer cleanup()

	snapshotsUpdateMode("1", func() {
		res := snapshots.Evaluate(nil, file, "receipt", map[string]int{"total": 10})
		fmt.Println("create:", res.Passed)
	})
	snapshotsUpdateMode("", func() {
		fmt.Println("same value:", snapshots.Evaluate(nil, file, "receipt", map[string]int{"total": 10}).Passed)

		res := snapshots.Evaluate(nil, file, "receipt", map[string]int{"total": 11})
		fmt.Println("changed value:", res.Passed)
		// The message shows both sides (stored and current JSON); print its title and the change.
		title, _, _ := strings.Cut(res.Message, "\n")
		fmt.Println(title)
		fmt.Println(strings.Contains(res.Message, `"total": 10`), strings.Contains(res.Message, `"total": 11`))
	})
	// Output:
	// create: true
	// same value: true
	// changed value: false
	// snapshot "receipt" mismatch:
	// true true
}

// The comparison is semantic. Key order and number spelling do not matter, and integers larger
// than 2^53 keep every digit, so two adjacent 64-bit ids are never confused with each other.
func Example_snapshotsComparisonIsSemantic() {
	file, cleanup := snapshotsTempTestFile()
	defer cleanup()

	// A snapshot file written by hand (or by another tool): different key order and number forms.
	dir := filepath.Join(filepath.Dir(file), "__snapshots__")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		panic(err)
	}
	stored := `{"order": {"total": 1.0, "id": 9007199254740993, "lines": ["a", "b"]}}`
	if err := os.WriteFile(filepath.Join(dir, "demo_test.snap.json"), []byte(stored), 0o644); err != nil {
		panic(err)
	}

	snapshotsUpdateMode("", func() {
		sameValue := map[string]any{"lines": []string{"a", "b"}, "id": int64(9007199254740993), "total": 1}
		fmt.Println(snapshots.Evaluate(nil, file, "order", sameValue).Passed)

		neighbourID := map[string]any{"lines": []string{"a", "b"}, "id": int64(9007199254740992), "total": 1}
		fmt.Println(snapshots.Evaluate(nil, file, "order", neighbourID).Passed)

		reordered := map[string]any{"lines": []string{"b", "a"}, "id": int64(9007199254740993), "total": 1}
		fmt.Println(snapshots.Evaluate(nil, file, "order", reordered).Passed) // slices keep order
	})
	// Output:
	// true
	// false
	// false
}

// An empty name is a usage error, reported instead of silently writing an entry under "".
func Example_snapshotsEmptyNameIsRejected() {
	file, cleanup := snapshotsTempTestFile()
	defer cleanup()

	res := snapshots.Evaluate(nil, file, "", "value")
	fmt.Println(res.Passed, res.Message)
	// Output: false snapshot name cannot be empty
}

// A value encoding/json cannot marshal fails with the marshal error, so snapshot only plain data
// (no channels, funcs or cyclic structures).
func Example_snapshotsUnmarshalableValue() {
	file, cleanup := snapshotsTempTestFile()
	defer cleanup()

	res := snapshots.Evaluate(nil, file, "bad", map[string]any{"ch": make(chan int)})
	fmt.Println(res.Passed)
	fmt.Println(res.Message)
	// Output:
	// false
	// snapshot: marshal: json: unsupported type: chan int
}
