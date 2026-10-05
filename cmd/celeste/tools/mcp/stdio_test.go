// cmd/celeste/tools/mcp/stdio_test.go
package mcp

import (
	"encoding/json"
	"io"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStdioTransport_SendReceive(t *testing.T) {
	// Use 'cat' as a mock server: it echoes stdin to stdout.
	// We send a JSON-RPC request; cat echoes it back.
	// The echoed request is valid JSON that can be unmarshaled as a Response
	// (it will have no result/error, but the JSON parses).
	transport, err := newStdioTransport("", "cat", nil, nil)
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
	transport, err := newStdioTransport("", "cat", nil, nil)
	require.NoError(t, err)

	err = transport.Close()
	assert.NoError(t, err)

	// Sending after close should fail
	req := &Request{JSONRPC: "2.0", ID: 1, Method: "test"}
	err = transport.Send(req)
	assert.Error(t, err)
}

func TestStdioTransport_SendNotification(t *testing.T) {
	transport, err := newStdioTransport("", "cat", nil, nil)
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

// hungServerEnv makes the test binary act as a stdio MCP server that ignores
// stdin EOF (see TestMCPHungServer).
const hungServerEnv = "CELESTE_MCP_HUNG_SERVER"

// TestMCPHungServer is not a test: run with hungServerEnv set, it drains
// stdin and then keeps running long after EOF, like a hung server.
func TestMCPHungServer(t *testing.T) {
	if os.Getenv(hungServerEnv) != "1" {
		t.Skip("helper process for TestStdioTransport_CloseKillsHungServer")
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
	time.Sleep(5 * time.Minute)
	os.Exit(0)
}

// Close kills a server that doesn't exit on stdin EOF instead of waiting on
// it forever, so a retired chat Env can't freeze the call closing it.
func TestStdioTransport_CloseKillsHungServer(t *testing.T) {
	t.Setenv(hungServerEnv, "1")
	tr, err := newStdioTransport("", os.Args[0], []string{"-test.run=^TestMCPHungServer$"}, nil)
	require.NoError(t, err)

	done := make(chan struct{})
	start := time.Now()
	go func() {
		_ = tr.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		_ = tr.cmd.Process.Kill()
		t.Fatal("Close blocked on a server that ignores stdin EOF")
	}
	require.Less(t, time.Since(start), 10*time.Second)
	require.NotNil(t, tr.cmd.ProcessState, "the server process was not reaped")
	require.False(t, tr.cmd.ProcessState.Success(), "the server should have been killed")
}
