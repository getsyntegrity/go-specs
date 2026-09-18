package coordination

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
)

func TestWriteAndPublishProducesACompleteFileAndLeavesNoTemp(t *testing.T) {
	dir := secureTempDir(t)
	if err := writeAndPublish(dir, "a--b.shard.json", []byte(`{"x":1}`), realPublishOps()); err != nil {
		t.Fatalf("writeAndPublish: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "a--b.shard.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"x":1}` {
		t.Fatalf("published content = %q", got)
	}
	if mode := modeOf(t, filepath.Join(dir, "a--b.shard.json")); mode != 0o600 {
		t.Fatalf("published file mode = %04o, want 0600", mode)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Fatalf("a temp file survived publication: %s", e.Name())
		}
	}
	if len(entries) != 1 {
		t.Fatalf("published %d entries, want exactly 1: %v", len(entries), entries)
	}
}

func TestTempFileLivesInTheDestinationDirectoryAndCarriesThePID(t *testing.T) {
	// Claims A3 and A6, asserted structurally. Living in the destination directory is what keeps
	// the publish atomic — link fails EXDEV across a filesystem boundary — and provoking a real
	// EXDEV would need two mounts, which is not portable in CI. The PID is what makes the temp
	// file this process's alone, which the st_nlink recovery depends on.
	name := tempName("pkg--abc.shard.json")
	if filepath.Dir(name) != "." {
		t.Fatalf("tempName returned a path with a directory component: %q", name)
	}
	if !strings.HasSuffix(name, ".tmp-"+strconv.Itoa(os.Getpid())) {
		t.Fatalf("tempName = %q, want a .tmp-<pid> suffix", name)
	}
	if !strings.HasPrefix(name, "pkg--abc.shard.json") {
		t.Fatalf("tempName = %q, want it derived from the final name", name)
	}
}

func TestPublishRefusesAnExistingDestinationAndLeavesItIntact(t *testing.T) {
	dir := secureTempDir(t)
	final := filepath.Join(dir, "a--b.shard.json")
	if err := os.WriteFile(final, []byte("first writer"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := writeAndPublish(dir, "a--b.shard.json", []byte("second writer"), realPublishOps())
	if !errors.Is(err, ErrDuplicateProducer) {
		t.Fatalf("got %v, want ErrDuplicateProducer", err)
	}

	got, err := os.ReadFile(final)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "first writer" {
		t.Fatalf("the earlier file was replaced: %q — os.Rename semantics have leaked in", got)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("a failed publish left litter behind: %v", entries)
	}
}

func TestConcurrentPublishersOfOneDestinationProduceExactlyOneWinner(t *testing.T) {
	// Claim A1. Each racer gets its own temp file, standing in for a separate package process:
	// within one process tempName is PID-derived and therefore already unique per destination.
	dir := secureTempDir(t)
	final := filepath.Join(dir, "contended--x.shard.json")
	const racers = 24

	tmps := make([]string, racers)
	for i := range tmps {
		tmps[i] = filepath.Join(dir, fmt.Sprintf("contended--x.shard.json.tmp-%d", i))
		if err := os.WriteFile(tmps[i], []byte(strconv.Itoa(i)), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	var wg sync.WaitGroup
	errs := make([]error, racers)
	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = publishExclusive(tmps[i], final, realPublishOps())
		}(i)
	}
	close(start)
	wg.Wait()

	winners := 0
	for i, err := range errs {
		switch {
		case err == nil:
			winners++
		case errors.Is(err, ErrDuplicateProducer):
		default:
			t.Fatalf("racer %d failed with an unexpected error: %v", i, err)
		}
	}
	if winners != 1 {
		t.Fatalf("%d of %d concurrent publishes succeeded, want exactly 1", winners, racers)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("the losing racers left temp files behind: %v", entries)
	}
}

// eexist reproduces what os.Link returns when the destination is taken.
func eexist(old, new string) error {
	return &os.LinkError{Op: "link", Old: old, New: new, Err: syscall.EEXIST}
}

func TestNFSFalseEEXISTIsAcceptedWhenOurOwnLinkSucceeded(t *testing.T) {
	// Claim A2, the arm that has never run in production and is load-bearing exactly when least
	// observable. Over NFS, link can report EEXIST for an operation the server actually performed
	// — the reply was dropped and the client's retry collided with its own earlier link. The temp
	// file then has two names, which is how we know the publish succeeded.
	var removed []string
	ops := publishOps{
		link:      func(old, new string) error { return eexist(old, new) },
		linkCount: func(string) (uint64, error) { return 2, nil },
		remove:    func(name string) error { removed = append(removed, name); return nil },
	}

	if err := publishExclusive("/run/shards/x.tmp-1", "/run/shards/x", ops); err != nil {
		t.Fatalf("a publish that actually succeeded was reported as a failure: %v", err)
	}
	if len(removed) != 1 || removed[0] != "/run/shards/x.tmp-1" {
		t.Fatalf("removed %v, want only the temp file", removed)
	}
}

func TestGenuineDuplicateIsStillReportedWhenTheTempFileHasOneName(t *testing.T) {
	// The other arm. If another producer created the destination first, our temp file still has
	// exactly one name — which is why the link count is taken on the TEMP file and not on the
	// destination. Checking the destination would report 1 in both cases and collapse the two.
	ops := publishOps{
		link:      func(old, new string) error { return eexist(old, new) },
		linkCount: func(string) (uint64, error) { return 1, nil },
		remove:    func(string) error { return nil },
	}

	err := publishExclusive("/run/shards/x.tmp-1", "/run/shards/x", ops)
	if !errors.Is(err, ErrDuplicateProducer) {
		t.Fatalf("got %v, want ErrDuplicateProducer", err)
	}
}

func TestDuplicateIsReportedWhenTheLinkCountCannotBeObtained(t *testing.T) {
	// Fail closed: an unreadable link count is not evidence that the publish succeeded.
	ops := publishOps{
		link:      func(old, new string) error { return eexist(old, new) },
		linkCount: func(string) (uint64, error) { return 0, errors.New("stat failed") },
		remove:    func(string) error { return nil },
	}

	err := publishExclusive("/run/shards/x.tmp-1", "/run/shards/x", ops)
	if !errors.Is(err, ErrDuplicateProducer) {
		t.Fatalf("got %v, want ErrDuplicateProducer", err)
	}
}

func TestPublishNeverRemovesAPublishedFile(t *testing.T) {
	// The second precondition the st_nlink inference rests on: nothing may unlink a published
	// file while the run is in flight. Producers have no delete authority at all.
	for name, tc := range map[string]struct {
		link      func(string, string) error
		linkCount func(string) (uint64, error)
	}{
		"success":            {func(string, string) error { return nil }, nil},
		"false eexist":       {func(o, n string) error { return eexist(o, n) }, func(string) (uint64, error) { return 2, nil }},
		"genuine duplicate":  {func(o, n string) error { return eexist(o, n) }, func(string) (uint64, error) { return 1, nil }},
		"unexpected failure": {func(string, string) error { return errors.New("boom") }, nil},
	} {
		t.Run(name, func(t *testing.T) {
			var removed []string
			ops := publishOps{
				link:      tc.link,
				linkCount: tc.linkCount,
				remove:    func(n string) error { removed = append(removed, n); return nil },
			}
			_ = publishExclusive("/run/shards/x.tmp-1", "/run/shards/x", ops)
			for _, r := range removed {
				if r == "/run/shards/x" {
					t.Fatal("a published file was unlinked; the st_nlink recovery is no longer sound")
				}
			}
		})
	}
}

func TestWriteAndPublishRefusesASymlinkedTempPath(t *testing.T) {
	dir := secureTempDir(t)
	elsewhere := secureTempDir(t)
	victim := filepath.Join(elsewhere, "victim")
	if err := os.WriteFile(victim, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(dir, tempName("a--b.shard.json"))); err != nil {
		t.Fatal(err)
	}

	if err := writeAndPublish(dir, "a--b.shard.json", []byte("payload"), realPublishOps()); err == nil {
		t.Fatal("writeAndPublish wrote through a symlinked temp path")
	}
	got, err := os.ReadFile(victim)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "original" {
		t.Fatalf("the symlink target was modified: %q", got)
	}
}

func TestPublishSurfacesANonEEXISTFailureAsItself(t *testing.T) {
	ops := publishOps{
		link:      func(string, string) error { return fs.ErrPermission },
		linkCount: func(string) (uint64, error) { return 2, nil },
		remove:    func(string) error { return nil },
	}
	err := publishExclusive("/run/shards/x.tmp-1", "/run/shards/x", ops)
	if err == nil {
		t.Fatal("a permission failure was swallowed")
	}
	if errors.Is(err, ErrDuplicateProducer) {
		t.Fatalf("a permission failure was misreported as a duplicate: %v", err)
	}
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("the underlying cause was lost: %v", err)
	}
}
