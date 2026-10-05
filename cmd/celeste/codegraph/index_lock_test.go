package codegraph

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every pooled connection waits for a lock instead of failing at once with
// SQLITE_BUSY when another connection or process is writing (#392).
func TestStore_EveryConnectionHasBusyTimeout(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "codegraph.db"))
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		c, err := store.db.Conn(ctx)
		require.NoError(t, err)
		defer c.Close()
		var ms int
		require.NoError(t, c.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&ms))
		require.Positive(t, ms, "connection %d: busy_timeout should be set", i)
	}
}

// A mark is cleared only by the indexer that set it.
func TestStore_DeleteMetaIfOnlyDeletesMatchingValue(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	require.NoError(t, store.SetMeta(metaBuildInProgress, []byte("other")))
	require.NoError(t, store.DeleteMetaIf(metaBuildInProgress, "mine"))
	v, err := store.GetMeta(metaBuildInProgress)
	require.NoError(t, err)
	assert.Equal(t, "other", string(v), "another indexer's mark stays")
	require.NoError(t, store.DeleteMetaIf(metaBuildInProgress, "other"))
	v, err = store.GetMeta(metaBuildInProgress)
	require.NoError(t, err)
	assert.Nil(t, v)
}

func openIndexer(t *testing.T, ws, db string) *Indexer {
	t.Helper()
	idx, err := NewIndexer(ws, db)
	require.NoError(t, err)
	t.Cleanup(func() { _ = idx.Close() })
	return idx
}

func shortLockWait(t *testing.T, d time.Duration) {
	t.Helper()
	old := buildLockWait
	buildLockWait = d
	t.Cleanup(func() { buildLockWait = old })
}

// #392: two Indexers on one database (the MCP server's, an MCP chat Env's,
// the TUI's). While one builds, the other's Update skips instead of seeing
// the build's mark and wiping the graph with a build of its own, and its
// explicit Build waits for the first to finish.
func TestIndexLock_SecondIndexerCannotWipeARunningBuild(t *testing.T) {
	files := manyPyFiles(80)
	ref, _ := buildFixture(t, files)
	want := edgeKeys(t, ref)

	ws := writeFixture(t, files)
	db := filepath.Join(t.TempDir(), "cg.db")
	a := openIndexer(t, ws, db)
	b := openIndexer(t, ws, db)

	buildDone := make(chan error, 1)
	var hookErr error
	testHookAfterPass1 = func() {
		testHookAfterPass1 = nil
		before, beforeSyms := graphCounts(t, a)
		mark, err := b.store.GetMeta(metaBuildInProgress)
		require.NoError(t, err)
		require.NotNil(t, mark, "A's build is in progress")

		hookErr = b.UpdateWithContext(context.Background())
		after, afterSyms := graphCounts(t, a)
		assert.Equal(t, before, after, "B's update must not touch A's build")
		assert.Equal(t, beforeSyms, afterSyms)

		// B's explicit Build waits for A's lock and then runs.
		go func() { buildDone <- b.Build() }()
		time.Sleep(50 * time.Millisecond)
		mid, _ := graphCounts(t, a)
		assert.Equal(t, before, mid, "B's build must wait for A's")
	}
	t.Cleanup(func() { testHookAfterPass1 = nil })

	require.NoError(t, a.Build())
	require.ErrorIs(t, hookErr, ErrIndexBusy)
	assert.Equal(t, want, edgeKeys(t, a), "A's build completes intact")

	select {
	case err := <-buildDone:
		require.NoError(t, err)
	case <-time.After(2 * time.Minute):
		t.Fatal("B's build never got the lock")
	}
	assert.Equal(t, want, edgeKeys(t, b))
	mark, err := b.store.GetMeta(metaBuildInProgress)
	require.NoError(t, err)
	assert.Nil(t, mark)
}

// An explicit Build that cannot get the lock in time gives up with
// ErrIndexBusy and leaves the graph alone.
func TestIndexLock_BuildGivesUpAfterWait(t *testing.T) {
	idx, _ := buildFixture(t, manyPyFiles(5))
	want := edgeKeys(t, idx)
	held, err := tryLockIndex(lockPath(idx.store.path))
	require.NoError(t, err)
	defer held.unlock()

	shortLockWait(t, 200*time.Millisecond)
	require.ErrorIs(t, idx.Build(), ErrIndexBusy)
	require.ErrorIs(t, idx.Update(), ErrIndexBusy)
	assert.Equal(t, want, edgeKeys(t, idx))
}

// Indexers racing on one database in the same process never leave it
// emptied or marked unfinished.
func TestIndexLock_ConcurrentIndexersLeaveACompleteGraph(t *testing.T) {
	files := manyPyFiles(40)
	ref, _ := buildFixture(t, files)
	want := edgeKeys(t, ref)

	ws := writeFixture(t, files)
	db := filepath.Join(t.TempDir(), "cg.db")
	idxs := []*Indexer{openIndexer(t, ws, db), openIndexer(t, ws, db), openIndexer(t, ws, db)}
	errs := make(chan error, 3*4)
	for i, idx := range idxs {
		go func() {
			for j := 0; j < 4; j++ {
				var err error
				if (i+j)%2 == 0 {
					err = idx.Build()
				} else {
					err = idx.Update()
				}
				if errors.Is(err, ErrIndexBusy) {
					err = nil
				}
				errs <- err
			}
		}()
	}
	for i := 0; i < cap(errs); i++ {
		require.NoError(t, <-errs)
	}
	require.NoError(t, idxs[0].Update())
	assert.Equal(t, want, edgeKeys(t, idxs[0]))
	mark, err := idxs[0].store.GetMeta(metaBuildInProgress)
	require.NoError(t, err)
	assert.Nil(t, mark)
}

const lockHelperEnv = "CODEGRAPH_TEST_LOCK_HELPER"

// TestIndexLockHelperProcess is not a test: run as a subprocess by
// TestIndexLock_OtherProcessAndStaleLock, it takes the index lock, says so
// and holds it until killed.
func TestIndexLockHelperProcess(t *testing.T) {
	path := os.Getenv(lockHelperEnv)
	if path == "" {
		t.Skip("helper process only")
	}
	l, err := tryLockIndex(path)
	if err != nil {
		os.Stdout.WriteString("error " + err.Error() + "\n")
		os.Exit(2)
	}
	_ = l
	os.Stdout.WriteString("locked\n")
	time.Sleep(10 * time.Minute)
	os.Exit(0)
}

// Another process holding the lock makes Update skip; once that process
// dies its lock is released by the OS, so the lock file it leaves behind
// does not block the next indexer.
func TestIndexLock_OtherProcessAndStaleLock(t *testing.T) {
	if lockIsNoop {
		t.Skip("no OS file lock on this platform")
	}
	idx, _ := buildFixture(t, manyPyFiles(5))
	want := edgeKeys(t, idx)
	path := lockPath(idx.store.path)

	cmd := exec.Command(os.Args[0], "-test.run=^TestIndexLockHelperProcess$", "-test.count=1")
	cmd.Env = append(os.Environ(), lockHelperEnv+"="+path)
	out, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })

	line, err := bufio.NewReader(out).ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "locked\n", line)

	require.ErrorIs(t, idx.Update(), ErrIndexBusy, "another process holds the lock")
	assert.Equal(t, want, edgeKeys(t, idx))

	require.NoError(t, cmd.Process.Kill())
	_ = cmd.Wait()
	_, err = os.Stat(path)
	require.NoError(t, err, "the dead process leaves its lock file behind")

	// The OS drops a dead process's lock; allow it a moment on Windows.
	deadline := time.Now().Add(10 * time.Second)
	for {
		err = idx.Update()
		if !errors.Is(err, ErrIndexBusy) || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	require.NoError(t, err, "a stale lock from a dead process must not block")
	assert.Equal(t, want, edgeKeys(t, idx))
}

// Removing an index (rebuild, reset) takes the same lock, so it never
// deletes a database another indexer is writing.
func TestRemoveIndex_WaitsForTheLock(t *testing.T) {
	db := filepath.Join(t.TempDir(), "cg.db")
	store, err := NewStore(db)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	held, err := tryLockIndex(lockPath(db))
	require.NoError(t, err)
	shortLockWait(t, 200*time.Millisecond)
	require.ErrorIs(t, RemoveIndex(context.Background(), db), ErrIndexBusy)
	_, err = os.Stat(db)
	require.NoError(t, err, "a locked index is not removed")

	held.unlock()
	require.NoError(t, RemoveIndex(context.Background(), db))
	_, err = os.Stat(db)
	assert.True(t, os.IsNotExist(err))
	require.NoError(t, RemoveIndex(context.Background(), db), "removing a missing index is fine")
}
