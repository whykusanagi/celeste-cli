package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/hooks"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools/mcp"
)

const repoServerJSON = `{"mcpServers":{"repo":{"command":"repo-cmd","args":["--flag","x"],"env":{"TOKEN":"ENV-SECRET-VALUE"},"enabled":true}}}`

// mcpTrustFixture is a workspace whose .mcp.json defines "repo".
func mcpTrustFixture(t *testing.T, in string, interactive bool) (hooksCLI, *bytes.Buffer, *bytes.Buffer, hooks.Source) {
	t.Helper()
	home, ws := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	p := filepath.Join(ws, ".mcp.json")
	writeMCPConfig(t, p, repoServerJSON)
	cfg, err := mcp.LoadConfig(p)
	require.NoError(t, err)
	sc := cfg.Servers["repo"]
	var out, errOut bytes.Buffer
	c := hooksCLI{cwd: ws, home: home, in: strings.NewReader(in), out: &out, errOut: &errOut, interactive: interactive}
	return c, &out, &errOut, hooks.MCPSource(p, "repo", sc.TrustSummary(), sc.TrustHash())
}

// #411: `celeste mcp trust` overrides a decline, but only after showing
// the command, args and source file and getting an explicit yes.
func TestMCPTrust_ConfirmsThenApprovesADecline(t *testing.T) {
	c, out, errOut, src := mcpTrustFixture(t, "y\n", true)
	require.NoError(t, hooks.LoadTrust(c.home).Decline(src))

	code := mcpTrustCommand([]string{"trust", "repo"}, c)
	require.Equal(t, 0, code, errOut.String())
	s := out.String()
	for _, want := range []string{`"repo-cmd"`, `"--flag" "x"`, ".mcp.json", "declined", "[y/N]"} {
		assert.Contains(t, s, want)
	}
	assert.NotContains(t, s+errOut.String(), "ENV-SECRET-VALUE", "env values are never printed")
	assert.Equal(t, hooks.Trusted, hooks.LoadTrust(c.home).Status(src))
}

func TestMCPTrust_NoKeepsTheDecline(t *testing.T) {
	for _, in := range []string{"n\n", "\n", ""} {
		c, _, _, src := mcpTrustFixture(t, in, true)
		require.NoError(t, hooks.LoadTrust(c.home).Decline(src))
		assert.Equal(t, 1, mcpTrustCommand([]string{"trust", "repo"}, c), "answer %q", in)
		assert.Equal(t, hooks.Declined, hooks.LoadTrust(c.home).Status(src), "answer %q", in)
	}
}

func TestMCPTrust_RefusesWithoutTTYUnlessYes(t *testing.T) {
	c, _, errOut, src := mcpTrustFixture(t, "y\n", false)
	require.NoError(t, hooks.LoadTrust(c.home).Decline(src))
	assert.Equal(t, 1, mcpTrustCommand([]string{"trust", "repo"}, c))
	assert.Contains(t, errOut.String(), "--yes")
	assert.Equal(t, hooks.Declined, hooks.LoadTrust(c.home).Status(src), "a piped y is not a confirmation")

	c.out.(*bytes.Buffer).Reset()
	assert.Equal(t, 0, mcpTrustCommand([]string{"trust", "--yes", "repo"}, c))
	assert.Contains(t, c.out.(*bytes.Buffer).String(), `"repo-cmd"`, "--yes still shows what it approves")
	assert.Equal(t, hooks.Trusted, hooks.LoadTrust(c.home).Status(src))
}

func TestMCPTrust_AlreadyApproved(t *testing.T) {
	c, out, _, src := mcpTrustFixture(t, "", false)
	require.NoError(t, hooks.LoadTrust(c.home).Approve(src))
	assert.Equal(t, 0, mcpTrustCommand([]string{"trust", "repo"}, c))
	assert.Contains(t, out.String(), "already approved")
}

func TestMCPTrust_UnknownAndHomeServers(t *testing.T) {
	c, _, errOut, _ := mcpTrustFixture(t, "", false)
	assert.Equal(t, 1, mcpTrustCommand([]string{"trust", "--yes", "nope"}, c))
	assert.Contains(t, errOut.String(), "no MCP server")

	writeMCPConfig(t, filepath.Join(c.home, ".celeste", "mcp.json"), `{"mcpServers":{"mine":{"command":"m"}}}`)
	errOut.Reset()
	assert.Equal(t, 1, mcpTrustCommand([]string{"trust", "--yes", "mine"}, c))
	assert.Contains(t, errOut.String(), "needs no approval")
}

func TestMCPTrust_Usage(t *testing.T) {
	c, _, errOut, _ := mcpTrustFixture(t, "", false)
	assert.Equal(t, 2, mcpTrustCommand([]string{"trust"}, c))
	assert.Equal(t, 2, mcpTrustCommand([]string{"trust", "a", "b"}, c))
	assert.Equal(t, 2, mcpTrustCommand([]string{"trust", "--bogus", "a"}, c))
	assert.Equal(t, 2, mcpTrustCommand([]string{"untrust", "--yes", "a"}, c))
	assert.Contains(t, errOut.String(), "celeste mcp trust")
	assert.Contains(t, subcommandUsage["mcp"], "celeste mcp trust [--yes] <server>")
	assert.Contains(t, subcommandUsage["mcp"], "celeste mcp untrust <server>")
}

// untrust forgets the decision either way, so the chat asks again.
func TestMCPUntrust_ForgetsDecision(t *testing.T) {
	for _, decide := range []func(*hooks.TrustStore, hooks.Source) error{(*hooks.TrustStore).Approve, (*hooks.TrustStore).Decline} {
		c, out, errOut, src := mcpTrustFixture(t, "", false)
		require.NoError(t, decide(hooks.LoadTrust(c.home), src))
		require.Equal(t, 0, mcpTrustCommand([]string{"untrust", "repo"}, c), errOut.String())
		assert.Equal(t, hooks.Untrusted, hooks.LoadTrust(c.home).Status(src))
		assert.Contains(t, out.String(), "Forgot")

		out.Reset()
		require.Equal(t, 0, mcpTrustCommand([]string{"untrust", "repo"}, c))
		assert.Contains(t, out.String(), "No approval or decline")
	}
}

// A server removed from the config can still be forgotten.
func TestMCPUntrust_RemovedServer(t *testing.T) {
	c, out, _, src := mcpTrustFixture(t, "", false)
	require.NoError(t, hooks.LoadTrust(c.home).Decline(src))
	writeMCPConfig(t, filepath.Join(c.cwd, ".mcp.json"), `{"mcpServers":{}}`)
	require.Equal(t, 0, mcpTrustCommand([]string{"untrust", "repo"}, c))
	assert.Contains(t, out.String(), "Forgot")
	assert.Equal(t, hooks.Untrusted, hooks.LoadTrust(c.home).Status(src))
}

// #411 (4): mcp list shows approved / declined / pending, and a server
// changed since its decline as pending.
func TestMCPList_ShowsDecisions(t *testing.T) {
	c, _, _, src := mcpTrustFixture(t, "", false)
	wsFile := filepath.Join(".", ".mcp.json")
	approval := func() []string {
		code, out, errOut := runMCPList(t, nil, c.cwd, c.home)
		require.Equal(t, 0, code, errOut)
		return strings.Fields(mcpListLine(t, out, "repo", wsFile))
	}
	assert.Equal(t, "pending", approval()[5])

	require.NoError(t, hooks.LoadTrust(c.home).Decline(src))
	f := approval()
	assert.Equal(t, "declined", f[5])
	assert.Equal(t, []string{"never", "(declined)"}, f[6:])

	require.NoError(t, hooks.LoadTrust(c.home).Approve(src))
	assert.Equal(t, "approved", approval()[5])

	require.NoError(t, hooks.LoadTrust(c.home).Decline(src))
	writeMCPConfig(t, filepath.Join(c.cwd, ".mcp.json"), strings.Replace(repoServerJSON, "repo-cmd", "other-cmd", 1))
	f = approval()
	assert.Equal(t, []string{"pending", "(changed", "since", "declined)"}, f[5:9])
}
