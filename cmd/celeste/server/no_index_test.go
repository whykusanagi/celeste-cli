package server

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/codegraph"
)

var queryToolCalls = []struct {
	tool string
	args map[string]any
}{
	{"celeste_code_review", map[string]any{}},
	{"celeste_code_graph", map[string]any{"symbol": "helper"}},
	{"celeste_code_search", map[string]any{"query": "helper"}},
	{"celeste_code_symbols", map[string]any{"file": "a.ts"}},
}

func assertNoIndexError(t *testing.T, srv *Server, ws string) {
	t.Helper()
	for _, tc := range queryToolCalls {
		t.Run(tc.tool, func(t *testing.T) {
			resp, payload := callTool(t, srv, tc.tool, tc.args)
			require.Nil(t, resp.Error)
			assert.Equal(t, true, payload["isError"], "a query without an index is an error result")
			text := payloadText(t, payload)
			assert.Contains(t, text, "No code graph index for "+ws)
			assert.Contains(t, text, "celeste_index")
			assert.NotContains(t, text, "looks clean")
			assert.NotContains(t, text, "not found")
		})
	}
}

// #399: on a never-indexed workspace the query tools say there is no index
// instead of "Codebase looks clean" / "Symbol not found", and they do not
// create the index database on the way.
func TestQueryTools_NeverIndexedWorkspace(t *testing.T) {
	srv, ws := newTestServerWithWorkspace(t)
	writeTSFile(t, ws, "a.ts", "export function helper() { return 1 }\n")

	assertNoIndexError(t, srv, ws)

	_, err := os.Stat(codegraph.IndexPath(ws))
	assert.True(t, os.IsNotExist(err), "a query must not create the index database: %v", err)
}

// #399: an index database that exists but was never built (celeste_index
// status opens one) is still no index.
func TestQueryTools_OpenedButNeverBuiltIndex(t *testing.T) {
	srv, ws := newTestServerWithWorkspace(t)
	writeTSFile(t, ws, "a.ts", "export function helper() { return 1 }\n")
	_, payload := callTool(t, srv, "celeste_index", map[string]any{"operation": "status"})
	require.NotEqual(t, true, payload["isError"], payloadText(t, payload))

	assertNoIndexError(t, srv, ws)
}

// After a build the same calls answer from the graph.
func TestQueryTools_AfterRebuildAnswer(t *testing.T) {
	srv, ws := newTestServerWithWorkspace(t)
	writeTSFile(t, ws, "a.ts", "export function helper() { return 1 }\n")
	_, payload := callTool(t, srv, "celeste_index", map[string]any{"operation": "rebuild"})
	require.NotEqual(t, true, payload["isError"], payloadText(t, payload))

	for _, tc := range queryToolCalls {
		_, payload := callTool(t, srv, tc.tool, tc.args)
		assert.NotEqual(t, true, payload["isError"], "%s: %s", tc.tool, payloadText(t, payload))
		assert.NotContains(t, payloadText(t, payload), "No code graph index")
	}
}

// #399: an unknown kinds value is rejected with the valid list instead of
// reporting a clean codebase.
func TestCodeReview_UnknownKindRejected(t *testing.T) {
	srv, ws := newTestServerWithWorkspace(t)
	writeTSFile(t, ws, "a.ts", "export function helper() { return 1 }\n")
	_, payload := callTool(t, srv, "celeste_index", map[string]any{"operation": "rebuild"})
	require.NotEqual(t, true, payload["isError"], payloadText(t, payload))

	_, payload = callTool(t, srv, "celeste_code_review", map[string]any{"kinds": "STUB,BOGUS"})
	assert.Equal(t, true, payload["isError"])
	text := payloadText(t, payload)
	assert.Contains(t, text, "BOGUS")
	for _, k := range []string{"ALL", "LAZY_REDIRECT", "STUB", "PLACEHOLDER", "TODO_FIXME", "EMPTY_HANDLER", "HARDCODED"} {
		assert.Contains(t, text, k)
	}
	assert.NotContains(t, text, "looks clean")
}

// #399: a rebuild killed after it emptied the graph leaves the earlier
// build's graph version behind; queries say the build did not finish
// instead of answering from the emptied graph.
func TestQueryTools_InterruptedRebuild(t *testing.T) {
	srv, ws := newTestServerWithWorkspace(t)
	writeTSFile(t, ws, "a.ts", "export function helper() { return 1 }\n")
	_, payload := callTool(t, srv, "celeste_index", map[string]any{"operation": "rebuild"})
	require.NotEqual(t, true, payload["isError"], payloadText(t, payload))

	idx, _, err := srv.indexerFor(ws)
	require.NoError(t, err)
	require.NoError(t, idx.Store().SetMeta("build_in_progress", []byte("dead-run")))
	require.NoError(t, idx.Store().ResetGraph())

	for _, tc := range queryToolCalls {
		t.Run(tc.tool, func(t *testing.T) {
			_, payload := callTool(t, srv, tc.tool, tc.args)
			assert.Equal(t, true, payload["isError"])
			text := payloadText(t, payload)
			assert.Contains(t, text, "did not finish")
			assert.Contains(t, text, "celeste_index")
			assert.NotContains(t, text, "looks clean")
		})
	}
}

// assertServedWithNote checks that every query tool answers from the graph
// with note above the answer, and never suggests a rebuild.
func assertServedWithNote(t *testing.T, srv *Server, note string) {
	t.Helper()
	for _, tc := range queryToolCalls {
		t.Run(tc.tool, func(t *testing.T) {
			_, payload := callTool(t, srv, tc.tool, tc.args)
			text := payloadText(t, payload)
			assert.NotEqual(t, true, payload["isError"], text)
			assert.True(t, strings.HasPrefix(text, "Note: "), text)
			assert.Contains(t, text, note)
			assert.Contains(t, text, "results may be incomplete")
			assert.NotContains(t, text, "rebuild")
			assert.NotContains(t, text, "No code graph index")
		})
	}
	_, payload := callTool(t, srv, "celeste_code_graph", map[string]any{"symbol": "helper"})
	assert.Contains(t, payloadText(t, payload), "## helper", "the answer comes from the graph")
}

// Review of #406: an upgrade or edge-scope refresh sets build_in_progress
// on a graph that still holds every row. When the update that set it was
// cancelled (no indexer holds the lock), the query tools still answer, say
// the last update did not finish and suggest an update, never a rebuild.
func TestQueryTools_PopulatedIndexWithUnfinishedUpdate(t *testing.T) {
	srv, ws := newTestServerWithWorkspace(t)
	writeTSFile(t, ws, "a.ts", "export function helper() { return 1 }\n")
	_, payload := callTool(t, srv, "celeste_index", map[string]any{"operation": "rebuild"})
	require.NotEqual(t, true, payload["isError"], payloadText(t, payload))

	idx, _, err := srv.indexerFor(ws)
	require.NoError(t, err)
	require.NoError(t, idx.Store().RescopeGraph("cancelled-run"))

	assertServedWithNote(t, srv, "did not finish")
	for _, tc := range queryToolCalls {
		_, payload := callTool(t, srv, tc.tool, tc.args)
		assert.Contains(t, payloadText(t, payload), "operation update", tc.tool)
	}

	// The update finishes it and the note goes away.
	_, payload = callTool(t, srv, "celeste_index", map[string]any{"operation": "update"})
	require.NotEqual(t, true, payload["isError"], payloadText(t, payload))
	_, payload = callTool(t, srv, "celeste_code_graph", map[string]any{"symbol": "helper"})
	assert.NotContains(t, payloadText(t, payload), "Note:")
}

// The same for main's v1 upgrade cut short (#394): UpgradeGraph keeps the
// non-Go rows and sets the mark.
func TestQueryTools_PopulatedIndexWithUnfinishedUpgrade(t *testing.T) {
	srv, ws := newTestServerWithWorkspace(t)
	writeTSFile(t, ws, "a.ts", "export function helper() { return 1 }\n")
	_, payload := callTool(t, srv, "celeste_index", map[string]any{"operation": "rebuild"})
	require.NotEqual(t, true, payload["isError"], payloadText(t, payload))

	idx, _, err := srv.indexerFor(ws)
	require.NoError(t, err)
	require.NoError(t, idx.Store().UpgradeGraph("cancelled-run"))

	assertServedWithNote(t, srv, "did not finish")
}
