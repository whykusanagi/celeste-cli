package server

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/codegraph"
)

// TestIndexRebuildWaitsForInFlightQuery: a rebuild must not close the
// Indexer a running call is still using (Aikido 806869451).
func TestIndexRebuildWaitsForInFlightQuery(t *testing.T) {
	srv, dir := newTestServerWithWorkspace(t)
	writeTSFile(t, dir, "a.ts", "export function alpha() { return 1 }\n")

	idx, release, _, err := srv.indexerFor(dir)
	require.NoError(t, err)
	srv.indexerMu.Lock()
	old := srv.indexers[dir]
	srv.indexerMu.Unlock()

	done := callToolAsync(srv, "celeste_index", map[string]any{"operation": "rebuild"})

	// Wait until the rebuild has taken the old Indexer out of the cache.
	deadline := time.Now().Add(10 * time.Second)
	for {
		srv.indexerMu.Lock()
		retired := old.retired
		srv.indexerMu.Unlock()
		if retired {
			break
		}
		if time.Now().After(deadline) {
			release()
			t.Fatal("rebuild never evicted the cached Indexer")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// The in-flight call can still use its Indexer.
	_, statsErr := idx.State()
	select {
	case <-done:
		release()
		t.Fatal("rebuild finished while a call still held the old Indexer")
	default:
	}
	release()
	require.NoError(t, statsErr, "the rebuild closed an Indexer still in use")

	select {
	case r := <-done:
		payload := toolCallPayload(t, r)
		require.NotEqual(t, true, payload["isError"], payloadText(t, payload))
	case <-time.After(60 * time.Second):
		t.Fatal("rebuild did not finish after the call released its Indexer")
	}
}

// TestIndexerCacheIsBounded: the cache keeps at most maxIndexers entries and
// evicts the least recently used idle one first (Aikido 806869934).
func TestIndexerCacheIsBounded(t *testing.T) {
	srv, _ := newTestServerWithWorkspace(t)
	home := os.Getenv("HOME")
	ws := func(i int) string {
		d := filepath.Join(home, "ws"+strconv.Itoa(i))
		require.NoError(t, os.MkdirAll(d, 0o755))
		return d
	}

	// ws0 stays busy for the whole test.
	_, hold, _, err := srv.indexerFor(ws(0))
	require.NoError(t, err)
	defer hold()
	var first *codegraph.Indexer
	// More workspaces than the cache may hold (maxIndexers is small).
	const workspaces = 12
	for i := 1; i <= workspaces; i++ {
		idx, release, _, err := srv.indexerFor(ws(i))
		require.NoError(t, err)
		if i == 1 {
			first = idx
		}
		release()
	}

	srv.indexerMu.Lock()
	n := len(srv.indexers)
	_, busyCached := srv.indexers[ws(0)]
	_, firstCached := srv.indexers[ws(1)]
	srv.indexerMu.Unlock()
	assert.Less(t, n, workspaces+1, "the cache grew with every workspace")
	assert.LessOrEqual(t, n, maxIndexers)
	assert.True(t, busyCached, "a busy Indexer was evicted before idle ones")
	assert.False(t, firstCached, "the least recently used idle Indexer was kept")
	_, statsErr := first.State()
	assert.Error(t, statsErr, "an evicted idle Indexer must be closed")
}

// TestIndexToolRejectsMissingWorkspace: a workspace that does not exist
// gets no index (and no index directory) (Aikido 806869934).
func TestIndexToolRejectsMissingWorkspace(t *testing.T) {
	srv, _ := newTestServerWithWorkspace(t)
	missing := filepath.Join(os.Getenv("HOME"), "no-such-project")
	_, payload := callTool(t, srv, "celeste_index", map[string]any{"operation": "status", "workspace": missing})
	assert.Equal(t, true, payload["isError"], payloadText(t, payload))
	_, err := os.Stat(filepath.Dir(codegraph.IndexPath(missing)))
	assert.True(t, os.IsNotExist(err), "an index directory was created for a missing workspace")
	assert.False(t, srv.indexerCached(missing))
}

// TestIndexRebuildWaitsForEvictedBusyIndexer: an Indexer a call still uses
// stays waited on after eviction drops it from the cache, so a rebuild of
// its workspace does not delete the database under the call
// (Aikido 806869451).
func TestIndexRebuildWaitsForEvictedBusyIndexer(t *testing.T) {
	srv, dir := newTestServerWithWorkspace(t)
	writeTSFile(t, dir, "a.ts", "export function alpha() { return 1 }\n")
	home := os.Getenv("HOME")

	idx, release, _, err := srv.indexerFor(dir)
	require.NoError(t, err)
	released := false
	defer func() {
		if !released {
			release()
		}
	}()
	// Fill the cache with busy entries: the oldest busy one, dir's, is
	// evicted while still in use.
	for i := 0; i < maxIndexers; i++ {
		d := filepath.Join(home, "busy"+strconv.Itoa(i))
		require.NoError(t, os.MkdirAll(d, 0o755))
		_, hold, _, err := srv.indexerFor(d)
		require.NoError(t, err)
		defer hold()
	}
	require.False(t, srv.indexerCached(dir), "the busy Indexer was not evicted")

	done := callToolAsync(srv, "celeste_index", map[string]any{"operation": "rebuild"})
	select {
	case <-done:
		t.Fatal("rebuild finished while a call still held the evicted Indexer")
	case <-time.After(500 * time.Millisecond):
	}
	_, stateErr := idx.State()
	require.NoError(t, stateErr, "the evicted Indexer was closed while in use")
	release()
	released = true

	select {
	case r := <-done:
		payload := toolCallPayload(t, r)
		require.NotEqual(t, true, payload["isError"], payloadText(t, payload))
	case <-time.After(60 * time.Second):
		t.Fatal("rebuild did not finish after the call released its Indexer")
	}
}

// TestRetiredIndexerStaysWaitedOnWhileClosing: an Indexer taken out of the
// cache stays among the entries a rebuild of its workspace waits for until
// its Close has returned, both when its last call releases it and when an
// eviction closes it idle; otherwise a rebuild could delete the database
// under a Close still running (Aikido review of #424).
func TestRetiredIndexerStaysWaitedOnWhileClosing(t *testing.T) {
	srv, dir := newTestServerWithWorkspace(t)
	home := os.Getenv("HOME")
	waitedOn := func(e *indexerEntry) bool {
		srv.indexerMu.Lock()
		defer srv.indexerMu.Unlock()
		for _, r := range srv.retiredBusy[e.path] {
			if r == e {
				return true
			}
		}
		return false
	}
	closing := map[*indexerEntry]bool{}
	testHookIndexerClosing = func(e *indexerEntry) { closing[e] = waitedOn(e) }
	t.Cleanup(func() { testHookIndexerClosing = nil })

	// Busy when evicted: closes on its last release.
	_, release, _, err := srv.indexerFor(dir)
	require.NoError(t, err)
	srv.indexerMu.Lock()
	busy := srv.indexers[dir]
	srv.indexerMu.Unlock()
	var idle *indexerEntry
	var holds []func()
	for i := 0; i < maxIndexers; i++ { // all busy: dir's, the oldest, goes
		d := filepath.Join(home, "ws"+strconv.Itoa(i))
		require.NoError(t, os.MkdirAll(d, 0o755))
		_, r, _, err := srv.indexerFor(d)
		require.NoError(t, err)
		if i == 0 {
			srv.indexerMu.Lock()
			idle = srv.indexers[d]
			srv.indexerMu.Unlock()
		}
		holds = append(holds, r)
	}
	require.False(t, srv.indexerCached(dir), "the busy Indexer was not evicted")
	release()
	for _, r := range holds {
		r()
	}
	require.Contains(t, closing, busy, "the evicted busy Indexer never closed")
	assert.True(t, closing[busy], "a released Indexer was not waited on while it closed")

	// Idle when evicted: closed by the eviction.
	for i := maxIndexers; i < 2*maxIndexers; i++ {
		d := filepath.Join(home, "ws"+strconv.Itoa(i))
		require.NoError(t, os.MkdirAll(d, 0o755))
		_, r, _, err := srv.indexerFor(d)
		require.NoError(t, err)
		r()
	}
	require.Contains(t, closing, idle, "the idle Indexer was never evicted")
	assert.True(t, closing[idle], "an evicted idle Indexer was not waited on while it closed")

	srv.indexerMu.Lock()
	defer srv.indexerMu.Unlock()
	assert.Empty(t, srv.retiredBusy[busy.path], "a closed Indexer stayed in the wait list")
}
