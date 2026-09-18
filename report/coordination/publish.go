package coordination

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// ErrDuplicateProducer reports that a final destination already existed at publish time.
//
// It is never silently tolerated: two shards for the same package path in one run means either a
// producer ran twice or two packages resolved to one identity, and both are facts the finalizer
// must be able to see (contract v1.2.7 §5).
var ErrDuplicateProducer = errors.New("go-specs report: a file for this producer identity was already published")

// publishOps is the injection seam for the create-no-replace publish. The NFS false-EEXIST branch
// is the contract's only network-filesystem defence and is written once, never exercised, and
// load-bearing exactly when least observable — so it is built to be testable rather than left to
// be correct by inspection (contract v1.2.7 §10, §13).
type publishOps struct {
	link      func(oldname, newname string) error
	linkCount func(path string) (uint64, error)
	remove    func(name string) error
}

func realPublishOps() publishOps {
	return publishOps{link: os.Link, linkCount: linkCount, remove: os.Remove}
}

// tempName is the temp file's name, inside the destination directory and carrying this process's
// PID.
//
// Both properties are load-bearing, not cosmetic. Living in the destination directory is what
// keeps the publish atomic: neither rename nor link works across a filesystem boundary on POSIX
// (link fails EXDEV), so a temp file in /tmp or on another mount cannot be published at all. The
// PID is what makes the file this process's alone, which is one of the two preconditions the
// st_nlink recovery below rests on.
func tempName(finalName string) string {
	return fmt.Sprintf("%s.tmp-%d", finalName, os.Getpid())
}

// writeAndPublish serializes body into a temp file inside dir and publishes it at finalName with
// an atomic create-no-replace operation.
//
// The sequence is write, Sync, Close, publish — in that order, so a shard becomes discoverable
// only once it is complete and durable. The finalizer only ever globs the final naming pattern
// and never opens a .tmp-* file, so a shard is atomically either wholly absent or wholly present.
func writeAndPublish(dir, finalName string, body []byte, ops publishOps) error {
	tmpPath := filepath.Join(dir, tempName(finalName))
	finalPath := filepath.Join(dir, finalName)

	f, err := openFileNoFollow(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
	if err != nil {
		return fmt.Errorf("go-specs report: create %s: %w", tmpPath, err)
	}
	if err := writeAndSync(f, body); err != nil {
		_ = f.Close()
		_ = ops.remove(tmpPath)
		return fmt.Errorf("go-specs report: write %s: %w", tmpPath, err)
	}
	if err := f.Close(); err != nil {
		_ = ops.remove(tmpPath)
		return fmt.Errorf("go-specs report: close %s: %w", tmpPath, err)
	}
	return publishExclusive(tmpPath, finalPath, ops)
}

// publishExclusive links tmpPath to finalPath and removes tmpPath.
//
// os.Rename is NOT an acceptable substitute here, and neither is "check the destination is
// absent, then rename". os.Rename replaces an existing destination silently on both supported
// platform families, and the check-then-rename shape is a TOCTOU race: two producers can both
// observe an absent destination and the later rename then destroys the earlier file. The run
// marker does not rescue that check either — it excludes foreign invocations from the run
// directory, it does not serialize producers inside the legitimate one, which is precisely the
// case a duplicate-producer error must catch (contract v1.2.7 §5).
//
// The atomicity therefore has to come from a single filesystem operation that fails when the
// destination exists: link(2), which returns EEXIST.
func publishExclusive(tmpPath, finalPath string, ops publishOps) error {
	err := ops.link(tmpPath, finalPath)
	if err == nil {
		// The temp file's second name is no longer needed. This is the only unlink this package
		// performs during a run, and it targets the temp name — never a published file.
		//
		// A failure here is deliberately NOT returned. The shard is already linked, complete and
		// durable at its final name, and the finalizer globs only the final pattern and never
		// opens a .tmp-* file. The sole consequence is litter, which the run's own cleanup or gc
		// removes later. Reporting it as a publish failure would turn a successful publish into
		// an error the caller has no way to act on.
		_ = ops.remove(tmpPath)
		return nil
	}

	if !errors.Is(err, fs.ErrExist) {
		_ = ops.remove(tmpPath)
		return fmt.Errorf("go-specs report: publish %s: %w", finalPath, err)
	}

	// EEXIST is ambiguous over NFS, and it produces the OPPOSITE error from the one an
	// implementer expects: the server can perform the link, drop the reply, and the client's
	// automatic retry then collides with the client's own earlier, successful link. Treating that
	// as a duplicate would fail a perfectly correct publish.
	//
	// Checking the TEMP file's link count — not the destination's — is what preserves genuine
	// duplicate detection. If our own link succeeded the temp file has two names; if another
	// producer created the destination first, our temp file still has exactly one.
	//
	// The inference rests on two preconditions: the temp file is linked nowhere else (it is
	// created by this process, inside the destination directory, under a PID-bearing name, and
	// nothing here links it a second time), and nothing unlinks a published file while the run is
	// in flight (no producer has delete authority, and gc runs only out-of-run). Any new linking
	// or pruning path invalidates it.
	if n, statErr := ops.linkCount(tmpPath); statErr == nil && n == 2 {
		// Same as the success path: the publish already happened, so cleanup cannot fail it.
		_ = ops.remove(tmpPath)
		return nil
	}

	_ = ops.remove(tmpPath)
	return fmt.Errorf("%w: %s", ErrDuplicateProducer, finalPath)
}
