package checkpoints

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Aikido 806869815: undoing a change never writes or deletes outside the
// workspace, also after a directory on the way was replaced by a symlink
// to one outside holding a file with the same contents (so the "changed
// since" check passes).
func TestUndoDoesNotFollowAReplacedDirectory(t *testing.T) {
	for _, existed := range []bool{true, false} {
		sm, dir := store(t)
		ws := filepath.Join(dir, "ws")
		require.NoError(t, os.MkdirAll(filepath.Join(ws, "sub"), 0o755))
		f := filepath.Join(ws, "sub", "f.txt")
		if existed {
			write(t, f, "PAYLOAD")
		}
		c, err := sm.CheckpointIn(ws, f, "call-1")
		require.NoError(t, err)
		write(t, f, "after")
		require.NoError(t, c.Commit())

		outside := filepath.Join(dir, "outside")
		require.NoError(t, os.MkdirAll(outside, 0o755))
		write(t, filepath.Join(outside, "f.txt"), "after")
		require.NoError(t, os.Rename(filepath.Join(ws, "sub"), filepath.Join(ws, "moved")))
		if err := os.Symlink(outside, filepath.Join(ws, "sub")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}

		_, err = sm.RevertLastIf(func(e Entry) error {
			_, changed, err := Changed(e)
			if err != nil {
				return err
			}
			if changed {
				return os.ErrExist
			}
			return nil
		})
		assert.Error(t, err, "existed=%v", existed)
		got, rerr := os.ReadFile(filepath.Join(outside, "f.txt"))
		require.NoError(t, rerr, "existed=%v: the file outside was removed", existed)
		assert.Equal(t, "after", string(got), "existed=%v: the file outside was overwritten", existed)
	}
}

// A change made through an in-workspace symlink is undone on the file it
// changed; the symlink stays a symlink.
func TestUndoThroughAnInWorkspaceSymlinkKeepsTheLink(t *testing.T) {
	sm, dir := store(t)
	ws := filepath.Join(dir, "ws")
	require.NoError(t, os.MkdirAll(ws, 0o755))
	target := filepath.Join(ws, "AGENTS.md")
	link := filepath.Join(ws, "CLAUDE.md")
	write(t, target, "before")
	if err := os.Symlink("AGENTS.md", link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	c, err := sm.CheckpointIn(ws, link, "call-1")
	require.NoError(t, err)
	write(t, target, "after")
	require.NoError(t, c.Commit())

	_, err = sm.RevertLast()
	require.NoError(t, err)
	fi, err := os.Lstat(link)
	require.NoError(t, err)
	assert.NotZero(t, fi.Mode()&os.ModeSymlink, "the symlink was replaced by a file")
	assert.Equal(t, "before", read(t, target))
}
