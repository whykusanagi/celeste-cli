package builtin

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

// A workspace swapped for a symlink to another directory after the bash
// tool was built is refused, not followed (Aikido review of #433).
func TestBashRefusesASwappedWorkspace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	root := t.TempDir()
	ws := filepath.Join(root, "ws")
	outside := filepath.Join(root, "outside")
	require.NoError(t, os.Mkdir(ws, 0o755))
	require.NoError(t, os.Mkdir(outside, 0o755))
	tool := NewBashTool(ws, nil)

	res, err := tool.Execute(context.Background(), map[string]any{"command": "echo ok"}, nil)
	require.NoError(t, err)
	require.False(t, res.Error, res.Content)

	require.NoError(t, os.Rename(ws, ws+".old"))
	require.NoError(t, os.Symlink(outside, ws))
	res, err = tool.Execute(context.Background(), map[string]any{"command": "touch escaped"}, nil)
	require.NoError(t, err)
	require.True(t, res.Error)
	require.Contains(t, res.Content, "workspace directory changed")
	_, statErr := os.Stat(filepath.Join(outside, "escaped"))
	require.True(t, os.IsNotExist(statErr), "the command must not run in the swapped directory")
}

// A workspace that could not be read when the tool was built has no pin,
// and bash refuses rather than running unchecked.
func TestBashRefusesWithoutAPin(t *testing.T) {
	ws := filepath.Join(t.TempDir(), "later")
	tool := NewBashTool(ws, nil)
	require.NoError(t, os.Mkdir(ws, 0o755))
	res, err := tool.Execute(context.Background(), map[string]any{"command": "echo ok"}, nil)
	require.NoError(t, err)
	require.True(t, res.Error)
	require.Contains(t, res.Content, "workspace directory changed")
}
