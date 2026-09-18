package coordination

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const (
	dirMode  fs.FileMode = 0o700
	fileMode fs.FileMode = 0o600
)

// openFileNoFollow opens path without traversing a symlink at its final component.
//
// On Unix the kernel enforces this atomically through O_NOFOLLOW. Elsewhere there is no portable
// equivalent, so the fallback is an lstat pre-check — which is a TOCTOU race and is documented as
// such rather than presented as equivalent.
func openFileNoFollow(path string, flag int, perm fs.FileMode) (*os.File, error) {
	if !noFollowSupported {
		if info, err := os.Lstat(path); err == nil && info.Mode()&fs.ModeSymlink != 0 {
			return nil, fmt.Errorf("go-specs report: refusing to open %s: it is a symlink", path)
		}
	}
	return os.OpenFile(path, flag|oNoFollow, perm)
}

// ensureSafeBaseDir refuses to operate when the base directory, or any of its existing ancestors,
// is group- or other-writable without the sticky bit (contract v1.2.6 §10, rule 2).
//
// If any ancestor is writable by someone else, another local user can swap a directory component
// and every downstream check — exclusive marker creation, create-no-replace publication,
// O_NOFOLLOW — becomes meaningless, because the path those checks defend is no longer the path
// they think it is. This is a startup check in InitializeRun and in every producer before it
// writes.
//
// The sticky exemption is what keeps /tmp-style scratch usable: on a sticky directory only the
// owner of an entry may remove or rename it, which is the property the check actually needs.
func ensureSafeBaseDir(dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("go-specs report: resolve %s: %w", dir, err)
	}
	for path := abs; ; {
		info, err := os.Lstat(path)
		switch {
		case err == nil:
			if info.Mode()&fs.ModeSymlink == 0 {
				if err := checkNotForeignWritable(path, info); err != nil {
					return err
				}
			}
		case errors.Is(err, fs.ErrNotExist):
			// Not yet created; this implementation will create it with mode 0700.
		default:
			return fmt.Errorf("go-specs report: inspect %s: %w", path, err)
		}

		parent := filepath.Dir(path)
		if parent == path {
			return nil
		}
		path = parent
	}
}

func checkNotForeignWritable(path string, info fs.FileInfo) error {
	mode := info.Mode()
	if mode&fs.ModeSticky != 0 {
		return nil
	}
	if perm := mode.Perm(); perm&0o022 != 0 {
		return fmt.Errorf(
			"go-specs report: refusing to use the reporting directory: %s has mode %04o, which lets another local user replace a path component and defeat every ownership and no-replace check below it; make it group- and other-unwritable (chmod go-w %s) or point %s somewhere private",
			path, perm, path, EnvReportDir)
	}
	return nil
}

// mkdirSecure creates dir with mode 0700 and verifies the result.
//
// Verification is not defensive noise: umask does not apply to a subsequent Chmod, and a
// directory that already existed was not created by this implementation at all, so its mode is
// evidence about who else can reach it (contract v1.2.6 §10, rule 3). A pre-existing directory
// with a wider mode is refused rather than quietly tightened — tightening it would hide the fact
// that something else created the path first.
func mkdirSecure(dir string) error {
	info, err := os.Lstat(dir)
	switch {
	case err == nil:
		if info.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("go-specs report: refusing to use %s: it is a symlink, not a directory", dir)
		}
		if !info.IsDir() {
			return fmt.Errorf("go-specs report: refusing to use %s: it exists and is not a directory", dir)
		}
		if perm := info.Mode().Perm(); perm != dirMode {
			return fmt.Errorf(
				"go-specs report: refusing to use %s: it already exists with mode %04o, want %04o; this implementation did not create it, so its contents and permissions are not accounted for",
				dir, perm, dirMode)
		}
		return nil
	case errors.Is(err, fs.ErrNotExist):
		if err := os.MkdirAll(dir, dirMode); err != nil {
			return fmt.Errorf("go-specs report: create %s: %w", dir, err)
		}
		// umask does not apply to Chmod, so this is what actually guarantees the mode.
		if err := os.Chmod(dir, dirMode); err != nil {
			return fmt.Errorf("go-specs report: set mode on %s: %w", dir, err)
		}
		created, err := os.Lstat(dir)
		if err != nil {
			return fmt.Errorf("go-specs report: verify %s: %w", dir, err)
		}
		if perm := created.Mode().Perm(); perm != dirMode {
			return fmt.Errorf("go-specs report: %s was created with mode %04o, want %04o", dir, perm, dirMode)
		}
		return nil
	default:
		return fmt.Errorf("go-specs report: inspect %s: %w", dir, err)
	}
}
