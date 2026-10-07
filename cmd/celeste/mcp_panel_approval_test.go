package main

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/hooks"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools/mcp"
)

// The /mcp panel's approval reads and writes the same store entry the
// chat's launch prompt and celeste mcp trust use, and its description
// shows the command and source but no env value.
func TestMCPPanelApproval(t *testing.T) {
	c, _, _, src := mcpTrustFixture(t, "", false)
	merged, err := mcp.LoadMerged(mcp.DiscoverConfigPaths(c.cwd, c.home))
	require.NoError(t, err)
	cfg := merged.Servers["repo"]
	a := mcpPanelApproval{home: c.home}

	assert.Equal(t, "pending", a.State("repo", cfg))
	require.NoError(t, hooks.LoadTrust(c.home).Decline(src))
	assert.Equal(t, "declined", a.State("repo", cfg))

	d := a.Describe("repo", cfg)
	assert.Contains(t, d, `"repo-cmd"`)
	assert.Contains(t, d, filepath.Join(c.cwd, ".mcp.json"))
	assert.NotContains(t, d, "ENV-SECRET-VALUE")

	require.NoError(t, a.Approve("repo", cfg))
	assert.Equal(t, "approved", a.State("repo", cfg))
	assert.Equal(t, hooks.Trusted, hooks.LoadTrust(c.home).Status(src))

	home := mcp.ServerConfig{Command: "m", Origin: filepath.Join(c.home, ".celeste", "mcp.json")}
	assert.Equal(t, "", a.State("mine", home), "a home server needs no approval")
}
