package main

import (
	"path/filepath"
	"strconv"
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
	assert.Contains(t, d, strconv.Quote(filepath.Join(c.cwd, ".mcp.json")), "the source is shown quoted")
	assert.NotContains(t, d, "ENV-SECRET-VALUE")

	require.NoError(t, a.Approve("repo", cfg))
	assert.Equal(t, "approved", a.State("repo", cfg))
	assert.Equal(t, hooks.Trusted, hooks.LoadTrust(c.home).Status(src))

	home := mcp.ServerConfig{Command: "m", Origin: filepath.Join(c.home, ".celeste", "mcp.json")}
	assert.Equal(t, "", a.State("mine", home), "a home server needs no approval")
}

// With no home there is no trust store: a workspace server is still one
// that needs approval, and approving it fails instead of connecting it
// unapproved (Aikido review of #413).
func TestMCPPanelApprovalWithoutHomeGatesWorkspaceServers(t *testing.T) {
	a := mcpPanelApproval{home: ""}
	cfg := mcp.ServerConfig{Command: "repo-cmd", Origin: filepath.Join(t.TempDir(), ".mcp.json")}
	assert.Equal(t, "pending", a.State("repo", cfg), "a workspace server without a trust store needs approval")
	assert.NotEmpty(t, a.Describe("repo", cfg))
	require.Error(t, a.Approve("repo", cfg), "an approval with nowhere to record it must not let the server connect")
	assert.Equal(t, "", a.State("none", mcp.ServerConfig{Command: "x"}), "a server from no known file needs no approval")
}
