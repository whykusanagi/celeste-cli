//go:build !windows

package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSetServerEnabledTightensConfig: rewriting a world-readable MCP config
// leaves it readable only by the owner, as its env values can be
// credentials (CodeRabbit review of #427).
func TestSetServerEnabledTightensConfig(t *testing.T) {
	p := filepath.Join(t.TempDir(), "mcp.json")
	require.NoError(t, os.WriteFile(p, []byte(`{"mcpServers":{"x":{"command":"npx","env":{"T":"v"}}}}`), 0o644))
	require.NoError(t, os.Chmod(p, 0o644))
	require.NoError(t, SetServerEnabled(p, "x", true))
	fi, err := os.Stat(p)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
}

// TestSetServerEnabledDoesNotFollowASymlinkSwappedIn: a config swapped for
// a symlink after it was read is replaced, not written through or
// chmod'd (codex review of this branch).
func TestSetServerEnabledDoesNotFollowASymlinkSwappedIn(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "mcp.json")
	require.NoError(t, os.WriteFile(p, []byte(`{"mcpServers":{"x":{"command":"npx"}}}`), 0o600))
	victim := filepath.Join(dir, "victim")
	require.NoError(t, os.WriteFile(victim, []byte("keep"), 0o644))
	require.NoError(t, os.Chmod(victim, 0o644))
	testHookConfigLoaded = func() {
		require.NoError(t, os.Remove(p))
		require.NoError(t, os.Symlink(victim, p))
	}
	t.Cleanup(func() { testHookConfigLoaded = nil })
	require.NoError(t, SetServerEnabled(p, "x", true))

	got, err := os.ReadFile(victim)
	require.NoError(t, err)
	fi, err := os.Stat(victim)
	require.NoError(t, err)
	assert.Equal(t, "keep", string(got))
	assert.Equal(t, os.FileMode(0o644), fi.Mode().Perm())
	ci, err := os.Lstat(p)
	require.NoError(t, err)
	assert.True(t, ci.Mode().IsRegular(), "config is %v", ci.Mode())
	assert.Equal(t, os.FileMode(0o600), ci.Mode().Perm())
}

// TestSetServerEnabledKeepsASymlinkedConfig: a config that is a symlink
// (a dotfile manager's) is rewritten at its target; the link stays.
func TestSetServerEnabledKeepsASymlinkedConfig(t *testing.T) {
	dir := t.TempDir()
	tracked := filepath.Join(dir, "tracked.json")
	require.NoError(t, os.WriteFile(tracked, []byte(`{"mcpServers":{"x":{"command":"npx"}}}`), 0o644))
	p := filepath.Join(dir, "mcp.json")
	require.NoError(t, os.Symlink(tracked, p))
	require.NoError(t, SetServerEnabled(p, "x", true))

	li, err := os.Lstat(p)
	require.NoError(t, err)
	assert.NotZero(t, li.Mode()&os.ModeSymlink, "the link stays")
	cfg, err := LoadConfig(tracked)
	require.NoError(t, err)
	assert.True(t, cfg.Servers["x"].Enabled)
	fi, err := os.Stat(tracked)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
}
