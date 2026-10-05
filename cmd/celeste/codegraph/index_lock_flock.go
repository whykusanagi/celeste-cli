//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package codegraph

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// lockIsNoop reports whether lockFile excludes nothing on this platform.
const lockIsNoop = false

// lockFile takes an exclusive flock(2) without blocking. flock locks belong
// to the open file, so two Indexers in one process exclude each other too.
func lockFile(f *os.File) error {
	for {
		err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		switch {
		case err == nil:
			return nil
		case errors.Is(err, unix.EINTR):
			continue
		case errors.Is(err, unix.EWOULDBLOCK):
			return errLocked
		case errors.Is(err, unix.ENOLCK), errors.Is(err, unix.ENOTSUP), errors.Is(err, unix.EOPNOTSUPP):
			return errLockUnsupported
		default:
			return err
		}
	}
}

func unlockFile(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_UN)
}
