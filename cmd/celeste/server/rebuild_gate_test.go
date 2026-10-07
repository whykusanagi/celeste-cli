package server

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Review of #393: a call that opens the index while a rebuild runs, after
// the rebuild evicted the cached Indexer and before it removed the old
// database, must not be left attached to that old database (and must not
// make the rebuild keep it in the cache). While a rebuild of a workspace
// is in flight, opening its index is refused as "being built"; afterwards
// the rebuilt index is the one every call gets.
func TestRebuild_ConcurrentQueryNeverGetsOldIndex(t *testing.T) {
	srv, ws := newTestServerWithWorkspace(t)
	writeTSFile(t, ws, "a.ts", "export function helper() { return 1 }\n")
	_, payload := callTool(t, srv, "celeste_index", map[string]any{"operation": "rebuild"})
	require.NotEqual(t, true, payload["isError"], payloadText(t, payload))

	// A file the old index does not have.
	writeTSFile(t, ws, "b.ts", "export function freshlyAdded() { return 2 }\n")

	evicted := make(chan struct{})
	release := make(chan struct{})
	testHookRebuildEvicted = func() {
		close(evicted)
		<-release
	}
	t.Cleanup(func() { testHookRebuildEvicted = nil })

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, payload := callTool(t, srv, "celeste_index", map[string]any{"operation": "rebuild"})
		assert.NotEqual(t, true, payload["isError"], payloadText(t, payload))
	}()
	<-evicted

	// Mid-rebuild: every way in to the index is refused, not opened.
	if _, _, err := srv.indexerFor(ws); assert.Error(t, err) {
		assert.Contains(t, err.Error(), "being built")
	}
	for _, tc := range queryToolCalls {
		_, payload := callTool(t, srv, tc.tool, tc.args)
		assert.Equal(t, true, payload["isError"], tc.tool)
		assert.Contains(t, payloadText(t, payload), "being built", tc.tool)
	}
	_, payload = callTool(t, srv, "celeste_index", map[string]any{"operation": "rebuild"})
	assert.Equal(t, true, payload["isError"], "a second rebuild of the same workspace waits for none: it is refused")

	close(release)
	wg.Wait()
	testHookRebuildEvicted = nil

	_, payload = callTool(t, srv, "celeste_code_search", map[string]any{"query": "freshlyAdded", "mode": "keyword"})
	require.NotEqual(t, true, payload["isError"], payloadText(t, payload))
	assert.Contains(t, payloadText(t, payload), "freshlyAdded", "the rebuilt index is served")
}
