package checkpoints

import (
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
			data, err := os.ReadFile(filepath.Join(sm.dir, e.Backup))
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
