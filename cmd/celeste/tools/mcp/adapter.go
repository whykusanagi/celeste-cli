// cmd/celeste/tools/mcp/adapter.go
package mcp

import (
	"context"
	"encoding/json"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// MCPTool wraps an MCP tool definition and implements the tools.Tool interface.
// It delegates execution to the MCP Client, bridging external MCP servers
// into celeste's unified tool system.
type MCPTool struct {
	def        MCPToolDef
	client     *Client
	serverName string
}

// NewMCPTool creates a new MCPTool adapter for the given MCP tool definition.
func NewMCPTool(def MCPToolDef, client *Client, serverName string) *MCPTool {
	return &MCPTool{
		def:        def,
		client:     client,
		serverName: serverName,
	}
}

func (m *MCPTool) Name() string {
	return m.def.Name
}

func (m *MCPTool) Description() string {
	return m.def.Description
}

func (m *MCPTool) Parameters() json.RawMessage {
	return m.def.InputSchema
}

// IsConcurrencySafe returns false because MCP tool calls go over a shared
// transport and we cannot guarantee the server handles concurrent calls safely.
func (m *MCPTool) IsConcurrencySafe(input map[string]any) bool {
	return false
}

// IsReadOnly returns false because we cannot know if an MCP tool mutates state.
func (m *MCPTool) IsReadOnly() bool {
	return false
}

// ValidateInput returns nil -- validation is delegated to the MCP server.
func (m *MCPTool) ValidateInput(input map[string]any) error {
	return nil
}

func (m *MCPTool) InterruptBehavior() tools.InterruptBehavior {
	return tools.InterruptCancel
}

// Execute calls the MCP tool via the client and returns the result.
// Server-side errors are returned as ToolResult with Error=true so the
// LLM can see and react to the error message.
func (m *MCPTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	if progress != nil {
		progress <- tools.ProgressEvent{
			ToolName: m.def.Name,
			Message:  "calling MCP server " + m.serverName,
			Percent:  -1,
		}
	}

	result, err := m.client.CallTool(ctx, m.def.Name, input)
	if err != nil {
		// Return the error as a tool result so the LLM can see it
		return tools.ToolResult{
			Content: err.Error(),
			Error:   true,
			Metadata: map[string]any{
				"mcp_server": m.serverName,
				"mcp_tool":   m.def.Name,
			},
		}, nil
	}

	return tools.ToolResult{
		Content: result,
		Error:   false,
		Metadata: map[string]any{
			"mcp_server": m.serverName,
			"mcp_tool":   m.def.Name,
		},
	}, nil
}
