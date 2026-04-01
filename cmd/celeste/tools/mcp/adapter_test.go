// cmd/celeste/tools/mcp/adapter_test.go
package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

func TestMCPTool_ImplementsToolInterface(t *testing.T) {
	var _ tools.Tool = &MCPTool{}
}

func TestMCPTool_Properties(t *testing.T) {
	def := MCPToolDef{
		Name:        "get_weather",
		Description: "Get weather for a location",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"location":{"type":"string"}},"required":["location"]}`),
	}

	tool := NewMCPTool(def, nil, "weather-server")

	assert.Equal(t, "get_weather", tool.Name())
	assert.Equal(t, "Get weather for a location", tool.Description())
	assert.False(t, tool.IsConcurrencySafe(nil))
	assert.False(t, tool.IsReadOnly())
	assert.Equal(t, tools.InterruptCancel, tool.InterruptBehavior())

	// Parameters should match inputSchema
	var params map[string]any
	require.NoError(t, json.Unmarshal(tool.Parameters(), &params))
	assert.Equal(t, "object", params["type"])
}

func TestMCPTool_Execute(t *testing.T) {
	// Set up a mock transport that responds to tools/call
	transport := &mockTransport{
		responses: []*Response{
			// Initialize
			{
				JSONRPC: "2.0",
				ID:      json.Number("1"),
				Result:  json.RawMessage(`{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"test"}}`),
			},
			// tools/call
			{
				JSONRPC: "2.0",
				ID:      json.Number("2"),
				Result:  json.RawMessage(`{"content":[{"type":"text","text":"Sunny, 72F in NYC"}]}`),
			},
		},
	}

	client := NewClient(transport, "celeste", "1.7.0")
	require.NoError(t, client.Initialize(context.Background()))

	def := MCPToolDef{
		Name:        "get_weather",
		Description: "Get weather",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"location":{"type":"string"}}}`),
	}

	tool := NewMCPTool(def, client, "weather-server")

	result, err := tool.Execute(context.Background(), map[string]any{"location": "NYC"}, nil)
	require.NoError(t, err)
	assert.Equal(t, "Sunny, 72F in NYC", result.Content)
	assert.False(t, result.Error)
	assert.Equal(t, "weather-server", result.Metadata["mcp_server"])
}

func TestMCPTool_Execute_ServerError(t *testing.T) {
	transport := &mockTransport{
		responses: []*Response{
			{
				JSONRPC: "2.0",
				ID:      json.Number("1"),
				Result:  json.RawMessage(`{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"test"}}`),
			},
			{
				JSONRPC: "2.0",
				ID:      json.Number("2"),
				Error:   &ErrorObject{Code: -32000, Message: "location not found"},
			},
		},
	}

	client := NewClient(transport, "celeste", "1.7.0")
	require.NoError(t, client.Initialize(context.Background()))

	def := MCPToolDef{
		Name:        "get_weather",
		Description: "Get weather",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}

	tool := NewMCPTool(def, client, "weather-server")

	result, err := tool.Execute(context.Background(), map[string]any{"location": "nowhere"}, nil)
	// MCP tool errors are returned as ToolResult.Error=true, not as Go errors,
	// so the caller can display the error message to the LLM.
	require.NoError(t, err)
	assert.True(t, result.Error)
	assert.Contains(t, result.Content, "location not found")
}

func TestMCPTool_ValidateInput(t *testing.T) {
	def := MCPToolDef{
		Name:        "test",
		Description: "test",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}

	tool := NewMCPTool(def, nil, "server")
	// MCPTool delegates validation to the server, so ValidateInput always returns nil
	assert.NoError(t, tool.ValidateInput(nil))
	assert.NoError(t, tool.ValidateInput(map[string]any{"anything": "goes"}))
}
