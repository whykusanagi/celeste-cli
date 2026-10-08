package codegraph

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestIndexer_CloseWaitsForBuild: Close must not tear down the parsers and
// the store under a running Build or Update (Aikido 806869451). It waits for
// buildMu, which every build and update holds.
func TestIndexer_CloseWaitsForBuild(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, ".celeste", "codegraph.db")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	idx, err := NewIndexer(dir, dbPath)
	require.NoError(t, err)

	idx.buildMu.Lock() // a build in progress
	closed := make(chan error, 1)
	go func() { closed <- idx.Close() }()
	select {
	case <-closed:
		idx.buildMu.Unlock()
		t.Fatal("Close returned while a build held buildMu")
	case <-time.After(200 * time.Millisecond):
	}
	idx.buildMu.Unlock()
	select {
	case err := <-closed:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return after the build finished")
	}
}
