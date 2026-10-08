package ctxmgr

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/filelock"
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
		// Distinct modification times, all past spillActiveAge, order the
		// sessions.
		past := time.Now().Add(time.Duration(i-10) * time.Hour)
		require.NoError(t, os.Chtimes(filepath.Join(base, sess, "c.txt"), past, past))
		require.NoError(t, os.Chtimes(filepath.Join(base, sess), past, past))
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

// TestCapToolResultWaitsForAnotherProcessSpill: the quota checks and the
// write also exclude other celeste processes spilling under the same base
// (codex review of this branch).
func TestCapToolResultWaitsForAnotherProcessSpill(t *testing.T) {
	if !filelock.Supported {
		t.Skip("no file lock on this platform")
	}
	base := filepath.Join(t.TempDir(), "tool-results")
	unlock, err := filelock.Lock(spillLockPath(base), time.Second)
	require.NoError(t, err) // another process, mid-spill
	done := make(chan error, 1)
	go func() {
		_, _, err := CapToolResult(strings.Repeat("x", 3000), 1024, "sess", "a", base)
		done <- err
	}()
	select {
	case <-done:
		unlock()
		t.Fatal("a spill went ahead while another process held the spill lock")
	case <-time.After(200 * time.Millisecond):
	}
	unlock()
	require.NoError(t, <-done)
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

// TestCapToolResultPrunesUnderSpillLock: the first spill's prune of old
// sessions and the creation of its own session directory happen under the
// spill lock, so another process's prune cannot remove a session directory
// between its creation and its spill (review of the round 2 branch).
func TestCapToolResultPrunesUnderSpillLock(t *testing.T) {
	if !filelock.Supported {
		t.Skip("no file lock on this platform")
	}
	base := filepath.Join(t.TempDir(), "tool-results")
	expired := filepath.Join(base, "expired")
	require.NoError(t, os.MkdirAll(expired, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(expired, "c.txt"), []byte("data"), 0o600))
	longAgo := time.Now().Add(-2 * SpillKeepAge)
	require.NoError(t, os.Chtimes(filepath.Join(expired, "c.txt"), longAgo, longAgo))
	require.NoError(t, os.Chtimes(expired, longAgo, longAgo))

	unlock, err := filelock.Lock(spillLockPath(base), time.Second)
	require.NoError(t, err) // another process, mid-spill
	done := make(chan error, 1)
	go func() {
		_, _, err := CapToolResult(strings.Repeat("x", 3000), 1024, "sess", "a", base)
		done <- err
	}()
	select {
	case err := <-done:
		unlock()
		t.Fatalf("a spill went ahead while another process held the spill lock: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	_, err = os.Stat(expired)
	assert.NoError(t, err, "the prune ran outside the spill lock")
	_, err = os.Stat(filepath.Join(base, "sess"))
	assert.True(t, os.IsNotExist(err), "the session directory was created outside the spill lock")
	unlock()
	require.NoError(t, <-done)
	_, err = os.Stat(expired)
	assert.True(t, os.IsNotExist(err), "the expired session was kept")
	_, err = os.Stat(filepath.Join(base, "sess", "a.txt"))
	assert.NoError(t, err)
}

// TestCapToolResultKeepsRecentlyActiveSessions: the prune a spill runs to
// stay within the total limit removes only sessions idle for a while, not
// one another celeste process may still be using; if that is not enough,
// the result is cut in memory instead (review of the round 2 branch).
func TestCapToolResultKeepsRecentlyActiveSessions(t *testing.T) {
	withSpillLimits(t, 1<<20, 1<<20)
	oldT := maxTotalSpillBytes
	maxTotalSpillBytes = 10000
	t.Cleanup(func() { maxTotalSpillBytes = oldT })
	base := t.TempDir()
	write := func(sess string, age time.Duration) string {
		dir := filepath.Join(base, sess)
		require.NoError(t, os.MkdirAll(dir, 0o700))
		p := filepath.Join(dir, "c.txt")
		require.NoError(t, os.WriteFile(p, []byte(strings.Repeat("o", 4000)), 0o600))
		at := time.Now().Add(-age)
		require.NoError(t, os.Chtimes(p, at, at))
		require.NoError(t, os.Chtimes(dir, at, at))
		return dir
	}
	idle := write("idle", 2*time.Hour)
	active := write("active", 10*time.Second)

	_, _, err := CapToolResult(strings.Repeat("x", 4000), 1024, "sess", "a", base)
	require.NoError(t, err)
	_, err = os.Stat(idle)
	assert.True(t, os.IsNotExist(err), "the idle session was kept past the total limit")
	_, err = os.Stat(active)
	require.NoError(t, err, "a recently active session was pruned")

	_, capped, err := CapToolResult(strings.Repeat("y", 4000), 1024, "sess", "b", base)
	require.Error(t, err, "a spill past the total limit went ahead")
	assert.False(t, capped)
	_, err = os.Stat(active)
	assert.NoError(t, err, "a recently active session was pruned")
	_, err = os.Stat(filepath.Join(base, "sess", "b.txt"))
	assert.True(t, os.IsNotExist(err))
}
