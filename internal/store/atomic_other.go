//go:build !windows

package store

import (
	"errors"
	"os"
	"syscall"
)

// On POSIX systems rename replaces an open target without complaint.
func isRetryableRename(error) bool { return false }

// syncDir makes a rename in dir durable.  Filesystems that cannot sync a
// directory report EINVAL or ENOTSUP, which is not an error for our purposes.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	if err := d.Sync(); err != nil &&
		!errors.Is(err, syscall.EINVAL) && !errors.Is(err, syscall.ENOTSUP) {
		return err
	}
	return nil
}
