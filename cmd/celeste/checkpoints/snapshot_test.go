package checkpoints

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSnapshotManager(t *testing.T) {
	sm := NewSnapshotManager("test-session")
	assert.NotNil(t, sm)
	assert.Contains(t, sm.baseDir, "test-session")
	assert.Equal(t, 100, sm.maxCount)
}

func TestSnapshot_ExistingFile(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")
	sm := newSnapshotManagerWithBase(backupDir)

	// Create source file
	srcFile := filepath.Join(dir, "source.txt")
	require.NoError(t, os.WriteFile(srcFile, []byte("original content"), 0644))

	err := sm.Snapshot(srcFile)
	require.NoError(t, err)
	assert.Len(t, sm.snapshots, 1)
	assert.Equal(t, srcFile, sm.snapshots[0].OriginalPath)
	assert.NotEmpty(t, sm.snapshots[0].BackupPath)
	assert.Equal(t, 1, sm.snapshots[0].Version)

	// Verify backup content
	data, err := os.ReadFile(sm.snapshots[0].BackupPath)
	require.NoError(t, err)
	assert.Equal(t, "original content", string(data))
}

func TestSnapshot_NewFile(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")
	sm := newSnapshotManagerWithBase(backupDir)

	// Snapshot a file that doesn't exist yet
	srcFile := filepath.Join(dir, "new_file.txt")
	err := sm.Snapshot(srcFile)
	require.NoError(t, err)
	assert.Len(t, sm.snapshots, 1)
	assert.Equal(t, "", sm.snapshots[0].BackupPath, "backup should be empty sentinel for new files")
	assert.True(t, sm.snapshots[0].Version == 1)
}

func TestSnapshot_Versioning(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")
	sm := newSnapshotManagerWithBase(backupDir)

	srcFile := filepath.Join(dir, "source.txt")
	require.NoError(t, os.WriteFile(srcFile, []byte("v1"), 0644))

	require.NoError(t, sm.Snapshot(srcFile))
	require.NoError(t, os.WriteFile(srcFile, []byte("v2"), 0644))
	require.NoError(t, sm.Snapshot(srcFile))

	assert.Len(t, sm.snapshots, 2)
	assert.Equal(t, 1, sm.snapshots[0].Version)
	assert.Equal(t, 2, sm.snapshots[1].Version)
}

func TestRevert_ExistingFile(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")
	sm := newSnapshotManagerWithBase(backupDir)

	srcFile := filepath.Join(dir, "source.txt")
	require.NoError(t, os.WriteFile(srcFile, []byte("original"), 0644))

	require.NoError(t, sm.Snapshot(srcFile))
	require.NoError(t, os.WriteFile(srcFile, []byte("modified"), 0644))

	err := sm.Revert(srcFile)
	require.NoError(t, err)

	data, err := os.ReadFile(srcFile)
	require.NoError(t, err)
	assert.Equal(t, "original", string(data))
	assert.Len(t, sm.snapshots, 0, "snapshot should be removed after revert")
}

func TestRevert_NewFile(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")
	sm := newSnapshotManagerWithBase(backupDir)

	srcFile := filepath.Join(dir, "new_file.txt")
	require.NoError(t, sm.Snapshot(srcFile))

	// Create the file (simulating a write after snapshot)
	require.NoError(t, os.WriteFile(srcFile, []byte("new content"), 0644))

	err := sm.Revert(srcFile)
	require.NoError(t, err)

	_, err = os.Stat(srcFile)
	assert.True(t, os.IsNotExist(err), "file should be deleted after reverting a new-file snapshot")
}

func TestRevert_NoSnapshot(t *testing.T) {
	dir := t.TempDir()
	sm := newSnapshotManagerWithBase(filepath.Join(dir, "backups"))

	err := sm.Revert("/nonexistent/file.txt")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no snapshot found")
}

func TestRevertLast(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")
	sm := newSnapshotManagerWithBase(backupDir)

	file1 := filepath.Join(dir, "file1.txt")
	file2 := filepath.Join(dir, "file2.txt")
	require.NoError(t, os.WriteFile(file1, []byte("f1-original"), 0644))
	require.NoError(t, os.WriteFile(file2, []byte("f2-original"), 0644))

	require.NoError(t, sm.Snapshot(file1))
	require.NoError(t, sm.Snapshot(file2))

	require.NoError(t, os.WriteFile(file1, []byte("f1-modified"), 0644))
	require.NoError(t, os.WriteFile(file2, []byte("f2-modified"), 0644))

	// RevertLast should revert file2 (the last snapshot)
	path, err := sm.RevertLast()
	require.NoError(t, err)
	assert.Equal(t, file2, path)

	data, err := os.ReadFile(file2)
	require.NoError(t, err)
	assert.Equal(t, "f2-original", string(data))

	// file1 should still be modified
	data, err = os.ReadFile(file1)
	require.NoError(t, err)
	assert.Equal(t, "f1-modified", string(data))
}

func TestRevertLast_Empty(t *testing.T) {
	dir := t.TempDir()
	sm := newSnapshotManagerWithBase(filepath.Join(dir, "backups"))

	_, err := sm.RevertLast()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no snapshots")
}

func TestGetChanges(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")
	sm := newSnapshotManagerWithBase(backupDir)

	srcFile := filepath.Join(dir, "source.txt")
	require.NoError(t, os.WriteFile(srcFile, []byte("line1\nline2\nline3"), 0644))

	require.NoError(t, sm.Snapshot(srcFile))
	require.NoError(t, os.WriteFile(srcFile, []byte("line1\nline2\nline3\nline4"), 0644))

	changes := sm.GetChanges()
	assert.Len(t, changes, 1)
	assert.Equal(t, srcFile, changes[0].Path)
	assert.Equal(t, 1, changes[0].Insertions)
	assert.Equal(t, 0, changes[0].Deletions)
}

func TestCleanup(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")
	sm := newSnapshotManagerWithBase(backupDir)

	srcFile := filepath.Join(dir, "source.txt")
	require.NoError(t, os.WriteFile(srcFile, []byte("content"), 0644))
	require.NoError(t, sm.Snapshot(srcFile))

	// Verify backup dir exists
	_, err := os.Stat(backupDir)
	require.NoError(t, err)

	require.NoError(t, sm.Cleanup())

	_, err = os.Stat(backupDir)
	assert.True(t, os.IsNotExist(err))
	assert.Nil(t, sm.snapshots)
}

func TestSnapshot_MaxCountEnforced(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")
	sm := newSnapshotManagerWithBase(backupDir)
	sm.maxCount = 3

	srcFile := filepath.Join(dir, "source.txt")
	require.NoError(t, os.WriteFile(srcFile, []byte("content"), 0644))

	for i := 0; i < 3; i++ {
		require.NoError(t, sm.Snapshot(srcFile))
	}

	err := sm.Snapshot(srcFile)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "snapshot limit reached")
}
