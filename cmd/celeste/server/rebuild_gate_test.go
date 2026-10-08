package server

import (
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

	rebuilt := callToolAsync(srv, "celeste_index", map[string]any{"operation": "rebuild"})
	<-evicted

	// Mid-rebuild: every way in to the index is refused, not opened.
	if _, _, _, err := srv.indexerFor(ws); assert.Error(t, err) {
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
	payload = toolCallPayload(t, <-rebuilt)
	assert.NotEqual(t, true, payload["isError"], payloadText(t, payload))
	testHookRebuildEvicted = nil

	_, payload = callTool(t, srv, "celeste_code_search", map[string]any{"query": "freshlyAdded", "mode": "keyword"})
	require.NotEqual(t, true, payload["isError"], payloadText(t, payload))
	assert.Contains(t, payloadText(t, payload), "freshlyAdded", "the rebuilt index is served")
}

// Review of #393, chat path: a celeste chat call that builds its Env while
// a rebuild runs opens the old database (its Setup does not go through
// indexerFor). The rebuild retires that Env once the rebuilt index is in
// place, so the next chat call builds one on the new database instead of
// reading the deleted one for the Env's whole lifetime.
func TestRebuild_RetiresChatEnvBuiltDuringRebuild(t *testing.T) {
	srv, ws := newTestServerWithWorkspace(t)
	writeTSFile(t, ws, "a.ts", "export function helper() { return 1 }\n")
	f := newFakeEnvs(t)
	srv.chatEnvs.close()
	srv.chatEnvs = f.chatEnvs

	before, _ := f.use(t, ws)
	var during *chatEnv
	testHookRebuildEvicted = func() { during, _ = f.use(t, ws) }
	t.Cleanup(func() { testHookRebuildEvicted = nil })
	_, payload := callTool(t, srv, "celeste_index", map[string]any{"operation": "rebuild"})
	testHookRebuildEvicted = nil
	require.NotEqual(t, true, payload["isError"], payloadText(t, payload))

	require.NotNil(t, during)
	assert.True(t, f.isClosed(before), "the Env cached before the rebuild is retired")
	assert.NotSame(t, before, during, "the call inside the rebuild built its own Env")
	assert.True(t, f.isClosed(during), "the Env built during the rebuild is retired once it ends")
	after, _ := f.use(t, ws)
	assert.NotSame(t, during, after, "the next chat call builds an Env on the rebuilt index")
	assert.Equal(t, int32(3), f.builds.Load())
}
