package codegraph

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"time"
)

// ErrIndexBusy is returned by Update, and by Build after waiting
// buildLockWait, when another indexer (in this process or another one)
// holds the index's write lock (#392).
var ErrIndexBusy = errors.New("the code graph index is being written by another indexer")

// errLocked is what lockFile returns when another holder has the lock.
var errLocked = errors.New("file is locked")

// buildLockWait bounds how long an explicit Build waits for another
// indexer's lock; lockPoll is how often it retries. Vars so tests can
// shorten them.
var (
	buildLockWait = 2 * time.Minute
	lockPoll      = 100 * time.Millisecond
)

// testHookAfterPass1, when set, runs inside a full build after pass 1, with
// the index lock held.
var testHookAfterPass1 func()

// indexLock is an exclusive OS file lock on the lock file next to an index
// database. Build and Update hold it so two Indexers on one database (the
// MCP server's, an MCP chat Env's, the TUI's or the CLI's, in one process
// or several) never write it at once: buildMu serialises only one Indexer.
// The OS releases the lock when its process dies, so a lock file left by a
// killed process never blocks the next indexer.
type indexLock struct {
	f *os.File
}

// lockPath is the lock file for the index database at dbPath.
func lockPath(dbPath string) string { return dbPath + ".lock" }

// tryLockIndex takes the lock at path without waiting. It returns
// ErrIndexBusy when another holder has it. A file system that cannot lock
// files gets a lock that excludes nothing rather than no index at all.
func tryLockIndex(path string) (*indexLock, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open index lock: %w", err)
	}
	if err := lockFile(f); err != nil {
		if errors.Is(err, errLocked) {
			_ = f.Close()
			return nil, ErrIndexBusy
		}
		_ = f.Close()
		return &indexLock{}, nil
	}
	return &indexLock{f: f}, nil
}

// unlock releases the lock. Safe on a nil or no-op lock.
func (l *indexLock) unlock() {
	if l == nil || l.f == nil {
		return
	}
	_ = unlockFile(l.f)
	_ = l.f.Close()
	l.f = nil
}

// acquireIndexLock takes the index lock for one Build or Update run and
// gives the run a fresh owner token for its marks. With wait false it
// returns ErrIndexBusy at once when the lock is held; with wait true it
// retries until ctx ends or buildLockWait passes. An in-memory store (no
// path) needs no lock.
func (idx *Indexer) acquireIndexLock(ctx context.Context, wait bool) (*indexLock, error) {
	idx.token = newOwnerToken()
	if idx.store.path == "" {
		return &indexLock{}, nil
	}
	path := lockPath(idx.store.path)
	deadline := time.Now().Add(buildLockWait)
	for {
		l, err := tryLockIndex(path)
		if !errors.Is(err, ErrIndexBusy) || !wait {
			return l, err
		}
		if !time.Now().Before(deadline) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(lockPoll):
		}
	}
}

// newOwnerToken returns a random token that identifies one Build or Update
// run in the marks it sets, so it clears only its own (#392).
func newOwnerToken() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("pid%d-%d", os.Getpid(), time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
