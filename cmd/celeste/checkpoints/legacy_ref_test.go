package checkpoints

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An entry from an index written before Entry.Root (Root "") made through
// an in-directory symlink (CLAUDE.md -> AGENTS.md) is undone on the file
// it changed: the link stays a link.
func TestLegacyEntryThroughInDirSymlinkKeepsTheLink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "AGENTS.md")
	link := filepath.Join(dir, "CLAUDE.md")
	write(t, target, "after")
	if err := os.Symlink("AGENTS.md", link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	ref, err := openRef(Entry{Path: link})
	require.NoError(t, err)
	defer ref.close()
	require.NoError(t, ref.replace([]byte("before"), 0o644))

	fi, err := os.Lstat(link)
	require.NoError(t, err)
	assert.NotZero(t, fi.Mode()&os.ModeSymlink, "the symlink was replaced by a file")
	assert.Equal(t, "before", read(t, target))
}

// A legacy entry whose symlink leads out of its directory is still never
// written through: the link itself is replaced.
func TestLegacyEntryLinkOutOfDirIsNotFollowed(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "ws")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	outside := filepath.Join(base, "outside.txt")
	write(t, outside, "OUTSIDE")
	link := filepath.Join(dir, "f.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	ref, err := openRef(Entry{Path: link})
	require.NoError(t, err)
	defer ref.close()
	require.NoError(t, ref.replace([]byte("restored"), 0o644))
	assert.Equal(t, "OUTSIDE", read(t, outside))
	assert.Equal(t, "restored", read(t, link))
}

// A legacy entry whose final component is a symlink out of its directory
// is never read through: its state and contents are refused and its stat
// is the link's own (CodeRabbit review of #421).
func TestLegacyEntryLinkOutOfDirIsNotRead(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "ws")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	outside := filepath.Join(base, "outside.txt")
	write(t, outside, "OUTSIDE")
	link := filepath.Join(dir, "f.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	ref, err := openRef(Entry{Path: link})
	require.NoError(t, err)
	defer ref.close()
	require.Nil(t, ref.root, "a link out of its directory gets no root")

	data, err := ref.readFile()
	assert.Error(t, err, "read through the link: %q", data)
	_, err = entryState(Entry{Path: link})
	assert.Error(t, err)
	fi, err := ref.stat()
	require.NoError(t, err)
	assert.NotZero(t, fi.Mode()&os.ModeSymlink, "stat followed the link")
}
