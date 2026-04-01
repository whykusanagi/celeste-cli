// cmd/celeste/tools/mcp/manager_test.go
package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// makeInitResponse creates a mock initialize response.
func makeInitResponse() *Response {
	result, _ := json.Marshal(initializeResult{
		ProtocolVersion: protocolVersion,
		Capabilities:    map[string]any{},
		ServerInfo:      serverInfo{Name: "test-server", Version: "1.0"},
	})
	return &Response{JSONRPC: "2.0", Result: result}
}

// makeToolsListResponse creates a mock tools/list response with n tools.
func makeToolsListResponse(names ...string) *Response {
	var defs []MCPToolDef
	for _, name := range names {
		schema, _ := json.Marshal(map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		})
		defs = append(defs, MCPToolDef{
			Name:        name,
			Description: "Test tool " + name,
			InputSchema: schema,
		})
	}
	result, _ := json.Marshal(toolsListResult{Tools: defs})
	return &Response{JSONRPC: "2.0", Result: result}
}

func TestManager_StartNoConfig(t *testing.T) {
	registry := tools.NewRegistry()
	manager := NewManager("/nonexistent/path/mcp.json", registry)

	err := manager.Start(context.Background())
	require.NoError(t, err, "Start should succeed when config file doesn't exist")

	status := manager.ServerStatus()
	assert.Empty(t, status, "No servers should be connected")
}

func TestManager_StartEmptyConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "mcp.json")
	err := os.WriteFile(configPath, []byte(`{"mcpServers": {}}`), 0644)
	require.NoError(t, err)

	registry := tools.NewRegistry()
	manager := NewManager(configPath, registry)

	err = manager.Start(context.Background())
	require.NoError(t, err)

	status := manager.ServerStatus()
	assert.Empty(t, status)
}

func TestManager_Stop(t *testing.T) {
	registry := tools.NewRegistry()
	manager := NewManager("", registry)

	// Manually add a mock client to test Stop
	mt := &mockTransport{
		responses: []*Response{
			makeInitResponse(),
			makeToolsListResponse("tool1"),
		},
	}
	client := NewClient(mt, "celeste", "1.0")
	err := client.Initialize(context.Background())
	require.NoError(t, err)

	manager.mu.Lock()
	manager.clients["test"] = client
	manager.toolCounts["test"] = 1
	manager.transports["test"] = "stdio"
	manager.mu.Unlock()

	// Verify server status before stop
	status := manager.ServerStatus()
	require.Len(t, status, 1)
	assert.Equal(t, "test", status[0].Name)
	assert.True(t, status[0].Connected)
	assert.Equal(t, 1, status[0].ToolCount)
	assert.Equal(t, "stdio", status[0].Transport)

	// Stop should close all clients
	err = manager.Stop()
	require.NoError(t, err)
	assert.True(t, mt.closed, "transport should be closed after Stop")

	status = manager.ServerStatus()
	assert.Empty(t, status, "no servers after Stop")
}

func TestManager_ServerStatus(t *testing.T) {
	registry := tools.NewRegistry()
	manager := NewManager("", registry)

	// Empty manager
	status := manager.ServerStatus()
	assert.Empty(t, status)

	// Add two mock clients
	manager.mu.Lock()
	manager.clients["server-a"] = &Client{}
	manager.toolCounts["server-a"] = 3
	manager.transports["server-a"] = "stdio"
	manager.clients["server-b"] = &Client{}
	manager.toolCounts["server-b"] = 1
	manager.transports["server-b"] = "sse"
	manager.mu.Unlock()

	status = manager.ServerStatus()
	assert.Len(t, status, 2)

	// Check both servers are present (order not guaranteed)
	names := map[string]bool{}
	for _, s := range status {
		names[s.Name] = true
		assert.True(t, s.Connected)
	}
	assert.True(t, names["server-a"])
	assert.True(t, names["server-b"])
}

func TestManager_CreateTransport_UnknownType(t *testing.T) {
	manager := &Manager{}
	_, err := manager.createTransport(ServerConfig{Transport: "grpc"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown transport type")
}

func TestManager_CreateTransport_StdioMissingCommand(t *testing.T) {
	manager := &Manager{}
	_, err := manager.createTransport(ServerConfig{Transport: "stdio"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "requires a command")
}

func TestManager_CreateTransport_SSEMissingURL(t *testing.T) {
	manager := &Manager{}
	_, err := manager.createTransport(ServerConfig{Transport: "sse"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "requires a URL")
}

func TestManager_InvalidConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "mcp.json")
	err := os.WriteFile(configPath, []byte(`{invalid json`), 0644)
	require.NoError(t, err)

	registry := tools.NewRegistry()
	manager := NewManager(configPath, registry)

	err = manager.Start(context.Background())
	assert.Error(t, err, "Start should fail with invalid config JSON")
}
