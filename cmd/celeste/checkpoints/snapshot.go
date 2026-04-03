package checkpoints

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// FileSnapshot represents a backup of a file before modification.
type FileSnapshot struct {
	OriginalPath string
	BackupPath   string // empty string if file didn't exist (null sentinel)
	Version      int
	Timestamp    time.Time
}

// SnapshotManager handles file backups for undo/revert support.
type SnapshotManager struct {
	baseDir   string // ~/.celeste/checkpoints/<session-id>/
	snapshots []FileSnapshot
	maxCount  int // maximum number of snapshots to retain
	mu        sync.Mutex
}

// NewSnapshotManager creates a SnapshotManager for the given session.
// Backups are stored under ~/.celeste/checkpoints/<sessionID>/.
func NewSnapshotManager(sessionID string) *SnapshotManager {
	home, _ := os.UserHomeDir()
	baseDir := filepath.Join(home, ".celeste", "checkpoints", sessionID)
	return &SnapshotManager{
		baseDir:   baseDir,
		snapshots: make([]FileSnapshot, 0),
		maxCount:  100,
	}
}

// newSnapshotManagerWithBase is an internal constructor for testing with a custom base directory.
func newSnapshotManagerWithBase(baseDir string) *SnapshotManager {
	return &SnapshotManager{
		baseDir:   baseDir,
		snapshots: make([]FileSnapshot, 0),
		maxCount:  100,
	}
}

// Snapshot creates a backup of the file at filePath before it is modified.
// If the file does not exist, a sentinel snapshot is recorded (BackupPath = "").
// Uses two-phase mtime checking to detect concurrent modifications during backup.
func (sm *SnapshotManager) Snapshot(filePath string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Enforce snapshot limit
	if len(sm.snapshots) >= sm.maxCount {
		return fmt.Errorf("snapshot limit reached (%d) — consider cleaning up", sm.maxCount)
	}

	version := sm.nextVersion(filePath)
	ts := time.Now()

	info, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		// File doesn't exist yet — record sentinel
		sm.snapshots = append(sm.snapshots, FileSnapshot{
			OriginalPath: filePath,
			BackupPath:   "", // sentinel: file was new
			Version:      version,
			Timestamp:    ts,
		})
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot stat file for snapshot: %w", err)
	}

	// Phase 1: record mtime
	mtimeBefore := info.ModTime()

	// Ensure backup directory
	if err := os.MkdirAll(sm.baseDir, 0755); err != nil {
		return fmt.Errorf("cannot create checkpoint directory: %w", err)
	}

	backupName := fmt.Sprintf("%s_v%d", sanitizeFilename(filePath), version)
	backupPath := filepath.Join(sm.baseDir, backupName)

	// Phase 2: copy file
	if err := copyFile(filePath, backupPath); err != nil {
		return fmt.Errorf("snapshot copy failed: %w", err)
	}

	// Phase 3: re-check mtime
	infoAfter, err := os.Stat(filePath)
	if err != nil {
		os.Remove(backupPath)
		return fmt.Errorf("file changed during snapshot: %w", err)
	}
	if !infoAfter.ModTime().Equal(mtimeBefore) {
		// File was modified during copy — redo once
		os.Remove(backupPath)
		if err := copyFile(filePath, backupPath); err != nil {
			return fmt.Errorf("snapshot retry copy failed: %w", err)
		}
	}

	sm.snapshots = append(sm.snapshots, FileSnapshot{
		OriginalPath: filePath,
		BackupPath:   backupPath,
		Version:      version,
		Timestamp:    ts,
	})
	return nil
}

// Revert restores the most recent backup for the given file path.
func (sm *SnapshotManager) Revert(filePath string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Find the most recent snapshot for this path
	idx := -1
	for i := len(sm.snapshots) - 1; i >= 0; i-- {
		if sm.snapshots[i].OriginalPath == filePath {
			idx = i
			break
		}
	}
	if idx == -1 {
		return fmt.Errorf("no snapshot found for %s", filePath)
	}

	snap := sm.snapshots[idx]

	if snap.BackupPath == "" {
		// File was new — revert means delete it
		if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("cannot remove file during revert: %w", err)
		}
	} else {
		if err := copyFile(snap.BackupPath, filePath); err != nil {
			return fmt.Errorf("revert copy failed: %w", err)
		}
	}

	// Remove this snapshot from the list
	sm.snapshots = append(sm.snapshots[:idx], sm.snapshots[idx+1:]...)
	return nil
}

// RevertLast reverts the most recent snapshot regardless of path.
// Returns the path that was reverted.
func (sm *SnapshotManager) RevertLast() (string, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if len(sm.snapshots) == 0 {
		return "", fmt.Errorf("no snapshots to revert")
	}

	snap := sm.snapshots[len(sm.snapshots)-1]

	if snap.BackupPath == "" {
		if err := os.Remove(snap.OriginalPath); err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("cannot remove file during revert: %w", err)
		}
	} else {
		if err := copyFile(snap.BackupPath, snap.OriginalPath); err != nil {
			return "", fmt.Errorf("revert copy failed: %w", err)
		}
	}

	sm.snapshots = sm.snapshots[:len(sm.snapshots)-1]
	return snap.OriginalPath, nil
}

// GetChanges returns a FileChange summary for each file that has been snapshotted.
func (sm *SnapshotManager) GetChanges() []FileChange {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	changes, _ := sm.computeDiffLocked()
	return changes
}

// Cleanup removes the entire checkpoint directory for this session.
func (sm *SnapshotManager) Cleanup() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.snapshots = nil
	if sm.baseDir != "" {
		return os.RemoveAll(sm.baseDir)
	}
	return nil
}

// nextVersion returns the next version number for a given file path.
func (sm *SnapshotManager) nextVersion(filePath string) int {
	maxV := 0
	for _, s := range sm.snapshots {
		if s.OriginalPath == filePath && s.Version > maxV {
			maxV = s.Version
		}
	}
	return maxV + 1
}

// sanitizeFilename converts a file path into a safe backup filename.
func sanitizeFilename(path string) string {
	// Replace path separators and other special chars with underscores
	name := filepath.Base(path)
	// Prefix with a hash of the full path to avoid collisions
	h := uint32(0)
	for _, c := range path {
		h = h*31 + uint32(c)
	}
	return fmt.Sprintf("%08x_%s", h, name)
}

// copyFile copies src to dst, preserving permissions.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
