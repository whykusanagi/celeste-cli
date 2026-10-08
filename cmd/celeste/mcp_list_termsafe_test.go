package main

import (
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
