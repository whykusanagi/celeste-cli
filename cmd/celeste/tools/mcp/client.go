// cmd/celeste/tools/mcp/client.go
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// preferredProtocolVersion is the MCP revision celeste proposes in the
// initialize handshake. Per the MCP spec the server may respond with a
// different version it supports; the client then either adopts that version
// (if known) or disconnects.
const preferredProtocolVersion = "2025-06-18"

// supportedProtocolVersions lists every MCP revision this client can speak.
// celeste only relies on initialize, tools/list, and tools/call, whose
// payloads are unchanged across these revisions, so each negotiates cleanly.
// Ordered newest-first for a readable mismatch error.
var supportedProtocolVersions = []string{
	"2025-06-18",
	"2025-03-26",
	"2024-11-05",
}

func isSupportedProtocolVersion(v string) bool {
	return slices.Contains(supportedProtocolVersions, v)
}

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
	serverProto string
	initialized bool
	mu          sync.Mutex // guards serverName, serverProto, initialized

	// turn is a 1-slot semaphore: the request/response exchange in
	// progress holds it. A call waiting for it gives up when its context
	// ends (2.0 F2e), which a mutex could not do. Made on first use, so a
	// zero Client works.
	turn     chan struct{}
	turnOnce sync.Once

	// inflight is a Receive still running for a call that was cancelled; the
	// next call takes it over, so at most one Receive per client is ever
	// outstanding. Guarded by turn.
	inflight chan received

	// abandoned holds the IDs of calls that gave up (cancelled, or failed on
	// a Receive error) and whose answer hasn't arrived yet. While any are
	// outstanding, a response with no ID can't be told apart from their late
	// answer and is dropped. Guarded by turn.
	abandoned map[string]struct{}
}

// acquire takes the client's turn for one exchange with the server, or
// returns ctx's error if ctx ends first. release gives it back.
func (c *Client) acquire(ctx context.Context) error {
	c.turnOnce.Do(func() { c.turn = make(chan struct{}, 1) })
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case c.turn <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Client) release() { <-c.turn }

// isInitialized reports whether Initialize has succeeded.
func (c *Client) isInitialized() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.initialized
}

// NewClient creates a new MCP client over the given transport.
func NewClient(transport Transport, clientName, clientVersion string) *Client {
	return &Client{
		transport:  transport,
		clientName: clientName,
		clientVer:  clientVersion,
	}
}

// received is one Transport.Receive result.
type received struct {
	resp *Response
	err  error
}

// contextSender is implemented by transports whose sends can be cut short by
// a context (HTTP and SSE). Stdio writes a line to a pipe and doesn't need it.
type contextSender interface {
	SendContext(ctx context.Context, req *Request) error
	SendNotificationContext(ctx context.Context, notif *Notification) error
}

// send sends req, honouring ctx when the transport can (#221). Streamable
// HTTP runs the whole call inside its POST, so this is where it gets cut off.
func (c *Client) send(ctx context.Context, req *Request) error {
	if cs, ok := c.transport.(contextSender); ok {
		return cs.SendContext(ctx, req)
	}
	return c.transport.Send(req)
}

func (c *Client) sendNotification(ctx context.Context, notif *Notification) error {
	if cs, ok := c.transport.(contextSender); ok {
		return cs.SendNotificationContext(ctx, notif)
	}
	return c.transport.SendNotification(notif)
}

// recv waits for the response to request id, honouring ctx (#221).
//
// Transport.Receive can't be interrupted, so it runs on a goroutine. On
// cancel, that Receive is left in c.inflight for the next call to take over.
// Request IDs are unique per process, so any response carrying another ID is
// the late answer to a call that gave up (cancelled, or failed on a Receive
// error) and is dropped. Messages that are not responses (a notification or
// a server-to-client request carries neither result nor error) are skipped.
//
// A response with no ID (id:null, sent when the server couldn't read the
// request's ID) is returned to the current call only while no abandoned call
// is still unanswered; otherwise it may belong to one of those and is
// dropped, and the current call waits on its ctx instead.
// The caller holds the client's turn (acquire).
func (c *Client) recv(ctx context.Context, id int64) (*Response, error) {
	want := strconv.FormatInt(id, 10)
	for {
		if c.inflight == nil {
			ch := make(chan received, 1)
			go func() {
				resp, err := c.transport.Receive()
				ch <- received{resp, err}
			}()
			c.inflight = ch
		}
		select {
		case <-ctx.Done():
			c.abandon(want)
			return nil, ctx.Err()
		case r := <-c.inflight:
			c.inflight = nil
			if r.err != nil {
				c.abandon(want)
				return nil, r.err
			}
			if r.resp == nil || (r.resp.Result == nil && r.resp.Error == nil) {
				continue // a notification or server request, not an answer
			}
			key := r.resp.ID.String()
			if key == "" && len(c.abandoned) > 0 {
				continue // maybe the late parse error for a call that gave up
			}
			if key != "" && key != want {
				delete(c.abandoned, key)
				continue // the late answer to a call that gave up
			}
			return r.resp, nil
		}
	}
}

// abandon records that the call with ID id gave up before its answer came.
func (c *Client) abandon(id string) {
	if c.abandoned == nil {
		c.abandoned = make(map[string]struct{})
	}
	c.abandoned[id] = struct{}{}
}

// ServerName returns the server's name after initialization.
func (c *Client) ServerName() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.serverName
}

// ProtocolVersion returns the MCP protocol version negotiated with the server
// during Initialize. Empty until Initialize succeeds.
func (c *Client) ProtocolVersion() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.serverProto
}

// Initialize performs the MCP initialize handshake.
// Sends initialize request, validates the server's protocol version,
// then sends notifications/initialized.
func (c *Client) Initialize(ctx context.Context) error {
	if err := c.acquire(ctx); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	defer c.release()

	params := map[string]any{
		"protocolVersion": preferredProtocolVersion,
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

	if err := c.send(ctx, req); err != nil {
		return fmt.Errorf("send initialize: %w", err)
	}

	resp, err := c.recv(ctx, req.ID)
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

	if !isSupportedProtocolVersion(result.ProtocolVersion) {
		return fmt.Errorf("unsupported protocol version: server=%s, client supports %s",
			result.ProtocolVersion, strings.Join(supportedProtocolVersions, ", "))
	}

	c.mu.Lock()
	c.serverName = result.ServerInfo.Name
	c.serverProto = result.ProtocolVersion
	c.initialized = true
	c.mu.Unlock()

	// Send notifications/initialized
	notif := NewNotification("notifications/initialized")
	if err := c.sendNotification(ctx, notif); err != nil {
		return fmt.Errorf("send initialized notification: %w", err)
	}

	return nil
}

// ListTools discovers available tools from the MCP server.
func (c *Client) ListTools(ctx context.Context) ([]MCPToolDef, error) {
	if err := c.acquire(ctx); err != nil {
		return nil, fmt.Errorf("tools/list: %w", err)
	}
	defer c.release()

	if !c.isInitialized() {
		return nil, fmt.Errorf("client not initialized")
	}

	req, err := NewRequest("tools/list", map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("create tools/list request: %w", err)
	}

	if err := c.send(ctx, req); err != nil {
		return nil, fmt.Errorf("send tools/list: %w", err)
	}

	resp, err := c.recv(ctx, req.ID)
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
	if err := c.acquire(ctx); err != nil {
		return "", fmt.Errorf("tools/call: %w", err)
	}
	defer c.release()

	if !c.isInitialized() {
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

	if err := c.send(ctx, req); err != nil {
		return "", fmt.Errorf("send tools/call: %w", err)
	}

	resp, err := c.recv(ctx, req.ID)
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
