package server

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// CodeRabbit review of #414: one directory is one cache key. A workspace
// spelled with a trailing slash or a "." segment names the same index, the
// same rebuild gate and the same chat cache as its clean spelling.
func TestWorkspaceFromArgs_OneKeyPerDirectory(t *testing.T) {
	srv, ws := newTestServerWithWorkspace(t)
	for _, spelling := range []string{ws + "/", ws + "/.", ws + string(filepath.Separator) + "." + string(filepath.Separator)} {
		got, err := srv.workspaceFromArgs(map[string]any{"workspace": spelling})
		require.NoError(t, err, spelling)
		assert.Equal(t, ws, got, spelling)
	}

	writeTSFile(t, ws, "a.ts", "export function helper() { return 1 }\n")
	_, payload := callTool(t, srv, "celeste_index", map[string]any{"operation": "rebuild"})
	require.NotEqual(t, true, payload["isError"], payloadText(t, payload))

	// While ws is being rebuilt, a query naming it with a trailing slash is
	// refused too, not answered from another cache entry.
	srv.indexerMu.Lock()
	srv.rebuilding[ws] = true
	srv.indexerMu.Unlock()
	t.Cleanup(func() {
		srv.indexerMu.Lock()
		delete(srv.rebuilding, ws)
		srv.indexerMu.Unlock()
	})
	_, payload = callTool(t, srv, "celeste_code_search", map[string]any{"query": "helper", "workspace": ws + "/"})
	assert.Equal(t, true, payload["isError"])
	assert.Contains(t, payloadText(t, payload), "being built")
}

// validateWorkspace compares paths with the OS separator, so a workspace
// under home is accepted (and ~/.ssh refused) on Windows too.
func TestValidateWorkspaceUsesOSSeparators(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	server := filepath.Join(home, "srv")
	require.NoError(t, validateWorkspace(filepath.Join(home, "proj"), server))
	require.NoError(t, validateWorkspace(server+string(filepath.Separator), server))
	require.Error(t, validateWorkspace(home, server))
	require.Error(t, validateWorkspace(filepath.Dir(home), server))
	require.Error(t, validateWorkspace(filepath.Join(home, ".ssh", "x"), server))
	require.Error(t, validateWorkspace(home+"x", server))
}
