package checkpoints

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/pathutil"
)

// FileChange represents the diff stats for a single file.
type FileChange struct {
	Path       string
	Insertions int
	Deletions  int
	IsNew      bool
	// Deleted: the file no longer exists.
	Deleted bool
	// Binary: either side holds a NUL byte; no line counts.
	Binary bool
	// Approx: the line comparison was too large; the counts are the
	// difference in line count only.
	Approx bool
	// TooBig: a side is over maxDiffBytes and was not read; OldSize and
	// NewSize are the byte sizes.
	TooBig           bool
	OldSize, NewSize int64
	// Err: this file could not be compared (the others still are).
	Err string
}

// maxDiffBytes bounds what /diff reads per side of a file.
const maxDiffBytes = 4 << 20

// ComputeDiff compares each changed file's oldest backup (its state before
// the session changed it) with the file now. Sorted by path. A file that
// cannot be compared carries its error in FileChange.Err. The session lock
// is held only while the index and the backups are read, so an undo in
// another process cannot remove a backup between the two; the current
// files are read and diffed after it is released, so a large /diff never
// keeps other processes waiting.
func (sm *SnapshotManager) ComputeDiff() ([]FileChange, error) {
	olds, err := sm.readOldSides()
	if err != nil {
		return nil, err
	}
	beforeDiffing()
	return diffOldSides(olds), nil
}

// beforeDiffing runs between the locked and the unlocked half of
// ComputeDiff (a test seam).
var beforeDiffing = func() {}

// oldSide is a changed file's state before the session: its entry, and the
// backup's bytes (unless the file is new, the backup is over maxDiffBytes,
// or it could not be read: err).
type oldSide struct {
	e    Entry
	size int64
	data []byte
	big  bool
	err  error
}

// readOldSides reloads the index and reads each file's oldest backup under
// sm.mu and the session lock.
func (sm *SnapshotManager) readOldSides() ([]oldSide, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.dir != "" {
		unlock, err := lockSession(sm.dir, false)
		if err != nil {
			return nil, err
		}
		defer unlock()
	}
	sm.reloadLocked()
	return sm.oldSidesLocked(), nil
}

// oldSidesLocked reads the oldest backup of each changed file, sorted by
// path; the caller holds sm.mu (and the session lock, for ComputeDiff).
func (sm *SnapshotManager) oldSidesLocked() []oldSide {
	earliest := make(map[string]Entry)
	var paths []string
	for _, e := range sm.entries {
		if _, seen := earliest[e.Path]; !seen {
			earliest[e.Path] = e
			paths = append(paths, e.Path)
		}
	}
	sort.Strings(paths)
	olds := make([]oldSide, 0, len(paths))
	for _, path := range paths {
		o := oldSide{e: earliest[path]}
		if o.e.Backup != "" {
			o.size, o.data, o.big, o.err = sm.readBackup(o.e)
		}
		olds = append(olds, o)
	}
	return olds
}

// readBackup reads e's backup, unless it is over maxDiffBytes (big).
func (sm *SnapshotManager) readBackup(e Entry) (size int64, data []byte, big bool, err error) {
	p, err := sm.backupPath(e)
	if err != nil {
		return 0, nil, false, err
	}
	info, err := os.Stat(p)
	if err != nil {
		return 0, nil, false, err
	}
	if info.Size() > maxDiffBytes {
		return info.Size(), nil, true, nil
	}
	data, err = os.ReadFile(p)
	return info.Size(), data, false, err
}

// diffOldSides compares each old side with its file now.
func diffOldSides(olds []oldSide) []FileChange {
	changes := make([]FileChange, 0, len(olds))
	for _, o := range olds {
		change := FileChange{Path: o.e.Path}
		if err := compare(o, &change); err != nil {
			change.Err = err.Error()
		}
		changes = append(changes, change)
	}
	return changes
}

// compare fills c for old side o's file.
func compare(o oldSide, c *FileChange) error {
	if o.err != nil {
		return o.err
	}
	hasOld := o.e.Backup != ""
	if !hasOld {
		c.IsNew = true
	}
	c.OldSize = o.size
	info, err := os.Stat(c.Path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		c.Deleted = true
	case err != nil:
		return err
	default:
		c.NewSize = info.Size()
	}
	if o.big || c.NewSize > maxDiffBytes {
		c.TooBig = true
		return nil
	}
	old := o.data
	var cur []byte
	if !c.Deleted {
		if cur, err = os.ReadFile(c.Path); err != nil {
			return err
		}
	}
	if bytes.IndexByte(old, 0) >= 0 || bytes.IndexByte(cur, 0) >= 0 {
		c.Binary = true
		return nil
	}
	var oldLines, newLines []string
	if hasOld {
		oldLines = strings.Split(string(old), "\n")
	}
	if c.Deleted {
		c.Deletions = len(oldLines)
		return nil
	}
	newLines = strings.Split(string(cur), "\n")
	c.Insertions, c.Deletions, c.Approx = diffStats(oldLines, newLines)
	return nil
}

// diffStats computes insertion and deletion counts between two sets of lines
// using a simple LCS-based approach.
func diffStats(oldLines, newLines []string) (insertions, deletions int, approx bool) {
	// Build LCS length table
	m, n := len(oldLines), len(newLines)

	// For large files, fall back to simple line count comparison
	// to avoid excessive memory usage
	if m*n > 10_000_000 {
		if n > m {
			return n - m, 0, true
		}
		return 0, m - n, true
	}

	// Standard LCS dynamic programming
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if oldLines[i-1] == newLines[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}

	lcsLen := dp[m][n]
	deletions = m - lcsLen
	insertions = n - lcsLen
	return
}

// FormatChanges renders ComputeDiff's result for /diff: one line per file,
// relative to workspace when inside it, in the order given (ComputeDiff
// sorts by path).
func FormatChanges(changes []FileChange, workspace string) string {
	if len(changes) == 0 {
		return "No files changed in this session."
	}
	lines := []string{"Files changed this session:"}
	for _, c := range changes {
		name := DisplayPath(workspace, c.Path)
		suffix := ""
		switch {
		case c.Deleted:
			suffix = " (deleted)"
		case c.IsNew:
			suffix = " (new)"
		}
		switch {
		case c.Err != "":
			lines = append(lines, fmt.Sprintf("  %s  (error: %s)", name, c.Err))
		case c.TooBig:
			lines = append(lines, fmt.Sprintf("  %s  (large file: %d -> %d bytes)%s", name, c.OldSize, c.NewSize, suffix))
		case c.Binary:
			lines = append(lines, fmt.Sprintf("  %s  (binary)%s", name, suffix))
		case c.Approx:
			lines = append(lines, fmt.Sprintf("  %s  +%d -%d (large file, line counts only)%s", name, c.Insertions, c.Deletions, suffix))
		default:
			lines = append(lines, fmt.Sprintf("  %s  +%d -%d%s", name, c.Insertions, c.Deletions, suffix))
		}
	}
	return strings.Join(lines, "\n")
}

// DisplayPath shows path relative to workspace when it is inside it, and
// as it is otherwise.
func DisplayPath(workspace, path string) string {
	if pathutil.Within(workspace, path) {
		if rel, err := filepath.Rel(workspace, path); err == nil {
			return rel
		}
	}
	return path
}
