package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeMCPConfig(t *testing.T, path, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
}

// mcpListLine returns the output line that lists server name from source.
func mcpListLine(t *testing.T, out, name, source string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && f[0] == name && f[1] == source {
			return line
		}
	}
	t.Fatalf("no line for %s from %s in:\n%s", name, source, out)
	return ""
}

func runMCPList(t *testing.T, args []string, cwd, home string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := mcpListCommand(args, cwd, home, &out, &errOut)
	return code, out.String(), errOut.String()
}

// #354: celeste mcp list shows every configured server with its source,
// transport, enabled and trusted flags and where it runs, and never prints
// a server's env, args, command or URL.
func TestMCPList_ShowsServersWithoutSecrets(t *testing.T) {
	home, ws := t.TempDir(), t.TempDir()
	writeMCPConfig(t, filepath.Join(home, ".celeste", "mcp.json"), `{"mcpServers":{
		"notes":{"command":"notes-mcp","args":["--token","ARG-SECRET"],"env":{"API_KEY":"ENV-SECRET"},"enabled":true,"trusted":true},
		"remote":{"transport":"sse","url":"https://user:URL-SECRET@example.com/sse?key=Q-SECRET"},
		"dup":{"command":"older"},
		"shared":{"command":"shared-home"}}}`)
	writeMCPConfig(t, filepath.Join(home, ".claude", "mcp.json"), `{"mcpServers":{"dup":{"command":"newer"}}}`)
	writeMCPConfig(t, filepath.Join(ws, ".mcp.json"), `{"mcpServers":{
		"repo":{"command":"repo-mcp","enabled":true,"trusted":true},
		"shared":{"command":"shared-repo","enabled":true}}}`)

	code, out, errOut := runMCPList(t, nil, ws, home)
	require.Equal(t, 0, code, errOut)

	for _, secret := range []string{"ARG-SECRET", "ENV-SECRET", "API_KEY", "URL-SECRET", "Q-SECRET", "notes-mcp", "repo-mcp", "example.com"} {
		assert.NotContains(t, out+errOut, secret)
	}
	assert.Contains(t, out, "NAME")
	assert.Contains(t, out, "TRANSPORT")

	home1 := filepath.Join("~", ".celeste", "mcp.json")
	home2 := filepath.Join("~", ".claude", "mcp.json")
	wsFile := filepath.Join(".", ".mcp.json")

	notes := strings.Fields(mcpListLine(t, out, "notes", home1))
	assert.Equal(t, []string{"notes", home1, "stdio", "yes", "yes", "all", "modes"}, notes)

	remote := strings.Fields(mcpListLine(t, out, "remote", home1))
	assert.Equal(t, []string{"remote", home1, "sse", "no", "no", "all", "modes"}, remote)

	// Later home configs win on a name clash (DiscoverConfigPaths order).
	assert.Contains(t, mcpListLine(t, out, "dup", home1), "overridden by "+home2)
	assert.Contains(t, mcpListLine(t, out, "dup", home2), "all modes")

	// A workspace server: chat only, and its "trusted" is not honoured.
	repo := strings.Fields(mcpListLine(t, out, "repo", wsFile))
	assert.Equal(t, []string{"repo", wsFile, "stdio", "yes", "ignored", "chat", "only"}, repo)

	// A home server the workspace redefines: the chat uses the workspace's.
	shared := mcpListLine(t, out, "shared", home1)
	assert.Contains(t, shared, "all but chat (chat uses")
	assert.Contains(t, shared, wsFile)
	assert.Contains(t, mcpListLine(t, out, "shared", wsFile), "chat only")
}

func TestMCPList_NoServers(t *testing.T) {
	code, out, _ := runMCPList(t, nil, t.TempDir(), t.TempDir())
	assert.Equal(t, 0, code)
	assert.Contains(t, out, "No MCP servers configured")
}

// A config that does not parse is reported; the others are still listed,
// and the command fails.
func TestMCPList_BadConfigReportedOthersListed(t *testing.T) {
	home, ws := t.TempDir(), t.TempDir()
	writeMCPConfig(t, filepath.Join(home, ".celeste", "mcp.json"), `{"mcpServers":{"ok":{"command":"x"}}}`)
	writeMCPConfig(t, filepath.Join(ws, ".mcp.json"), `{not json`)

	code, out, errOut := runMCPList(t, nil, ws, home)
	assert.Equal(t, 1, code)
	mcpListLine(t, out, "ok", filepath.Join("~", ".celeste", "mcp.json"))
	assert.Contains(t, errOut, ".mcp.json")
}

// A server name is file content: control characters are shown escaped.
func TestMCPList_QuotesControlCharacters(t *testing.T) {
	home := t.TempDir()
	writeMCPConfig(t, filepath.Join(home, ".celeste", "mcp.json"), `{"mcpServers":{"evil\u001b[2J":{"command":"x"}}}`)
	code, out, _ := runMCPList(t, nil, t.TempDir(), home)
	assert.Equal(t, 0, code)
	assert.NotContains(t, out, "\x1b")
	assert.Contains(t, out, `"evil\x1b[2J"`)
}

func TestMCPList_HelpAndArgs(t *testing.T) {
	for _, h := range []string{"-h", "--help", "-help"} {
		code, out, _ := runMCPList(t, []string{h}, t.TempDir(), t.TempDir())
		assert.Equal(t, 0, code)
		assert.Contains(t, out, "Usage: celeste mcp list")
	}
	code, _, errOut := runMCPList(t, []string{"extra"}, t.TempDir(), t.TempDir())
	assert.Equal(t, 2, code)
	assert.Contains(t, errOut, "Usage: celeste mcp list")
}

// The mcp usage names both subcommands.
func TestMCPUsage_NamesList(t *testing.T) {
	assert.Contains(t, subcommandUsage["mcp"], "celeste mcp list")
	assert.Contains(t, subcommandUsage["mcp"], "celeste mcp install")
}
