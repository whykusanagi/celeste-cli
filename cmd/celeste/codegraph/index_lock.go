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

// ErrIndexInUse is returned by RemoveIndex when the database file could
// not be deleted although no indexer holds the index lock: on Windows,
// SQLite opens the file without FILE_SHARE_DELETE, so a TUI or MCP server
// that merely has the index open blocks the delete. Nothing is deleted
// then; a rebuild resets the graph in place instead.
var ErrIndexInUse = errors.New("the code graph index files are in use by another process")

// removeFile deletes one index file; a var so tests can make it fail.
var removeFile = os.Remove

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

// testHookReresolveParse, when set, runs before a recovering update parses
// a file again for its raw edges.
var testHookReresolveParse func(path string)

// testHookReresolveResolve, when set, runs before a recovering update
// resolves each stride of the non-Go raw edges it collected.
var testHookReresolveResolve func()

// testHookReplaceNonGoAfterDelete, when set, runs inside
// Store.ReplaceNonGoEdges after the delete and before the inserts.
var testHookReplaceNonGoAfterDelete func()

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

// IndexWriterActive reports whether an indexer (in this process or
// another) holds the lock of the index database at dbPath. It never
// creates the lock file, and reports false for an in-memory store ("") or
// where the lock cannot be probed. The probe takes the lock for an
// instant, so an Update starting in that instant skips as busy; its
// caller's next refresh catches up.
func IndexWriterActive(dbPath string) bool {
	if dbPath == "" {
		return false
	}
	f, err := os.OpenFile(lockPath(dbPath), os.O_RDWR, 0)
	if err != nil {
		return false
	}
	defer f.Close()
	switch err := lockFile(f); {
	case err == nil:
		_ = unlockFile(f)
		return false
	case errors.Is(err, errLocked):
		return true
	default:
		return false
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
	lock := &indexLock{}
	if idx.store.path != "" {
		var err error
		if lock, err = lockIndex(ctx, idx.store.path, wait); err != nil {
			return nil, err
		}
	}
	idx.token = newOwnerToken()
	return lock, nil
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
// memory files, for a reset. It takes the index lock first, waiting as an
// explicit Build does, so it never deletes a database another indexer is
// writing; it returns ErrIndexBusy when the wait runs out, and
// ErrIndexInUse when another process has the database open on Windows.
// Missing files are not an error. The caller closes its own Indexer on the
// database first. A rebuild uses Rebuild, which keeps the lock until the
// new index is built.
func RemoveIndex(ctx context.Context, dbPath string) error {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return fmt.Errorf("index directory: %w", err)
	}
	lock, err := lockIndex(ctx, dbPath, true)
	if err != nil {
		return err
	}
	defer lock.unlock()
	return removeIndexFiles(dbPath)
}

// removeIndexFiles deletes the database files at dbPath; the caller holds
// the index lock. The main file goes first: when it cannot be deleted
// (another process has it open on Windows) it returns ErrIndexInUse and
// leaves the WAL and shared memory files, which belong to that open
// database. On unix the delete succeeds even then, and a process that
// still has the old database open keeps reading and writing the unlinked
// file until it reopens the index; that was so before the index lock too.
func removeIndexFiles(dbPath string) error {
	if err := removeFile(dbPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: %v", ErrIndexInUse, err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := removeFile(dbPath + suffix); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove index: %w", err)
		}
	}
	return nil
}

// Rebuild deletes the index database at dbPath and builds a new one for
// workspace, holding the index lock from before the delete until the
// build has finished, so no other indexer can start on the half-removed
// database in between. Taking the lock waits up to two minutes for
// another indexer, as an explicit Build does (ErrIndexBusy after that).
// removed is false when the old files were in use (ErrIndexInUse, on
// Windows) and the build reset the graph in place instead. The returned
// Indexer is open and the caller closes it; the caller closes its own
// Indexer on the database first.
func Rebuild(ctx context.Context, workspace, dbPath string) (idx *Indexer, removed bool, err error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, false, fmt.Errorf("index directory: %w", err)
	}
	lock, err := lockIndex(ctx, dbPath, true)
	if err != nil {
		return nil, false, err
	}
	defer lock.unlock()
	removed = true
	if err := removeIndexFiles(dbPath); errors.Is(err, ErrIndexInUse) {
		removed = false
	} else if err != nil {
		return nil, false, err
	}
	idx, err = NewIndexer(workspace, dbPath)
	if err != nil {
		return nil, removed, err
	}
	idx.buildMu.Lock()
	defer idx.buildMu.Unlock()
	idx.token = newOwnerToken()
	if err := idx.buildLocked(ctx); err != nil {
		_ = idx.Close()
		return nil, removed, err
	}
	return idx, removed, nil
}
