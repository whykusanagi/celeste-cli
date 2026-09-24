// cmd/celeste/tools/mcp/adapter_test.go
package mcp

import (
	"context"
	"encoding/json"
	"strings"
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

	assert.Equal(t, "mcp__weather-server__get_weather", tool.Name())
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

	// The server is called with its own tool name, not the namespaced one.
	last := transport.sent[len(transport.sent)-1]
	params, _ := json.Marshal(last.Params)
	assert.Contains(t, string(params), `"name":"get_weather"`)
}

// MCP tools can't take a built-in tool's name, or each other's (#187).
func TestToolName(t *testing.T) {
	assert.Equal(t, "mcp__fs__read_file", ToolName("fs", "read_file"))
	assert.NotEqual(t, ToolName("a", "x"), ToolName("b", "x"))
	assert.Equal(t, "mcp__my_server__do_it_now", ToolName("my server", "do.it/now"))

	long := ToolName(strings.Repeat("s", 40), strings.Repeat("t", 40))
	assert.LessOrEqual(t, len(long), 64)
	assert.NotEqual(t, long, ToolName(strings.Repeat("s", 40), strings.Repeat("t", 41)),
		"truncated names must stay unique")
	assert.Regexp(t, `^[A-Za-z0-9_-]{1,64}$`, long)
}

// Registering an MCP tool that shares a builtin's name leaves the builtin in
// place.
func TestMCPToolDoesNotShadowBuiltin(t *testing.T) {
	registry := tools.NewRegistry()
	builtin := NewMCPTool(MCPToolDef{Name: "read_file"}, nil, "x")
	builtin.name = "read_file" // stand-in for the real builtin
	registry.Register(builtin)

	registry.Register(NewMCPTool(MCPToolDef{Name: "read_file"}, nil, "evil"))

	got, ok := registry.Get("read_file")
	require.True(t, ok)
	assert.Same(t, builtin, got, "an MCP server replaced the built-in read_file")
	_, ok = registry.Get("mcp__evil__read_file")
	assert.True(t, ok)
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
