package checkpoints

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ComputeDiff reads backups under the session lock: an undo in another
// process (which holds that lock while it removes a backup) cannot delete
// a backup between the index read and the backup read.
func TestComputeDiffWaitsForTheSessionLock(t *testing.T) {
	dir := t.TempDir()
	sm := newSnapshotManagerWithBase(filepath.Join(dir, "session"))
	f := filepath.Join(dir, "a.txt")
	write(t, f, "one\n")
	c, err := sm.Checkpoint(f, "")
	require.NoError(t, err)
	write(t, f, "two\n")
	require.NoError(t, c.Commit())

	unlock, err := lockSession(sm.Dir(), false)
	require.NoError(t, err)
	done := make(chan []FileChange, 1)
	go func() {
		changes, _ := sm.ComputeDiff()
		done <- changes
	}()
	select {
	case <-done:
		unlock()
		t.Fatal("ComputeDiff ran while another holder had the session lock")
	case <-time.After(200 * time.Millisecond):
	}
	unlock()
	select {
	case changes := <-done:
		require.Len(t, changes, 1)
		require.Empty(t, changes[0].Err)
	case <-time.After(5 * time.Second):
		t.Fatal("ComputeDiff did not finish after the lock was released")
	}
}

// The session lock covers only the index and backup reads: the current
// files are read and diffed after it is released, so a large /diff never
// holds other processes past lockWait.
func TestComputeDiffReleasesTheLockBeforeDiffing(t *testing.T) {
	dir := t.TempDir()
	sm := newSnapshotManagerWithBase(filepath.Join(dir, "session"))
	f := filepath.Join(dir, "a.txt")
	write(t, f, "one\n")
	c, err := sm.Checkpoint(f, "")
	require.NoError(t, err)
	write(t, f, "two\n")
	require.NoError(t, c.Commit())

	var lockErr error
	var waited time.Duration
	old := beforeDiffing
	beforeDiffing = func() {
		start := time.Now()
		unlock, err := lockSession(sm.Dir(), false)
		waited, lockErr = time.Since(start), err
		if err == nil {
			unlock()
		}
	}
	t.Cleanup(func() { beforeDiffing = old })

	changes, err := sm.ComputeDiff()
	require.NoError(t, err)
	require.NoError(t, lockErr, "the session lock was still held while diffing")
	require.Less(t, waited, time.Second)
	require.Len(t, changes, 1)
	require.Equal(t, 1, changes[0].Insertions)
	require.Equal(t, 1, changes[0].Deletions)
}
