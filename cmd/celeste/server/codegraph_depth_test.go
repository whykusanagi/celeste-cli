package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// #399: celeste_code_graph's depth reaches callers of callers over MCP.
func TestCelesteCodeGraph_DepthOverMCP(t *testing.T) {
	srv, ws := newTestServerWithWorkspace(t)
	writeTSFile(t, ws, "go.mod", "module example.com/m\n\ngo 1.22\n")
	writeTSFile(t, ws, "main.go", "package main\n\nfunc main() { middle() }\n\nfunc middle() { helper() }\n\nfunc helper() {}\n")
	_, payload := callTool(t, srv, "celeste_index", map[string]any{"operation": "rebuild"})
	require.NotEqual(t, true, payload["isError"], payloadText(t, payload))

	_, payload = callTool(t, srv, "celeste_code_graph", map[string]any{"symbol": "helper", "direction": "callers", "depth": 1})
	d1 := payloadText(t, payload)
	assert.Contains(t, d1, "<- middle (calls)")
	assert.NotContains(t, d1, "<- main")

	_, payload = callTool(t, srv, "celeste_code_graph", map[string]any{"symbol": "helper", "direction": "callers", "depth": 2})
	assert.Contains(t, payloadText(t, payload), "<- main (calls) main.go:3 [hop 2, via middle]")
}
