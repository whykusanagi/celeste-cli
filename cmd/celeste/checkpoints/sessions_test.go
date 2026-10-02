package checkpoints

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSessionDirIsSafe(t *testing.T) {
	root := filepath.Join("r")
	assert.Equal(t, filepath.Join(root, "20261002-abc_1"), SessionDir(root, "20261002-abc_1"))
	assert.Equal(t, filepath.Join(root, "a_b_c"), SessionDir(root, "a/b\\c"))
	assert.Equal(t, filepath.Join(root, "_.."), SessionDir(root, ".."))
	assert.Equal(t, filepath.Join(root, "_"), SessionDir(root, ""))
}

func TestNewSnapshotManagerUsesHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	sm := NewSnapshotManager("chat-1")
	assert.Equal(t, filepath.Join(home, ".celeste", "checkpoints", "chat-1"), sm.Dir())
}
