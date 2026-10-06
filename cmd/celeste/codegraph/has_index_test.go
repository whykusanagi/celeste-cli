package codegraph

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func indexState(t *testing.T, idx *Indexer) IndexState {
	t.Helper()
	st, err := idx.State()
	require.NoError(t, err)
	return st
}

// #399: State tells a never-built index from a built one, including a
// built index of a workspace with no source files.
func TestIndexState_MissingThenBuilt(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]string
	}{
		{"with source", map[string]string{"main.go": "package main\n\nfunc main() {}\n"}},
		{"empty workspace", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, body := range tc.files {
				require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644))
			}
			idx, err := NewIndexer(dir, filepath.Join(t.TempDir(), "cg.db"))
			require.NoError(t, err)
			defer idx.Close()

			assert.Equal(t, IndexMissing, indexState(t, idx), "a freshly opened index has not been built")
			require.NoError(t, idx.Build())
			assert.Equal(t, IndexBuilt, indexState(t, idx), "a built index is an index")
		})
	}
}

// #399: a rebuild that was killed after it emptied the graph leaves the
// graph version of the earlier build behind. That index is not built: it
// is interrupted while no indexer holds the lock, and being built while
// one does.
func TestIndexState_InterruptedAndBuilding(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644))
	dbPath := filepath.Join(t.TempDir(), "cg.db")
	idx, err := NewIndexer(dir, dbPath)
	require.NoError(t, err)
	defer idx.Close()
	require.NoError(t, idx.Build())

	// What a rebuild killed after ResetGraph leaves behind.
	require.NoError(t, idx.store.SetMeta(metaBuildInProgress, []byte("dead-run")))
	require.NoError(t, idx.store.ResetGraph())
	assert.Equal(t, IndexInterrupted, indexState(t, idx))
	assert.False(t, IndexWriterActive(dbPath))

	lock, err := lockIndex(context.Background(), dbPath, false)
	require.NoError(t, err)
	assert.True(t, IndexWriterActive(dbPath))
	assert.Equal(t, IndexBuilding, indexState(t, idx), "a live indexer holds the lock")
	lock.unlock()

	// Update finishes the interrupted build.
	require.NoError(t, idx.Update())
	assert.Equal(t, IndexBuilt, indexState(t, idx))
}

// A never-built index whose lock is held is being built (a rebuild that
// has not yet written its first mark), not missing.
func TestIndexState_FirstBuildInProgress(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cg.db")
	idx, err := NewIndexer(t.TempDir(), dbPath)
	require.NoError(t, err)
	defer idx.Close()
	lock, err := lockIndex(context.Background(), dbPath, false)
	require.NoError(t, err)
	defer lock.unlock()
	assert.Equal(t, IndexBuilding, indexState(t, idx))
}

// IndexWriterActive never creates the lock file.
func TestIndexWriterActive_NoLockFile(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cg.db")
	assert.False(t, IndexWriterActive(dbPath))
	_, err := os.Stat(lockPath(dbPath))
	assert.True(t, os.IsNotExist(err), "probing must not create the lock file: %v", err)
}
