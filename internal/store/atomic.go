package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// renameRetries is the backoff schedule for a rename that fails because
// another process holds the target open (see isRetryableRename).
var renameRetries = []time.Duration{
	10 * time.Millisecond,
	20 * time.Millisecond,
	50 * time.Millisecond,
	100 * time.Millisecond,
	200 * time.Millisecond,
	500 * time.Millisecond,
}

// tmpName returns the temporary file used while writing path.
func tmpName(path string) string {
	dir, base := filepath.Split(path)
	return filepath.Join(dir, "."+base+".tmp")
}

// isTmpName reports whether a file name is a temporary file left by
// writeFileAtomic.
func isTmpName(name string) bool {
	return strings.HasPrefix(name, ".") && strings.HasSuffix(name, ".tmp")
}

// writeFileAtomic replaces path with data so that a reader, or a crash, sees
// either the old file or the new one, never a partial one.  On failure the old
// file is left intact.
func writeFileAtomic(path string, data []byte) (err error) {
	tmp := tmpName(path)
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			f.Close()
			os.Remove(tmp)
		}
	}()
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = renameWithRetry(tmp, path); err != nil {
		return err
	}
	// The new content is in place; a failed directory sync only weakens
	// durability across a power loss, so report it but the write happened.
	if err := syncDir(filepath.Dir(path)); err != nil {
		return fmt.Errorf("sync directory of %s: %w", path, err)
	}
	return nil
}

func renameWithRetry(from, to string) error {
	err := os.Rename(from, to)
	for _, d := range renameRetries {
		if err == nil || !isRetryableRename(err) {
			break
		}
		time.Sleep(d)
		err = os.Rename(from, to)
	}
	return err
}
