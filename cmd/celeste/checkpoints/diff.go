package checkpoints

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FileChange represents the diff stats for a single file.
type FileChange struct {
	Path       string
	Insertions int
	Deletions  int
	IsNew      bool
	// Deleted: the file no longer exists.
	Deleted bool
}

// ComputeDiff compares each snapshot's backup against the current file
// to compute line-level diff stats.
func (sm *SnapshotManager) ComputeDiff() ([]FileChange, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.reloadLocked()
	return sm.computeDiffLocked()
}

// computeDiffLocked compares each changed file's oldest backup (its state
// before the session changed it) with the file now. Sorted by path. The
// caller holds sm.mu.
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
		e := earliest[path]
		change := FileChange{Path: path}

		var origLines []string
		if e.Backup == "" {
			change.IsNew = true
		} else {
			src, err := sm.backupPath(e)
			if err != nil {
				return nil, err
			}
			data, err := os.ReadFile(src)
			if err != nil {
				return nil, err
			}
			origLines = strings.Split(string(data), "\n")
		}

		currentData, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				change.Deleted = true
				change.Deletions = len(origLines)
				changes = append(changes, change)
				continue
			}
			return nil, err
		}
		ins, del := diffStats(origLines, strings.Split(string(currentData), "\n"))
		change.Insertions = ins
		change.Deletions = del
		changes = append(changes, change)
	}
	return changes, nil
}

// diffStats computes insertion and deletion counts between two sets of lines
// using a simple LCS-based approach.
func diffStats(oldLines, newLines []string) (insertions, deletions int) {
	// Build LCS length table
	m, n := len(oldLines), len(newLines)

	// For large files, fall back to simple line count comparison
	// to avoid excessive memory usage
	if m*n > 10_000_000 {
		if n > m {
			return n - m, 0
		}
		return 0, m - n
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
		lines = append(lines, fmt.Sprintf("  %s  +%d -%d%s", name, c.Insertions, c.Deletions, suffix))
	}
	return strings.Join(lines, "\n")
}

// DisplayPath shows path relative to workspace when it is inside it, and
// as it is otherwise.
func DisplayPath(workspace, path string) string {
	if rel, err := filepath.Rel(workspace, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return rel
	}
	return path
}
