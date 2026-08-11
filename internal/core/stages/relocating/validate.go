package relocating

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var (
	ErrDestNotDirectory  = errors.New("relocating: destination exists and is not a directory")
	ErrDestNotWritable   = errors.New("relocating: destination is not writable")
	ErrDestIsCurrentRoot = errors.New("relocating: destination equals the current work root")
	ErrDestInsideCurrent = errors.New("relocating: destination is inside the current work root")
)

// validate applies Q6's rules: reject a non-directory, non-writable target,
// the current root itself, or a path inside the current root. A non-empty
// target is intentionally NOT rejected here — Q6 says "warn, don't block",
// and there is no UI in this phase (Q5) to show a warning to, so
// ChangeWorkRoot proceeds unconditionally on a non-empty-but-otherwise-
// valid target.
func validate(dst string, refs WorkRootRefs) error {
	if dst == "" {
		return errors.New("relocating: destination must not be empty")
	}
	if !filepath.IsAbs(dst) {
		return errors.New("relocating: destination must be an absolute path")
	}

	currentRoot := refs.Root.Load()
	cleanDst := filepath.Clean(dst)
	cleanCurrent := filepath.Clean(currentRoot.Name())

	if cleanDst == cleanCurrent {
		return ErrDestIsCurrentRoot
	}
	if isSubPath(cleanCurrent, cleanDst) {
		return ErrDestInsideCurrent
	}
	// A pure string compare above misses a destination that is the SAME
	// physical directory as the current root via a symlink, or (on a
	// case-insensitive filesystem, e.g. default macOS APFS) a case-only
	// spelling difference — either would pass buildNewRoot, and copyKey's
	// GetStream(src)-then-PutStream(dst, O_TRUNC) on what turns out to be
	// the SAME underlying file would truncate the source while still
	// reading it. os.Stat follows symlinks and resolves case-insensitively
	// to the same inode, so os.SameFile catches both.
	if same, err := sameDirectory(cleanCurrent, cleanDst); err != nil {
		return fmt.Errorf("relocating: compare destination to current root: %w", err)
	} else if same {
		return ErrDestIsCurrentRoot
	}

	info, err := os.Stat(dst)
	if err != nil {
		if os.IsNotExist(err) {
			return checkNearestExistingAncestorWritable(dst)
		}
		return fmt.Errorf("relocating: stat destination: %w", err)
	}
	if !info.IsDir() {
		return ErrDestNotDirectory
	}
	return checkWritable(dst)
}

// sameDirectory reports whether a and b resolve to the same physical
// directory (same device+inode on Unix, same file index on Windows) once
// symlinks and filesystem case-folding are resolved by os.Stat. Either
// path not existing yet is not an error here — it simply can't be the same
// directory as one that does exist.
func sameDirectory(a, b string) (bool, error) {
	infoA, err := os.Stat(a)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	infoB, err := os.Stat(b)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return os.SameFile(infoA, infoB), nil
}

// isSubPath reports whether child is strictly nested inside parent.
func isSubPath(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	if rel == "." {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func checkWritable(dir string) error {
	f, err := os.CreateTemp(dir, ".relocate-write-test-*")
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDestNotWritable, err)
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return nil
}

// checkNearestExistingAncestorWritable walks up from dst until it finds an
// existing ancestor directory, then checks that one is writable — dst
// itself doesn't exist yet (buildNewRoot creates it via MkdirAll).
func checkNearestExistingAncestorWritable(dst string) error {
	dir := dst
	for {
		info, err := os.Stat(dir)
		if err == nil {
			if !info.IsDir() {
				return ErrDestNotDirectory
			}
			return checkWritable(dir)
		}
		if !os.IsNotExist(err) {
			return fmt.Errorf("relocating: stat %s: %w", dir, err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return fmt.Errorf("relocating: no existing ancestor directory found for %s", dst)
		}
		dir = parent
	}
}
