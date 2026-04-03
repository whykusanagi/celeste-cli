package checkpoints

import (
	"os"
	"strings"
)

// FileChange represents the diff stats for a single file.
type FileChange struct {
	Path       string
	Insertions int
	Deletions  int
	IsNew      bool
}

// ComputeDiff compares each snapshot's backup against the current file
// to compute line-level diff stats.
func (sm *SnapshotManager) ComputeDiff() ([]FileChange, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.computeDiffLocked()
}

// computeDiffLocked does the actual diff computation (must hold sm.mu).
func (sm *SnapshotManager) computeDiffLocked() ([]FileChange, error) {
	// Collect the earliest snapshot per file (the original state)
	earliest := make(map[string]FileSnapshot)
	for _, snap := range sm.snapshots {
		if _, exists := earliest[snap.OriginalPath]; !exists {
			earliest[snap.OriginalPath] = snap
		}
	}

	changes := make([]FileChange, 0, len(earliest))
	for path, snap := range earliest {
		change := FileChange{Path: path}

		// Get original content
		var origLines []string
		if snap.BackupPath == "" {
			// File was new
			change.IsNew = true
		} else {
			data, err := os.ReadFile(snap.BackupPath)
			if err != nil {
				return nil, err
			}
			origLines = strings.Split(string(data), "\n")
		}

		// Get current content
		var currentLines []string
		currentData, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				// File was deleted — all original lines are deletions
				change.Deletions = len(origLines)
				changes = append(changes, change)
				continue
			}
			return nil, err
		}
		currentLines = strings.Split(string(currentData), "\n")

		// Compute simple line diff stats using LCS
		ins, del := diffStats(origLines, currentLines)
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
