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

// TestCapToolResultRechecksTotalLimit: the limit on all sessions' spills
// together holds for every spill of a run, not only at its first prune: a
// spill that would pass it removes the oldest other sessions first
// (CodeRabbit review of #424).
func TestCapToolResultRechecksTotalLimit(t *testing.T) {
	withSpillLimits(t, 1<<20, 5000)
	oldT := maxTotalSpillBytes
	maxTotalSpillBytes = 10000
	t.Cleanup(func() { maxTotalSpillBytes = oldT })
	base := t.TempDir()
	for i, sess := range []string{"s1", "s2", "s3", "s4", "s5"} {
		_, _, err := CapToolResult(strings.Repeat("x", 4000), 1024, sess, "c", base)
		require.NoError(t, err, "spill %d", i)
		var total int64
		des, err := os.ReadDir(base)
		require.NoError(t, err)
		for _, d := range des {
			total += dirBytes(filepath.Join(base, d.Name()))
		}
		assert.LessOrEqual(t, total, maxTotalSpillBytes, "after spill %d", i)
		_, err = os.Stat(filepath.Join(base, sess, "c.txt"))
		require.NoError(t, err, "the current session's spill was removed")
		// Distinct modification times order the sessions.
		past := time.Now().Add(time.Duration(i-10) * time.Minute)
		require.NoError(t, os.Chtimes(filepath.Join(base, sess, "c.txt"), past, past))
	}
}

// TestCapToolResultConcurrentSpillsKeepSessionQuota: two spills of one
// session at once cannot both pass the quota check (Aikido review of #424).
func TestCapToolResultConcurrentSpillsKeepSessionQuota(t *testing.T) {
	withSpillLimits(t, 1<<20, 5000)
	base := t.TempDir()
	arrived := make(chan struct{}, 2)
	testHookSpillChecked = func() {
		arrived <- struct{}{}
		// Wait for the other spill to pass its check too, unless a lock
		// keeps it out.
		deadline := time.After(300 * time.Millisecond)
		for len(arrived) < 2 {
			select {
			case <-deadline:
				return
			case <-time.After(time.Millisecond):
			}
		}
	}
	t.Cleanup(func() { testHookSpillChecked = nil })
	errs := make(chan error, 2)
	for _, id := range []string{"a", "b"} {
		go func() {
			_, _, err := CapToolResult(strings.Repeat("x", 3000), 1024, "sess", id, base)
			errs <- err
		}()
	}
	failed := 0
	for range 2 {
		if <-errs != nil {
			failed++
		}
	}
	assert.Equal(t, 1, failed, "exactly one of two spills over the quota is refused")
	assert.LessOrEqual(t, dirBytes(filepath.Join(base, "sess")), int64(5000))
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
