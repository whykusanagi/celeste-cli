package ctxmgr

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withSpillLimits(t *testing.T, file, session int64) {
	t.Helper()
	oldF, oldS := maxSpillFileBytes, maxSessionSpillBytes
	maxSpillFileBytes, maxSessionSpillBytes = file, session
	t.Cleanup(func() { maxSpillFileBytes, maxSessionSpillBytes = oldF, oldS })
}

// TestCapToolResultCapsSpillFile: a result over the per-file limit spills
// only its first part (Aikido 806869375).
func TestCapToolResultCapsSpillFile(t *testing.T) {
	withSpillLimits(t, 4096, 1<<20)
	base := t.TempDir()
	out, capped, err := CapToolResult(strings.Repeat("x", 20000), 1024, "sess", "call1", base)
	require.NoError(t, err)
	require.True(t, capped)
	fi, err := os.Stat(filepath.Join(base, "sess", "call1.txt"))
	require.NoError(t, err)
	assert.LessOrEqual(t, fi.Size(), int64(4096))
	assert.Contains(t, out, "first 4096 bytes")
}

// TestCapToolResultSessionQuota: once a session's spill files reach the
// quota, a further result is not spilled (Aikido 806869375).
func TestCapToolResultSessionQuota(t *testing.T) {
	withSpillLimits(t, 1<<20, 5000)
	base := t.TempDir()
	_, _, err := CapToolResult(strings.Repeat("x", 3000), 1024, "sess", "a", base)
	require.NoError(t, err)
	_, _, err = CapToolResult(strings.Repeat("y", 3000), 1024, "sess", "b", base)
	require.Error(t, err)
	_, statErr := os.Stat(filepath.Join(base, "sess", "b.txt"))
	assert.True(t, os.IsNotExist(statErr), "a spill over the session quota was written")
}

// TestPruneToolResults: session spill directories older than the retention
// age are removed; the current one and recent ones stay (Aikido 806869375).
func TestPruneToolResults(t *testing.T) {
	base := t.TempDir()
	old := filepath.Join(base, "old")
	recent := filepath.Join(base, "recent")
	current := filepath.Join(base, "current")
	for _, d := range []string{old, recent, current} {
		require.NoError(t, os.MkdirAll(d, 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(d, "c.txt"), []byte("data"), 0o600))
	}
	longAgo := time.Now().Add(-2 * SpillKeepAge)
	for _, p := range []string{old, filepath.Join(old, "c.txt"), current, filepath.Join(current, "c.txt")} {
		require.NoError(t, os.Chtimes(p, longAgo, longAgo))
	}
	require.NoError(t, PruneToolResults(base, "current", time.Now()))
	_, err := os.Stat(old)
	assert.True(t, os.IsNotExist(err), "an expired session's spills were kept")
	_, err = os.Stat(recent)
	assert.NoError(t, err)
	_, err = os.Stat(current)
	assert.NoError(t, err, "the current session's spills were pruned")
}

// TestCapToolResultQuotaIgnoresOverwrittenFile: rewriting the spill file of
// the same tool call id does not count the file it replaces toward the
// session quota.
func TestCapToolResultQuotaIgnoresOverwrittenFile(t *testing.T) {
	withSpillLimits(t, 1<<20, 5000)
	base := t.TempDir()
	for i := 0; i < 3; i++ {
		_, _, err := CapToolResult(strings.Repeat("x", 3000), 1024, "sess", "same", base)
		require.NoError(t, err, "spill %d of the same id", i)
	}
}
