//go:build unix

package codegraph

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

// A FIFO (or device) with an indexable name is not a source file: handing
// it to a parser would block indexing on the open.
func TestWalkSourceFilesSkipsNonRegularFiles(t *testing.T) {
	ws := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(ws, "a.go"), []byte("package a\n"), 0o644))
	if err := syscall.Mkfifo(filepath.Join(ws, "x.go"), 0o644); err != nil {
		t.Skipf("mkfifo unavailable: %v", err)
	}
	idx, err := NewIndexer(ws, filepath.Join(t.TempDir(), "cg.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = idx.Close() })
	files, err := idx.walkSourceFiles()
	require.NoError(t, err)
	require.Equal(t, []string{"a.go"}, files)
}
