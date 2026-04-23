package storage

import (
	"fmt"
	"os"
)

// AtomicWriteFile writes data to path via a ".tmp" sibling + fsync + rename.
// Guarantees that callers never observe a half-written file at `path` — the
// rename happens after the bytes are on disk, and is atomic when path.tmp
// and path are on the same filesystem (always the case here).
//
// If any step fails, the tmp file is removed. The destination file, if one
// already existed, stays untouched on failure.
func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("open tmp: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("fsync tmp: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename tmp -> path: %w", err)
	}
	return nil
}
