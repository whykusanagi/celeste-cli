package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Aikido 806869793: a workspace .mcp.json that fails to load is reported
// with its error escaped. The decoder copies the offending key, file
// content, into the error.
func TestMCPList_ErrorsAreTerminalSafe(t *testing.T) {
	home, ws := t.TempDir(), t.TempDir()
	u := func(hex string) string { return `\` + "u" + hex } // a JSON \uXXXX escape
	key := "x" + u("001b") + "]0;t" + u("0007") + u("009b") + "2J"
	writeMCPConfig(t, filepath.Join(ws, ".mcp.json"), `{"mcpServers":{"`+key+`":{"command":5}}}`)
	code, _, errOut := runMCPList(t, nil, ws, home)
	assert.Equal(t, 1, code)
	assert.NotEmpty(t, errOut)
	for _, bad := range []string{"\x1b", "\x07", "\xc2\x9b"} {
		assert.False(t, strings.Contains(errOut, bad), "stderr keeps %q: %q", bad, errOut)
	}
}

// `celeste mcp trust` reports a workspace config that fails to load with
// the error escaped, as `celeste mcp list` does.
func TestMCPTrust_ErrorsAreTerminalSafe(t *testing.T) {
	home, ws := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	u := func(hex string) string { return `\` + "u" + hex }
	key := "x" + u("001b") + "]0;t" + u("0007")
	writeMCPConfig(t, filepath.Join(ws, ".mcp.json"), `{"mcpServers":{"`+key+`":{"command":5}}}`)
	var out, errOut bytes.Buffer
	c := hooksCLI{cwd: ws, home: home, in: strings.NewReader(""), out: &out, errOut: &errOut}
	assert.Equal(t, 1, mcpTrustCommand([]string{"trust", "repo"}, c))
	assert.NotEmpty(t, errOut.String())
	for _, bad := range []string{"\x1b", "\x07"} {
		assert.False(t, strings.Contains(out.String()+errOut.String(), bad), "output keeps %q: %q", bad, errOut.String())
	}
}

// `celeste hooks list` names a workspace MCP config that fails to load
// with the error escaped.
func TestHooksList_MCPWarningIsTerminalSafe(t *testing.T) {
	home, ws := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	u := func(hex string) string { return `\` + "u" + hex }
	writeMCPConfig(t, filepath.Join(ws, ".mcp.json"), `{"mcpServers":{"x`+u("001b")+`]0;t`+u("0007")+`":{"command":5}}}`)
	var out, errOut bytes.Buffer
	hooksList(hooksCLI{cwd: ws, home: home, in: strings.NewReader(""), out: &out, errOut: &errOut})
	assert.Contains(t, errOut.String(), "mcp: skipping")
	assert.False(t, strings.ContainsAny(out.String()+errOut.String(), "\x1b\x07"), "output keeps a control: %q", errOut.String())
}
