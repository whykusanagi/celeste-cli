//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package filelock

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// Supported reports whether Lock excludes anything on this platform.
const Supported = true

func tryLock(f *os.File) error {
	for {
		err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		switch {
		case err == nil:
			return nil
		case errors.Is(err, unix.EINTR):
			continue
		case errors.Is(err, unix.EWOULDBLOCK):
			return errBusy
		case errors.Is(err, unix.ENOLCK), errors.Is(err, unix.ENOTSUP), errors.Is(err, unix.EOPNOTSUPP):
			// A file system without locks (some network mounts): go on
			// unlocked, as before there was a lock.
			return nil
		default:
			return err
		}
	}
}

func unlockFile(f *os.File) error { return unix.Flock(int(f.Fd()), unix.LOCK_UN) }
