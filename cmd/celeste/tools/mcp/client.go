// cmd/celeste/tools/mcp/client.go
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

const protocolVersion = "2024-11-05"

// MCPToolDef is a tool definition returned by the MCP server.
type MCPToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// initializeResult is the server's response to the initialize request.
type initializeResult struct {
	ProtocolVersion string     `json:"protocolVersion"`
	Capabilities    any        `json:"capabilities"`
	ServerInfo      serverInfo `json:"serverInfo"`
}

// serverInfo is the server's identity.
type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// toolsListResult is the server's response to tools/list.
type toolsListResult struct {
	Tools []MCPToolDef `json:"tools"`
}

// ToolCallResult is the server's response to tools/call.
type ToolCallResult struct {
	Content []ContentBlock `json:"content"`
}

// ContentBlock is a single content item in a tool call response.
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Client is a high-level MCP client that handles the protocol handshake,
// tool discovery, and tool execution over a Transport.
type Client struct {
	transport   Transport
	clientName  string
	clientVer   string
	serverName  string
	initialized bool
	mu          sync.Mutex
}

// NewClient creates a new MCP client over the given transport.
func NewClient(transport Transport, clientName, clientVersion string) *Client {
	return &Client{
		transport:  transport,
		clientName: clientName,
		clientVer:  clientVersion,
	}
}

// ServerName returns the server's name after initialization.
func (c *Client) ServerName() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.serverName
}

// Initialize performs the MCP initialize handshake.
// Sends initialize request, validates the server's protocol version,
// then sends notifications/initialized.
func (c *Client) Initialize(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	params := map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    c.clientName,
			"version": c.clientVer,
		},
	}

	req, err := NewRequest("initialize", params)
	if err != nil {
		return fmt.Errorf("create initialize request: %w", err)
	}

	if err := c.transport.Send(req); err != nil {
		return fmt.Errorf("send initialize: %w", err)
	}

	resp, err := c.transport.Receive()
	if err != nil {
		return fmt.Errorf("receive initialize response: %w", err)
	}

	if resp.Error != nil {
		return fmt.Errorf("initialize error: %w", resp.Error)
	}

	var result initializeResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return fmt.Errorf("unmarshal initialize result: %w", err)
	}

	if result.ProtocolVersion != protocolVersion {
		return fmt.Errorf("protocol version mismatch: server=%s, expected=%s", result.ProtocolVersion, protocolVersion)
	}

	c.serverName = result.ServerInfo.Name
	c.initialized = true

	// Send notifications/initialized
	notif := NewNotification("notifications/initialized")
	if err := c.transport.SendNotification(notif); err != nil {
		return fmt.Errorf("send initialized notification: %w", err)
	}

	return nil
}

// ListTools discovers available tools from the MCP server.
func (c *Client) ListTools(ctx context.Context) ([]MCPToolDef, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.initialized {
		return nil, fmt.Errorf("client not initialized")
	}

	req, err := NewRequest("tools/list", map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("create tools/list request: %w", err)
	}

	if err := c.transport.Send(req); err != nil {
		return nil, fmt.Errorf("send tools/list: %w", err)
	}

	resp, err := c.transport.Receive()
	if err != nil {
		return nil, fmt.Errorf("receive tools/list response: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("tools/list error: %w", resp.Error)
	}

	var result toolsListResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("unmarshal tools/list result: %w", err)
	}

	return result.Tools, nil
}

// CallTool executes a tool on the MCP server and returns the text result.
// Multiple text content blocks are joined with newlines.
func (c *Client) CallTool(ctx context.Context, name string, arguments map[string]any) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.initialized {
		return "", fmt.Errorf("client not initialized")
	}

	params := map[string]any{
		"name":      name,
		"arguments": arguments,
	}

	req, err := NewRequest("tools/call", params)
	if err != nil {
		return "", fmt.Errorf("create tools/call request: %w", err)
	}

	if err := c.transport.Send(req); err != nil {
		return "", fmt.Errorf("send tools/call: %w", err)
	}

	resp, err := c.transport.Receive()
	if err != nil {
		return "", fmt.Errorf("receive tools/call response: %w", err)
	}

	if resp.Error != nil {
		return "", fmt.Errorf("tools/call error: %w", resp.Error)
	}

	var result ToolCallResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return "", fmt.Errorf("unmarshal tools/call result: %w", err)
	}

	// Extract text from content blocks
	var texts []string
	for _, block := range result.Content {
		if block.Type == "text" {
			texts = append(texts, block.Text)
		}
	}

	return strings.Join(texts, "\n"), nil
}

// Close shuts down the client and its transport.
func (c *Client) Close() error {
	return c.transport.Close()
}
