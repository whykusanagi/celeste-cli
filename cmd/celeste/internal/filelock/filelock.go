// Package filelock serializes a read-modify-write of a small shared file
// (the trust store) across celeste processes with an advisory lock on a
// sidecar file.
package filelock

import (
	"errors"
	"fmt"
	"os"
	"time"
)

// errBusy is tryLock's error when another holder has the lock.
var errBusy = errors.New("file lock busy")

// pollInterval is how often Lock retries a busy lock.
const pollInterval = 10 * time.Millisecond

// Lock takes an exclusive lock on path (created 0600 if missing; it is
// never removed, as removing a lock file lets two holders lock different
// files), waiting at most wait for another holder to let go. The lock
// belongs to the open file, so two holders in one process exclude each
// other too. unlock releases it. Where the platform has no file lock
// (Supported is false) Lock excludes nothing and always succeeds.
func Lock(path string, wait time.Duration) (unlock func(), err error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(wait)
	for {
		err = tryLock(f)
		if !errors.Is(err, errBusy) {
			break
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, fmt.Errorf("%s is held by another celeste process", path)
		}
		time.Sleep(pollInterval)
	}
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return func() {
		_ = unlockFile(f)
		_ = f.Close()
	}, nil
}
