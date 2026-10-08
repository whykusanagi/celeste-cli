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
