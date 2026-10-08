package server

import (
	"os"
	"path/filepath"
	"strings"
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

// On a case-insensitive filesystem (Windows, macOS) ~/.SSH is ~/.ssh, so the
// protected-directory check compares case-insensitively there.
func TestValidateWorkspaceProtectedDirsIgnoreCase(t *testing.T) {
	prev := workspaceCaseInsensitive
	workspaceCaseInsensitive = true
	t.Cleanup(func() { workspaceCaseInsensitive = prev })
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	server := filepath.Join(home, "srv")
	for _, p := range []string{
		filepath.Join(home, ".SSH"),
		filepath.Join(home, ".Ssh", "keys"),
		filepath.Join(home, ".GnuPG"),
		filepath.Join(home, ".Config", "GCloud"),
		filepath.Join(home, "x", ".KUBE"),
	} {
		require.Error(t, validateWorkspace(p, server), p)
	}
	require.NoError(t, validateWorkspace(filepath.Join(home, "Projects"), server))
}

// One directory is one cache key across symlink aliases, and a symlink under
// home is judged by where it points: an alias of a directory outside home or
// of a protected directory is refused.
func TestWorkspaceFromArgs_SymlinkAliases(t *testing.T) {
	srv, ws := newTestServerWithWorkspace(t)
	home := filepath.Dir(ws)
	proj := filepath.Join(home, "proj")
	require.NoError(t, os.Mkdir(proj, 0o755))
	alias := filepath.Join(home, "alias")
	if err := os.Symlink(proj, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	a, err := srv.workspaceFromArgs(map[string]any{"workspace": proj})
	require.NoError(t, err)
	b, err := srv.workspaceFromArgs(map[string]any{"workspace": alias})
	require.NoError(t, err)
	assert.Equal(t, a, b)

	// An alias of the server's own workspace keys the server's entry.
	wsAlias := filepath.Join(home, "ws-alias")
	require.NoError(t, os.Symlink(ws, wsAlias))
	c, err := srv.workspaceFromArgs(map[string]any{"workspace": wsAlias})
	require.NoError(t, err)
	assert.Equal(t, ws, c)

	outside := t.TempDir()
	out := filepath.Join(home, "out")
	require.NoError(t, os.Symlink(outside, out))
	_, err = srv.workspaceFromArgs(map[string]any{"workspace": out})
	require.Error(t, err)

	require.NoError(t, os.Mkdir(filepath.Join(home, ".ssh"), 0o700))
	keys := filepath.Join(home, "keys")
	require.NoError(t, os.Symlink(filepath.Join(home, ".ssh"), keys))
	_, err = srv.workspaceFromArgs(map[string]any{"workspace": keys})
	require.Error(t, err)
}

// On a case-insensitive filesystem ~/Proj and ~/proj are one directory, so
// they are one key (one index, rebuild gate and chat cache), and a case
// variant of the server's own workspace keys the server's entry. Where the
// filesystem tells the two spellings apart the test has nothing to check.
func TestWorkspaceFromArgs_CaseVariantsShareAKey(t *testing.T) {
	srv, ws := newTestServerWithWorkspace(t)
	home := filepath.Dir(ws)
	proj := filepath.Join(home, "proj")
	require.NoError(t, os.Mkdir(proj, 0o755))
	if _, err := os.Lstat(filepath.Join(home, "PROJ")); err != nil {
		t.Skip("the filesystem is case-sensitive")
	}
	prev := workspaceCaseInsensitive
	workspaceCaseInsensitive = true
	t.Cleanup(func() { workspaceCaseInsensitive = prev })

	a, err := srv.workspaceFromArgs(map[string]any{"workspace": proj})
	require.NoError(t, err)
	for _, variant := range []string{filepath.Join(home, "Proj"), filepath.Join(home, "PROJ")} {
		b, err := srv.workspaceFromArgs(map[string]any{"workspace": variant})
		require.NoError(t, err)
		assert.Equal(t, a, b, variant)
	}

	upper := filepath.Join(filepath.Dir(ws), strings.ToUpper(filepath.Base(ws)))
	c, err := srv.workspaceFromArgs(map[string]any{"workspace": upper})
	require.NoError(t, err)
	assert.Equal(t, ws, c)
}
