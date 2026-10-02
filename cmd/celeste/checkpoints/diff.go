package checkpoints

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/pathutil"
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
// cannot be compared carries its error in FileChange.Err.
func (sm *SnapshotManager) ComputeDiff() ([]FileChange, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.reloadLocked()
	return sm.computeDiffLocked()
}

// computeDiffLocked is ComputeDiff; the caller holds sm.mu.
func (sm *SnapshotManager) computeDiffLocked() ([]FileChange, error) {
	earliest := make(map[string]Entry)
	var paths []string
	for _, e := range sm.entries {
		if _, seen := earliest[e.Path]; !seen {
			earliest[e.Path] = e
			paths = append(paths, e.Path)
		}
	}
	sort.Strings(paths)

	changes := make([]FileChange, 0, len(paths))
	for _, path := range paths {
		change := FileChange{Path: path}
		if err := sm.compare(earliest[path], &change); err != nil {
			change.Err = err.Error()
		}
		changes = append(changes, change)
	}
	return changes, nil
}

// compare fills c for entry e's file.
func (sm *SnapshotManager) compare(e Entry, c *FileChange) error {
	var src string
	if e.Backup == "" {
		c.IsNew = true
	} else {
		p, err := sm.backupPath(e)
		if err != nil {
			return err
		}
		info, err := os.Stat(p)
		if err != nil {
			return err
		}
		src, c.OldSize = p, info.Size()
	}
	info, err := os.Stat(c.Path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		c.Deleted = true
	case err != nil:
		return err
	default:
		c.NewSize = info.Size()
	}
	if c.OldSize > maxDiffBytes || c.NewSize > maxDiffBytes {
		c.TooBig = true
		return nil
	}
	var old, cur []byte
	if src != "" {
		if old, err = os.ReadFile(src); err != nil {
			return err
		}
	}
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
	if src != "" {
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
