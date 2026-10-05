package codegraph

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// ErrIndexBusy is returned by Update, and by Build after waiting
// buildLockWait, when another indexer (in this process or another one)
// holds the index's write lock (#392).
var ErrIndexBusy = errors.New("the code graph index is being written by another indexer")

// errLocked is what lockFile returns when another holder has the lock;
// errLockUnsupported is what it returns when the file system cannot lock
// files at all (some network file systems).
var (
	errLocked          = errors.New("file is locked")
	errLockUnsupported = errors.New("file locking is not supported here")
)

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
// files at all gets a lock that excludes nothing rather than no index; any
// other locking error is returned, so writers never run unexcluded by
// accident.
func tryLockIndex(path string) (*indexLock, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open index lock: %w", err)
	}
	switch err := lockFile(f); {
	case err == nil:
		return &indexLock{f: f}, nil
	case errors.Is(err, errLocked):
		_ = f.Close()
		return nil, ErrIndexBusy
	case errors.Is(err, errLockUnsupported):
		_ = f.Close()
		return &indexLock{}, nil
	default:
		_ = f.Close()
		return nil, fmt.Errorf("lock index: %w", err)
	}
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
	return lockIndex(ctx, idx.store.path, wait)
}

// lockIndex takes the lock of the index database at dbPath, waiting as
// acquireIndexLock describes.
func lockIndex(ctx context.Context, dbPath string, wait bool) (*indexLock, error) {
	path := lockPath(dbPath)
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

// RemoveIndex deletes the index database at dbPath with its WAL and shared
// memory files, for a rebuild or reset. It takes the index lock first,
// waiting as an explicit Build does, so it never deletes a database another
// indexer is writing; it returns ErrIndexBusy when the wait runs out.
// Missing files are not an error. The caller closes its own Indexer on the
// database first.
func RemoveIndex(ctx context.Context, dbPath string) error {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return fmt.Errorf("index directory: %w", err)
	}
	lock, err := lockIndex(ctx, dbPath, true)
	if err != nil {
		return err
	}
	defer lock.unlock()
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Remove(dbPath + suffix); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove index: %w", err)
		}
	}
	return nil
}
