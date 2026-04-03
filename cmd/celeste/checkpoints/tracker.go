package checkpoints

import (
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
	ft.mu.RLock()
	recorded, tracked := ft.readTimes[path]
	ft.mu.RUnlock()

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

// ClearStale removes tracking for the given path (e.g. after a successful re-read).
func (ft *FileTracker) ClearStale(path string) {
	ft.mu.Lock()
	delete(ft.readTimes, path)
	ft.mu.Unlock()
}
