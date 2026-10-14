package ctxmgr

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPruneLeavesUnmarkedSessionShapedDirectory(t *testing.T) {
	base := t.TempDir()
	backup := filepath.Join(base, "backup")
	require.NoError(t, os.Mkdir(backup, 0o700))
	contents := strings.Repeat("b", 8000)
	file := filepath.Join(backup, "precious.txt")
	require.NoError(t, os.WriteFile(file, []byte(contents), 0o600))
	old := time.Now().Add(-2 * SpillKeepAge)
	require.NoError(t, os.Chtimes(file, old, old))
	require.NoError(t, os.Chtimes(backup, old, old))
	assert.Zero(t, totalSpillBytes(base), "unmarked data must not count toward quota")
	require.NoError(t, PruneToolResults(base, "different", time.Now()))
	got, err := os.ReadFile(file)
	require.NoError(t, err, "old non-spill data must survive age pruning")
	assert.Equal(t, contents, string(got))

	oldTotal := maxTotalSpillBytes
	maxTotalSpillBytes = 5000
	t.Cleanup(func() { maxTotalSpillBytes = oldTotal })
	_, _, err = CapToolResult(strings.Repeat("s", 3000), 1024, "active", "call", base)
	require.NoError(t, err, "an unmarked directory must not block a spill")
	_, err = os.ReadFile(file)
	require.NoError(t, err, "unmarked data must survive quota pruning")
}

func TestSpillOwnershipMarkerIsPrivateAndValidated(t *testing.T) {
	base := t.TempDir()
	_, _, err := CapToolResult(strings.Repeat("x", 3000), 1024, "owned", "call", base)
	require.NoError(t, err)
	owned := filepath.Join(base, "owned")
	assert.EqualValues(t, 3000, totalSpillBytes(base))
	info, err := os.Lstat(filepath.Join(owned, ".celeste-spill-v1"))
	require.NoError(t, err)
	assert.True(t, info.Mode().IsRegular(), "the marker must be a regular file")
	if runtime.GOOS != "windows" {
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "the marker must be owner-only")
	}
	if err := os.Remove(filepath.Join(owned, ".celeste-spill-v1")); err != nil {
		t.Fatal(err)
	}
	assert.Zero(t, totalSpillBytes(base), "removing the marker revokes pruning ownership")
}

func TestUnownedPreexistingSpillDirectoryIsNotClaimed(t *testing.T) {
	base := t.TempDir()
	for _, setup := range []struct {
		name   string
		marker func(t *testing.T, dir string)
	}{
		{name: "missing"},
		{name: "invalid", marker: func(t *testing.T, dir string) {
			require.NoError(t, os.WriteFile(filepath.Join(dir, spillMarkerName), []byte("not a Celeste marker"), 0o600))
		}},
		{name: "symlink", marker: func(t *testing.T, dir string) {
			outside := filepath.Join(t.TempDir(), "outside")
			require.NoError(t, os.WriteFile(outside, []byte(spillMarkerBody), 0o600))
			if err := os.Symlink(outside, filepath.Join(dir, spillMarkerName)); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
		}},
	} {
		t.Run(setup.name, func(t *testing.T) {
			dir := filepath.Join(base, setup.name)
			require.NoError(t, os.Mkdir(dir, 0o700))
			original := filepath.Join(dir, "call.txt")
			require.NoError(t, os.WriteFile(original, []byte("unrelated"), 0o600))
			if setup.marker != nil {
				setup.marker(t, dir)
			}
			_, _, err := CapToolResult(strings.Repeat("s", 3000), 1024, setup.name, "call", base)
			require.Error(t, err, "an unrelated directory cannot be claimed as a spill")
			got, err := os.ReadFile(original)
			require.NoError(t, err)
			assert.Equal(t, "unrelated", string(got))
			assert.False(t, ownedSpillDir(dir))
		})
	}
}

func TestMarkedSessionPrunesWhenExpiredWithoutRefreshingTimestamp(t *testing.T) {
	base := t.TempDir()
	_, _, err := CapToolResult(strings.Repeat("s", 3000), 1024, "expired", "call", base)
	require.NoError(t, err)
	dir := filepath.Join(base, "expired")
	old := time.Now().Add(-2 * SpillKeepAge)
	require.NoError(t, os.Chtimes(filepath.Join(dir, "call.txt"), old, old))
	require.NoError(t, os.Chtimes(dir, old, old))
	require.NoError(t, PruneToolResults(base, "other", time.Now()))
	_, err = os.Stat(dir)
	assert.ErrorIs(t, err, os.ErrNotExist)
}
