package checkpoints

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeDiff_Insertions(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")
	sm := newSnapshotManagerWithBase(backupDir)

	srcFile := filepath.Join(dir, "source.txt")
	require.NoError(t, os.WriteFile(srcFile, []byte("line1\nline2"), 0644))

	require.NoError(t, sm.Snapshot(srcFile))
	require.NoError(t, os.WriteFile(srcFile, []byte("line1\nline2\nline3\nline4"), 0644))

	changes, err := sm.ComputeDiff()
	require.NoError(t, err)
	assert.Len(t, changes, 1)
	assert.Equal(t, 2, changes[0].Insertions)
	assert.Equal(t, 0, changes[0].Deletions)
	assert.False(t, changes[0].IsNew)
}

func TestComputeDiff_Deletions(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")
	sm := newSnapshotManagerWithBase(backupDir)

	srcFile := filepath.Join(dir, "source.txt")
	require.NoError(t, os.WriteFile(srcFile, []byte("line1\nline2\nline3"), 0644))

	require.NoError(t, sm.Snapshot(srcFile))
	require.NoError(t, os.WriteFile(srcFile, []byte("line1"), 0644))

	changes, err := sm.ComputeDiff()
	require.NoError(t, err)
	assert.Len(t, changes, 1)
	assert.Equal(t, 0, changes[0].Insertions)
	assert.Equal(t, 2, changes[0].Deletions)
}

func TestComputeDiff_NewFile(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")
	sm := newSnapshotManagerWithBase(backupDir)

	srcFile := filepath.Join(dir, "new.txt")
	require.NoError(t, sm.Snapshot(srcFile))

	// Now create the file
	require.NoError(t, os.WriteFile(srcFile, []byte("line1\nline2\nline3"), 0644))

	changes, err := sm.ComputeDiff()
	require.NoError(t, err)
	assert.Len(t, changes, 1)
	assert.True(t, changes[0].IsNew)
	assert.Equal(t, 3, changes[0].Insertions)
	assert.Equal(t, 0, changes[0].Deletions)
}

func TestComputeDiff_DeletedFile(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")
	sm := newSnapshotManagerWithBase(backupDir)

	srcFile := filepath.Join(dir, "source.txt")
	require.NoError(t, os.WriteFile(srcFile, []byte("line1\nline2"), 0644))

	require.NoError(t, sm.Snapshot(srcFile))
	require.NoError(t, os.Remove(srcFile))

	changes, err := sm.ComputeDiff()
	require.NoError(t, err)
	assert.Len(t, changes, 1)
	assert.Equal(t, 0, changes[0].Insertions)
	assert.Equal(t, 2, changes[0].Deletions)
}

func TestComputeDiff_MultipleSnapshots_UsesEarliest(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")
	sm := newSnapshotManagerWithBase(backupDir)

	srcFile := filepath.Join(dir, "source.txt")
	require.NoError(t, os.WriteFile(srcFile, []byte("original"), 0644))

	require.NoError(t, sm.Snapshot(srcFile))
	require.NoError(t, os.WriteFile(srcFile, []byte("modified once"), 0644))
	require.NoError(t, sm.Snapshot(srcFile))
	require.NoError(t, os.WriteFile(srcFile, []byte("modified twice"), 0644))

	changes, err := sm.ComputeDiff()
	require.NoError(t, err)
	assert.Len(t, changes, 1)
	// Diff should be against the earliest snapshot ("original"), not the second
}

func TestComputeDiff_Mixed(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")
	sm := newSnapshotManagerWithBase(backupDir)

	srcFile := filepath.Join(dir, "source.txt")
	require.NoError(t, os.WriteFile(srcFile, []byte("line1\nline2\nline3"), 0644))

	require.NoError(t, sm.Snapshot(srcFile))
	// Replace line2 with lineX and add line4
	require.NoError(t, os.WriteFile(srcFile, []byte("line1\nlineX\nline3\nline4"), 0644))

	changes, err := sm.ComputeDiff()
	require.NoError(t, err)
	assert.Len(t, changes, 1)
	// line2 -> lineX is 1 deletion + 1 insertion, plus line4 is 1 insertion
	assert.Equal(t, 2, changes[0].Insertions)
	assert.Equal(t, 1, changes[0].Deletions)
}

func TestDiffStats(t *testing.T) {
	tests := []struct {
		name    string
		old     []string
		new     []string
		wantIns int
		wantDel int
	}{
		{"identical", []string{"a", "b"}, []string{"a", "b"}, 0, 0},
		{"pure insert", []string{"a"}, []string{"a", "b", "c"}, 2, 0},
		{"pure delete", []string{"a", "b", "c"}, []string{"a"}, 0, 2},
		{"replacement", []string{"a", "b"}, []string{"a", "c"}, 1, 1},
		{"empty to content", []string{}, []string{"a", "b"}, 2, 0},
		{"content to empty", []string{"a", "b"}, []string{}, 0, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ins, del := diffStats(tt.old, tt.new)
			assert.Equal(t, tt.wantIns, ins, "insertions")
			assert.Equal(t, tt.wantDel, del, "deletions")
		})
	}
}
