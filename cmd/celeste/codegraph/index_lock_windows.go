//go:build windows

package codegraph

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// lockIsNoop reports whether lockFile excludes nothing on this platform.
const lockIsNoop = false

// lockFile takes an exclusive LockFileEx lock on the file's first byte
// without blocking. The lock belongs to the handle, so two Indexers in one
// process exclude each other too.
func lockFile(f *os.File) error {
	err := windows.LockFileEx(windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, new(windows.Overlapped))
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_IO_PENDING) {
		return errLocked
	}
	if errors.Is(err, windows.ERROR_NOT_SUPPORTED) || errors.Is(err, windows.ERROR_INVALID_FUNCTION) {
		return errLockUnsupported
	}
	return err
}

func unlockFile(f *os.File) error {
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, new(windows.Overlapped))
}
