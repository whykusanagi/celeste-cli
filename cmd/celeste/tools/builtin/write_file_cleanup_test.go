package builtin

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A write_file that fails after creating its new file, with checkpoints
// off, must leave neither the file nor the directories it created: the
// cleanup runs through the workspace root while that root is still open.
func TestWriteFileFailedNewFileCleansUpWithoutCheckpoints(t *testing.T) {
	ws, _ := timingSetup(t)
	failWritesAfter(t, 0)

	res := run(t, NewWriteFileTool(ws), context.Background(), map[string]any{"path": "sub/deeper/new.txt", "content": "hello"})
	require.True(t, res.Error, res.Content)

	_, err := os.Lstat(filepath.Join(ws, "sub", "deeper", "new.txt"))
	assert.True(t, os.IsNotExist(err), "the partial new file is removed: %v", err)
	_, err = os.Lstat(filepath.Join(ws, "sub"))
	assert.True(t, os.IsNotExist(err), "the directories the write created are removed: %v", err)
}

// undo removes the created directories through the root, not by path name:
// with the path-name copy of the workspace gone, a root-relative removal
// still finds them.
func TestProtectedWriteGuardUndoRemovesDirsThroughRoot(t *testing.T) {
	ws, _ := timingSetup(t)
	real, err := filepath.EvalSymlinks(ws)
	require.NoError(t, err)
	target := filepath.Join(real, "a", "b", "f.txt")

	g, err := guardProtectedWrite(target)
	require.NoError(t, err)
	root, rel, err := workspaceRoot(real, target)
	require.NoError(t, err)
	defer root.Close()
	g.inRoot(root, rel)
	require.NoError(t, g.mkdirAll(filepath.Dir(target)))

	// Point the recorded path names somewhere else: a path-name removal
	// would miss, a root-relative one still cleans up.
	for i := range g.createdDirs {
		g.createdDirs[i] = filepath.Join(t.TempDir(), "elsewhere", filepath.Base(g.createdDirs[i]))
	}
	g.undo()
	_, err = os.Lstat(filepath.Join(real, "a"))
	assert.True(t, os.IsNotExist(err), "created dirs removed through the root: %v", err)
}
