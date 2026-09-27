package store

import (
	"errors"
	"syscall"
)

// Windows error codes returned when another process has the rename target
// open without FILE_SHARE_DELETE (an outside reader, a backup tool,
// antivirus).
const (
	errorAccessDenied     syscall.Errno = 5
	errorSharingViolation syscall.Errno = 32
	errorLockViolation    syscall.Errno = 33
)

func isRetryableRename(err error) bool {
	return errors.Is(err, errorSharingViolation) ||
		errors.Is(err, errorAccessDenied) ||
		errors.Is(err, errorLockViolation)
}

// syncDir is a no-op: Windows has no way to fsync a directory.
func syncDir(string) error { return nil }
