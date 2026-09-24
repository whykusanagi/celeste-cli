// cmd/celeste/tools/mcp/adapter.go
package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// maxToolNameLen is the longest tool name providers accept
// (OpenAI-compatible APIs: ^[a-zA-Z0-9_-]{1,64}$).
const maxToolNameLen = 64

// ToolName returns the registry name for an MCP server's tool,
// mcp__<server>__<tool>. MCP tools used to register under the server's bare
// name, which let a server replace a built-in tool (and inherit its allow
// rules) or clobber another server's tool (#187).
func ToolName(server, tool string) string {
	name := "mcp__" + sanitizeNamePart(server) + "__" + sanitizeNamePart(tool)
	if len(name) <= maxToolNameLen {
		return name
	}
	// Too long: keep a readable prefix and make it unique with a hash.
	sum := sha256.Sum256([]byte(server + "\x00" + tool))
	suffix := "_" + hex.EncodeToString(sum[:])[:8]
	return name[:maxToolNameLen-len(suffix)] + suffix
}

// sanitizeNamePart maps anything outside [A-Za-z0-9_-] to '_'.
func sanitizeNamePart(s string) string {
	if s == "" {
		return "_"
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// MCPTool wraps an MCP tool definition and implements the tools.Tool interface.
// It delegates execution to the MCP Client, bridging external MCP servers
// into celeste's unified tool system.
type MCPTool struct {
	def        MCPToolDef
	client     *Client
	serverName string
	name       string // namespaced registry name; def.Name is what the server knows
}

// NewMCPTool creates a new MCPTool adapter for the given MCP tool definition.
func NewMCPTool(def MCPToolDef, client *Client, serverName string) *MCPTool {
	return &MCPTool{
		def:        def,
		client:     client,
		serverName: serverName,
		name:       ToolName(serverName, def.Name),
	}
}

func (m *MCPTool) Name() string {
	return m.name
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
// The server's readOnlyHint is deliberately not trusted here: it is the
// server's own unverified claim, and IsReadOnly auto-approves in default mode.
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
			ToolName: m.name,
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
