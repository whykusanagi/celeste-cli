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

	done := make(chan map[string]any, 1)
	go func() {
		_, payload := callTool(t, srv, "celeste_index", map[string]any{"operation": "rebuild"})
		done <- payload
	}()

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
	case payload := <-done:
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
