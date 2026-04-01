// cmd/celeste/tools/mcp/stdio_test.go
package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStdioTransport_SendReceive(t *testing.T) {
	// Use 'cat' as a mock server: it echoes stdin to stdout.
	// We send a JSON-RPC request; cat echoes it back.
	// The echoed request is valid JSON that can be unmarshaled as a Response
	// (it will have no result/error, but the JSON parses).
	transport, err := NewStdioTransport("cat", nil, nil)
	require.NoError(t, err)
	defer transport.Close()

	req := &Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "test",
		Params:  json.RawMessage(`{"hello":"world"}`),
	}

	err = transport.Send(req)
	require.NoError(t, err)

	resp, err := transport.Receive()
	require.NoError(t, err)
	// cat echoes the request back as-is, so the JSONRPC field should be "2.0"
	assert.Equal(t, "2.0", resp.JSONRPC)
}

func TestStdioTransport_Close(t *testing.T) {
	transport, err := NewStdioTransport("cat", nil, nil)
	require.NoError(t, err)

	err = transport.Close()
	assert.NoError(t, err)

	// Sending after close should fail
	req := &Request{JSONRPC: "2.0", ID: 1, Method: "test"}
	err = transport.Send(req)
	assert.Error(t, err)
}

func TestStdioTransport_SendNotification(t *testing.T) {
	transport, err := NewStdioTransport("cat", nil, nil)
	require.NoError(t, err)
	defer transport.Close()

	notif := NewNotification("notifications/initialized")
	err = transport.SendNotification(notif)
	assert.NoError(t, err)

	// cat echoes it back; read the line (it will parse as a Response with empty fields)
	resp, err := transport.Receive()
	require.NoError(t, err)
	assert.Equal(t, "2.0", resp.JSONRPC)
}

func TestStdioTransport_EnvExpansion(t *testing.T) {
	// Verify that environment variable expansion works in the env map.
	t.Setenv("MCP_TEST_VAR", "expanded_value")
	env := map[string]string{
		"RESULT": "${MCP_TEST_VAR}",
	}

	expanded := expandEnvVars(env)
	assert.Equal(t, "expanded_value", expanded["RESULT"])
}

func TestExpandEnvVars_NoMatch(t *testing.T) {
	env := map[string]string{
		"KEY": "${NONEXISTENT_MCP_VAR_12345}",
	}
	expanded := expandEnvVars(env)
	// Unset vars expand to empty string
	assert.Equal(t, "", expanded["KEY"])
}
