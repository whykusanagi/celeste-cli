package checkpoints

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewFileTracker(t *testing.T) {
	ft := NewFileTracker()
	assert.NotNil(t, ft)
	assert.NotNil(t, ft.readTimes)
}

func TestRecordRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	require.NoError(t, os.WriteFile(path, []byte("hello"), 0644))

	ft := NewFileTracker()
	err := ft.RecordRead(path)
	require.NoError(t, err)

	ft.mu.RLock()
	_, tracked := ft.readTimes[path]
	ft.mu.RUnlock()
	assert.True(t, tracked)
}

func TestRecordRead_NonexistentFile(t *testing.T) {
	ft := NewFileTracker()
	err := ft.RecordRead("/nonexistent/file.txt")
	assert.Error(t, err)
}

func TestCheckStale_NotTracked(t *testing.T) {
	ft := NewFileTracker()
	err := ft.CheckStale("/some/random/path")
	assert.NoError(t, err, "untracked files should not be considered stale")
}

func TestCheckStale_NotModified(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	require.NoError(t, os.WriteFile(path, []byte("hello"), 0644))

	ft := NewFileTracker()
	require.NoError(t, ft.RecordRead(path))

	err := ft.CheckStale(path)
	assert.NoError(t, err)
}

func TestCheckStale_Modified(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	require.NoError(t, os.WriteFile(path, []byte("hello"), 0644))

	ft := NewFileTracker()
	require.NoError(t, ft.RecordRead(path))

	// Wait to ensure different mtime, then modify
	time.Sleep(50 * time.Millisecond)
	require.NoError(t, os.WriteFile(path, []byte("modified"), 0644))

	err := ft.CheckStale(path)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "modified externally")
}

func TestCheckStale_Deleted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	require.NoError(t, os.WriteFile(path, []byte("hello"), 0644))

	ft := NewFileTracker()
	require.NoError(t, ft.RecordRead(path))

	require.NoError(t, os.Remove(path))

	err := ft.CheckStale(path)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "deleted")
}

func TestClearStale(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	require.NoError(t, os.WriteFile(path, []byte("hello"), 0644))

	ft := NewFileTracker()
	require.NoError(t, ft.RecordRead(path))
	ft.ClearStale(path)

	ft.mu.RLock()
	_, tracked := ft.readTimes[path]
	ft.mu.RUnlock()
	assert.False(t, tracked)
}
