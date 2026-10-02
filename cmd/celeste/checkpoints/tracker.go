package checkpoints

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
)

// FileTracker tracks file modification times to detect stale reads.
// When a file is read, its mtime is recorded. Before writing, the caller
// can check whether the file was modified externally since the last read.
type FileTracker struct {
	readTimes map[string]time.Time // path -> mtime at last read
	mu        sync.RWMutex
}

// NewFileTracker creates a new FileTracker.
func NewFileTracker() *FileTracker {
	return &FileTracker{
		readTimes: make(map[string]time.Time),
	}
}

// RecordRead stats the file at path and stores its current mtime.
func (ft *FileTracker) RecordRead(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("cannot stat file for tracking: %w", err)
	}
	ft.mu.Lock()
	ft.readTimes[path] = info.ModTime()
	ft.mu.Unlock()
	return nil
}

// CheckStale compares the file's current mtime against the stored mtime.
// Returns an error if the file was modified externally since the last read.
// Returns nil if the file has never been tracked (first write is allowed).
func (ft *FileTracker) CheckStale(path string) error {
	recorded, tracked := ft.lookup(path)

	if !tracked {
		return nil // never read — allow write
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("file was deleted since you last read it — read it again before editing")
		}
		return fmt.Errorf("cannot stat file for stale check: %w", err)
	}

	if !info.ModTime().Equal(recorded) {
		return fmt.Errorf("file was modified externally since you last read it — read it again before editing")
	}
	return nil
}

// ErrNotRead: an existing file was never read in this session.
var ErrNotRead = errors.New("not read in this session")

// CheckRead is the must-read-before-edit rule (2.0 W4 ruling 6): nil for a
// file that does not exist, or that was read (or written) in this session
// and is unchanged since; ErrNotRead for an existing file never read; the
// CheckStale error for one changed since its read; the stat error for a file
// that cannot be checked (reading it first would not help).
func (ft *FileTracker) CheckRead(path string) error {
	_, tracked := ft.lookup(path)
	if !tracked {
		_, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("cannot check whether the file was read: %w", err)
		}
		return ErrNotRead
	}
	return ft.CheckStale(path)
}

// lookup finds the recorded read of path, or of another name of the same
// file (a symlink to it, a different case on a case-insensitive
// filesystem): reads count per file, not per spelling.
func (ft *FileTracker) lookup(path string) (time.Time, bool) {
	ft.mu.RLock()
	defer ft.mu.RUnlock()
	if t, ok := ft.readTimes[path]; ok {
		return t, true
	}
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}, false
	}
	for p, t := range ft.readTimes {
		if other, err := os.Stat(p); err == nil && os.SameFile(info, other) {
			return t, true
		}
	}
	return time.Time{}, false
}

// ClearStale removes tracking for the given path (e.g. after a successful re-read).
func (ft *FileTracker) ClearStale(path string) {
	ft.mu.Lock()
	delete(ft.readTimes, path)
	ft.mu.Unlock()
}

// Reset forgets every recorded read, so the next write to any file is
// allowed as a first write. MCP chat calls it when a shared environment
// starts a call with no other call in flight: staleness is judged per call.
func (ft *FileTracker) Reset() {
	ft.mu.Lock()
	ft.readTimes = make(map[string]time.Time)
	ft.mu.Unlock()
}
