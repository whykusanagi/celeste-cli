//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package server

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/codegraph"
)

// #392 review: an update while another celeste process holds the index
// lock is not a JSON-RPC error; the result reports the index as it is and
// says the update was skipped.
func TestCelesteIndex_UpdateWhileAnotherIndexerWritesIsSoft(t *testing.T) {
	srv, ws := newTestServerWithWorkspace(t)
	writeTSFile(t, ws, "a.ts", "export function helper() { return 1 }\n")

	lockFile := codegraph.DefaultIndexPath(ws) + ".lock"
	f, err := os.OpenFile(lockFile, os.O_RDWR|os.O_CREATE, 0o644)
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	require.NoError(t, unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB))

	resp, payload := callTool(t, srv, "celeste_index", map[string]any{"operation": "update", "workspace": ws})
	require.Nil(t, resp.Error)
	assert.NotEqual(t, true, payload["isError"])
	content := payload["content"].([]any)
	var report map[string]any
	require.NoError(t, json.Unmarshal([]byte(content[0].(map[string]any)["text"].(string)), &report))
	assert.Equal(t, "update", report["operation"])
	assert.Contains(t, report["skipped"], "another celeste process")
}

// Review of #406: while an indexer holds the lock to finish an update of a
// graph that still holds rows, the query tools answer from it with a note
// that results may be incomplete, instead of "being built".
func TestQueryTools_PopulatedIndexWhileUpdating(t *testing.T) {
	srv, ws := newTestServerWithWorkspace(t)
	writeTSFile(t, ws, "a.ts", "export function helper() { return 1 }\n")
	_, payload := callTool(t, srv, "celeste_index", map[string]any{"operation": "rebuild"})
	require.NotEqual(t, true, payload["isError"], payloadText(t, payload))

	idx, _, err := srv.indexerFor(ws)
	require.NoError(t, err)
	require.NoError(t, idx.Store().RescopeGraph("live-run"))

	lockFile := codegraph.DefaultIndexPath(ws) + ".lock"
	f, err := os.OpenFile(lockFile, os.O_RDWR|os.O_CREATE, 0o644)
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	require.NoError(t, unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB))

	assertServedWithNote(t, srv, "is being updated")
	for _, tc := range queryToolCalls {
		_, payload := callTool(t, srv, tc.tool, tc.args)
		assert.NotContains(t, payloadText(t, payload), "being built", tc.tool)
	}
}
