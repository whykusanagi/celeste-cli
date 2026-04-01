# Plan 5: MCP Client

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add MCP client support for dynamic tool discovery from external servers.

**Architecture:** New `tools/mcp/` sub-package with transport abstraction (stdio/SSE), JSON-RPC 2.0 client, tool discovery, and adapter that converts MCP tools to the unified Tool interface. Servers configured via `~/.celeste/mcp.json`.

**Tech Stack:** Go 1.26, standard library (os/exec for stdio, net/http for SSE), encoding/json for JSON-RPC

**Prerequisite Plans:** Plan 1 (Unified Tool Layer)

---

## Codebase Context

**Existing types from Plan 1 (`cmd/celeste/tools/`):**

1. **`tool.go`** -- `Tool` interface:
   - `Name() string`
   - `Description() string`
   - `Parameters() json.RawMessage`
   - `IsConcurrencySafe(input map[string]any) bool`
   - `IsReadOnly() bool`
   - `ValidateInput(input map[string]any) error`
   - `Execute(ctx context.Context, input map[string]any, progress chan<- ProgressEvent) (ToolResult, error)`
   - `InterruptBehavior() InterruptBehavior`
   - `ToolResult{Content string, Error bool, Metadata map[string]any}`
   - `ProgressEvent{ToolName string, Message string, Percent float64}`
   - `RuntimeMode` constants: `ModeChat`, `ModeClaw`, `ModeAgent`, `ModeOrchestrator`

2. **`registry.go`** -- `Registry`:
   - `NewRegistry() *Registry`
   - `Register(tool Tool)` -- registers for all modes
   - `RegisterWithModes(tool Tool, modes ...RuntimeMode)`
   - `Get(name string) (Tool, bool)`
   - `GetAll() []Tool` -- sorted by name
   - `GetTools(mode RuntimeMode) []Tool`
   - `GetToolDefinitions() []map[string]any`
   - `Execute(ctx context.Context, name string, input map[string]any) (ToolResult, error)`
   - `Count() int`

3. **`builtin/base.go`** -- `BaseTool` struct:
   - Fields: `ToolName`, `ToolDescription`, `ToolParameters`, `ReadOnly`, `ConcurrencySafe`, `Interrupt`, `RequiredFields`
   - Provides default implementations for all `Tool` interface methods

**MCP protocol reference (version "2024-11-05"):**

- Transport: JSON-RPC 2.0 over stdio (stdin/stdout of child process) or SSE (HTTP)
- Initialize: client sends `initialize` with `{protocolVersion, capabilities: {}, clientInfo: {name, version}}`, server responds with `{protocolVersion, capabilities, serverInfo}`
- After initialize, client sends `notifications/initialized` notification (no ID, no response expected)
- Tool discovery: client sends `tools/list` with `{}` params, server responds with `{tools: [{name, description, inputSchema}]}`
- Tool execution: client sends `tools/call` with `{name, arguments}`, server responds with `{content: [{type: "text", text: "..."}]}`
- JSON-RPC 2.0: requests have `jsonrpc: "2.0"`, `id`, `method`, `params`; responses have `jsonrpc: "2.0"`, `id`, `result` or `error`; notifications have no `id`

**Module path:** `github.com/whykusanagi/celeste-cli`

---

## File Structure

```
cmd/celeste/tools/mcp/                # NEW sub-package
├── jsonrpc.go                         # JSON-RPC 2.0 types and helpers
├── jsonrpc_test.go                    # JSON-RPC marshal/unmarshal tests
├── transport.go                       # Transport interface
├── stdio.go                           # StdioTransport implementation
├── stdio_test.go                      # Stdio transport tests
├── sse.go                             # SSETransport implementation
├── sse_test.go                        # SSE transport tests
├── client.go                          # MCP Client (initialize, tools/list, tools/call)
├── client_test.go                     # Client tests with mock transport
├── adapter.go                         # MCPTool adapter implementing tools.Tool
├── adapter_test.go                    # Adapter tests
├── discovery.go                       # DiscoverAndRegister helper
├── discovery_test.go                  # Discovery tests
├── config.go                          # Config loading from mcp.json
├── config_test.go                     # Config tests
├── manager.go                         # Manager: lifecycle, health, reconnect
└── manager_test.go                    # Manager tests
```

**Modified files (Task 10):**
- `cmd/celeste/main.go` -- load MCP config, create Manager, start/stop lifecycle

---

### Task 1: JSON-RPC Types

**Files:**
- Create: `cmd/celeste/tools/mcp/jsonrpc.go`
- Test: `cmd/celeste/tools/mcp/jsonrpc_test.go`

- [ ] **Step 1: Write the test**

```go
// cmd/celeste/tools/mcp/jsonrpc_test.go
package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestMarshal(t *testing.T) {
	req := &Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params:  json.RawMessage(`{"protocolVersion":"2024-11-05"}`),
	}

	data, err := json.Marshal(req)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, "2.0", decoded["jsonrpc"])
	assert.Equal(t, float64(1), decoded["id"])
	assert.Equal(t, "initialize", decoded["method"])
}

func TestRequestMarshal_NilParams(t *testing.T) {
	req := &Request{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "tools/list",
	}

	data, err := json.Marshal(req)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(data, &decoded))
	// params should be absent when nil
	_, hasParams := decoded["params"]
	assert.False(t, hasParams)
}

func TestNotificationMarshal(t *testing.T) {
	notif := &Notification{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}

	data, err := json.Marshal(notif)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, "2.0", decoded["jsonrpc"])
	assert.Equal(t, "notifications/initialized", decoded["method"])
	// No id field
	_, hasID := decoded["id"]
	assert.False(t, hasID)
}

func TestResponseUnmarshal_Success(t *testing.T) {
	raw := `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05"}}`
	var resp Response
	require.NoError(t, json.Unmarshal([]byte(raw), &resp))

	assert.Equal(t, "2.0", resp.JSONRPC)
	assert.Equal(t, json.Number("1"), resp.ID)
	assert.Nil(t, resp.Error)
	assert.NotNil(t, resp.Result)
}

func TestResponseUnmarshal_Error(t *testing.T) {
	raw := `{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"method not found"}}`
	var resp Response
	require.NoError(t, json.Unmarshal([]byte(raw), &resp))

	assert.NotNil(t, resp.Error)
	assert.Equal(t, -32601, resp.Error.Code)
	assert.Equal(t, "method not found", resp.Error.Message)
}

func TestErrorObjectError(t *testing.T) {
	e := &ErrorObject{Code: -32600, Message: "invalid request", Data: json.RawMessage(`"extra"`)}
	assert.Contains(t, e.Error(), "-32600")
	assert.Contains(t, e.Error(), "invalid request")
}

func TestNewRequest(t *testing.T) {
	params := map[string]any{"name": "test_tool", "arguments": map[string]any{"x": 1}}
	req, err := NewRequest("tools/call", params)
	require.NoError(t, err)

	assert.Equal(t, "2.0", req.JSONRPC)
	assert.Equal(t, "tools/call", req.Method)
	assert.Greater(t, req.ID, int64(0))

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(req.Params, &decoded))
	assert.Equal(t, "test_tool", decoded["name"])
}

func TestNewNotification(t *testing.T) {
	notif := NewNotification("notifications/initialized")
	assert.Equal(t, "2.0", notif.JSONRPC)
	assert.Equal(t, "notifications/initialized", notif.Method)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/kusanagi/Development/celeste-cli && go test ./cmd/celeste/tools/mcp/ -v -run TestRequest`
Expected: FAIL -- package does not exist yet

- [ ] **Step 3: Write the JSON-RPC types**

```go
// cmd/celeste/tools/mcp/jsonrpc.go
package mcp

import (
	"encoding/json"
	"fmt"
	"sync/atomic"
)

// idCounter provides unique IDs for JSON-RPC requests.
var idCounter atomic.Int64

// Request is a JSON-RPC 2.0 request.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Notification is a JSON-RPC 2.0 notification (no ID, no response expected).
type Notification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.Number     `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *ErrorObject    `json:"error,omitempty"`
}

// ErrorObject is the error payload in a JSON-RPC 2.0 response.
type ErrorObject struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Error implements the error interface.
func (e *ErrorObject) Error() string {
	return fmt.Sprintf("JSON-RPC error %d: %s", e.Code, e.Message)
}

// NewRequest creates a new JSON-RPC 2.0 request with an auto-incremented ID.
// params is marshaled to JSON. Pass nil for no params.
func NewRequest(method string, params any) (*Request, error) {
	req := &Request{
		JSONRPC: "2.0",
		ID:      idCounter.Add(1),
		Method:  method,
	}
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal params: %w", err)
		}
		req.Params = data
	}
	return req, nil
}

// NewNotification creates a JSON-RPC 2.0 notification (no ID).
func NewNotification(method string) *Notification {
	return &Notification{
		JSONRPC: "2.0",
		Method:  method,
	}
}
```

- [ ] **Step 4: Run tests**

Run: `cd /Users/kusanagi/Development/celeste-cli && go test ./cmd/celeste/tools/mcp/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/celeste/tools/mcp/jsonrpc.go cmd/celeste/tools/mcp/jsonrpc_test.go
git commit -m "feat(mcp): add JSON-RPC 2.0 request/response types"
```

---

### Task 2: Transport Interface

**Files:**
- Create: `cmd/celeste/tools/mcp/transport.go`

- [ ] **Step 1: Write the transport interface**

```go
// cmd/celeste/tools/mcp/transport.go
package mcp

// Transport is the abstraction over MCP communication channels.
// Implementations handle the physical sending and receiving of JSON-RPC
// messages over stdio (child process) or SSE (HTTP).
type Transport interface {
	// Send sends a JSON-RPC request or notification to the server.
	// For requests, the caller should subsequently call Receive to get the response.
	Send(req *Request) error

	// SendNotification sends a JSON-RPC notification (no response expected).
	SendNotification(notif *Notification) error

	// Receive reads the next JSON-RPC response from the server.
	// Blocks until a response is available or the context is cancelled.
	Receive() (*Response, error)

	// Close shuts down the transport, releasing all resources.
	Close() error
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/kusanagi/Development/celeste-cli && go build ./cmd/celeste/tools/mcp/`
Expected: success (no errors)

- [ ] **Step 3: Commit**

```bash
git add cmd/celeste/tools/mcp/transport.go
git commit -m "feat(mcp): add Transport interface for stdio/SSE abstraction"
```

---

### Task 3: Stdio Transport

**Files:**
- Create: `cmd/celeste/tools/mcp/stdio.go`
- Test: `cmd/celeste/tools/mcp/stdio_test.go`

- [ ] **Step 1: Write the test**

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/kusanagi/Development/celeste-cli && go test ./cmd/celeste/tools/mcp/ -v -run TestStdio`
Expected: FAIL -- `NewStdioTransport` and `expandEnvVars` not defined

- [ ] **Step 3: Write the stdio transport**

```go
// cmd/celeste/tools/mcp/stdio.go
package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// StdioTransport communicates with an MCP server via a child process's
// stdin and stdout. Each JSON-RPC message is a single line of JSON.
type StdioTransport struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	reader *bufio.Reader
	mu     sync.Mutex
	closed bool
}

// NewStdioTransport spawns a child process and connects to its stdin/stdout.
// command is the executable to run (e.g., "npx", "python3").
// args are the command-line arguments.
// env is an optional map of environment variables (supports ${VAR} expansion).
func NewStdioTransport(command string, args []string, env map[string]string) (*StdioTransport, error) {
	cmd := exec.Command(command, args...)

	// Build environment: inherit current env + add custom vars
	if len(env) > 0 {
		expanded := expandEnvVars(env)
		cmdEnv := os.Environ()
		for k, v := range expanded {
			cmdEnv = append(cmdEnv, k+"="+v)
		}
		cmd.Env = cmdEnv
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("create stdout pipe: %w", err)
	}

	// Discard stderr to avoid blocking
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		stdin.Close()
		return nil, fmt.Errorf("start process %q: %w", command, err)
	}

	return &StdioTransport{
		cmd:    cmd,
		stdin:  stdin,
		reader: bufio.NewReader(stdout),
	}, nil
}

// Send sends a JSON-RPC request as a single JSON line to the child process stdin.
func (t *StdioTransport) Send(req *Request) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return fmt.Errorf("transport is closed")
	}

	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	data = append(data, '\n')
	if _, err := t.stdin.Write(data); err != nil {
		return fmt.Errorf("write to stdin: %w", err)
	}
	return nil
}

// SendNotification sends a JSON-RPC notification as a single JSON line.
func (t *StdioTransport) SendNotification(notif *Notification) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return fmt.Errorf("transport is closed")
	}

	data, err := json.Marshal(notif)
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}

	data = append(data, '\n')
	if _, err := t.stdin.Write(data); err != nil {
		return fmt.Errorf("write notification to stdin: %w", err)
	}
	return nil
}

// Receive reads the next JSON line from stdout and parses it as a Response.
func (t *StdioTransport) Receive() (*Response, error) {
	line, err := t.reader.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("read from stdout: %w", err)
	}

	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w (raw: %s)", err, string(line))
	}
	return &resp, nil
}

// Close shuts down the child process and closes pipes.
func (t *StdioTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil
	}
	t.closed = true

	t.stdin.Close()
	// Wait for process to exit (ignore error -- process may have already exited)
	_ = t.cmd.Wait()
	return nil
}

// expandEnvVars expands ${VAR} references in environment variable values
// using the current process's environment.
func expandEnvVars(env map[string]string) map[string]string {
	result := make(map[string]string, len(env))
	for k, v := range env {
		result[k] = os.Expand(v, func(key string) string {
			return os.Getenv(key)
		})
	}
	return result
}

// isEnvVarRef checks if a string contains ${...} patterns.
func isEnvVarRef(s string) bool {
	return strings.Contains(s, "${")
}
```

- [ ] **Step 4: Run tests**

Run: `cd /Users/kusanagi/Development/celeste-cli && go test ./cmd/celeste/tools/mcp/ -v -run TestStdio`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/celeste/tools/mcp/stdio.go cmd/celeste/tools/mcp/stdio_test.go
git commit -m "feat(mcp): add StdioTransport for child process communication"
```

---

### Task 4: SSE Transport

**Files:**
- Create: `cmd/celeste/tools/mcp/sse.go`
- Test: `cmd/celeste/tools/mcp/sse_test.go`

- [ ] **Step 1: Write the test**

```go
// cmd/celeste/tools/mcp/sse_test.go
package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockMCPSSEServer simulates an MCP server over SSE.
// POST requests enqueue a response; the SSE stream delivers them as events.
type mockMCPSSEServer struct {
	mu        sync.Mutex
	responses []Response
	eventCh   chan string
}

func newMockMCPSSEServer() *mockMCPSSEServer {
	return &mockMCPSSEServer{
		eventCh: make(chan string, 100),
	}
}

func (m *mockMCPSSEServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		// SSE endpoint: stream events
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		// Send the endpoint event so the client knows where to POST
		fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", r.URL.Path)
		flusher.Flush()

		for event := range m.eventCh {
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", event)
			flusher.Flush()
		}

	case http.MethodPost:
		// Receive request, parse it, generate a response
		var req Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		// Build a simple echo response
		resp := Response{
			JSONRPC: "2.0",
			ID:      json.Number(fmt.Sprintf("%d", req.ID)),
			Result:  req.Params,
		}

		data, _ := json.Marshal(resp)
		m.eventCh <- string(data)

		w.WriteHeader(http.StatusAccepted)
	}
}

func TestSSETransport_SendReceive(t *testing.T) {
	mock := newMockMCPSSEServer()
	server := httptest.NewServer(mock)
	defer server.Close()
	defer close(mock.eventCh)

	transport, err := NewSSETransport(server.URL)
	require.NoError(t, err)
	defer transport.Close()

	req := &Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/list",
		Params:  json.RawMessage(`{}`),
	}

	err = transport.Send(req)
	require.NoError(t, err)

	resp, err := transport.Receive()
	require.NoError(t, err)
	assert.Equal(t, "2.0", resp.JSONRPC)
	assert.Equal(t, json.Number("1"), resp.ID)
}

func TestSSETransport_Close(t *testing.T) {
	mock := newMockMCPSSEServer()
	server := httptest.NewServer(mock)
	defer server.Close()
	defer close(mock.eventCh)

	transport, err := NewSSETransport(server.URL)
	require.NoError(t, err)

	err = transport.Close()
	assert.NoError(t, err)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/kusanagi/Development/celeste-cli && go test ./cmd/celeste/tools/mcp/ -v -run TestSSE`
Expected: FAIL -- `NewSSETransport` not defined

- [ ] **Step 3: Write the SSE transport**

```go
// cmd/celeste/tools/mcp/sse.go
package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

// SSETransport communicates with an MCP server over HTTP Server-Sent Events.
// Requests are sent via POST to the server's endpoint URL.
// Responses are received via an SSE event stream from a GET connection.
type SSETransport struct {
	baseURL    string
	postURL    string // discovered from SSE endpoint event
	client     *http.Client
	responseCh chan *Response
	mu         sync.Mutex
	closed     bool
	done       chan struct{}
}

// NewSSETransport connects to an MCP server's SSE endpoint.
// url is the base URL of the MCP server (e.g., "http://localhost:3000/sse").
func NewSSETransport(url string) (*SSETransport, error) {
	t := &SSETransport{
		baseURL:    url,
		client:     &http.Client{},
		responseCh: make(chan *Response, 100),
		done:       make(chan struct{}),
	}

	// Connect to the SSE stream in a goroutine
	go t.connectSSE()

	return t, nil
}

// connectSSE establishes the SSE connection and reads events.
func (t *SSETransport) connectSSE() {
	resp, err := t.client.Get(t.baseURL)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	var eventType string

	for scanner.Scan() {
		select {
		case <-t.done:
			return
		default:
		}

		line := scanner.Text()

		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
			continue
		}

		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")

			switch eventType {
			case "endpoint":
				// The server tells us where to POST requests
				t.mu.Lock()
				if strings.HasPrefix(data, "/") {
					// Relative path -- combine with base URL
					// Extract scheme+host from baseURL
					parts := strings.SplitN(t.baseURL, "://", 2)
					if len(parts) == 2 {
						hostEnd := strings.Index(parts[1], "/")
						if hostEnd == -1 {
							t.postURL = t.baseURL + data
						} else {
							t.postURL = parts[0] + "://" + parts[1][:hostEnd] + data
						}
					}
				} else {
					t.postURL = data
				}
				t.mu.Unlock()

			case "message":
				var rpcResp Response
				if err := json.Unmarshal([]byte(data), &rpcResp); err == nil {
					t.responseCh <- &rpcResp
				}
			}

			eventType = ""
			continue
		}
	}
}

// Send sends a JSON-RPC request via HTTP POST to the server's endpoint.
func (t *SSETransport) Send(req *Request) error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return fmt.Errorf("transport is closed")
	}
	postURL := t.postURL
	t.mu.Unlock()

	if postURL == "" {
		// If we have not yet received the endpoint event, POST to base URL
		postURL = t.baseURL
	}

	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	httpResp, err := t.client.Post(postURL, "application/json", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("POST request: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode >= 400 {
		return fmt.Errorf("POST returned status %d", httpResp.StatusCode)
	}

	return nil
}

// SendNotification sends a JSON-RPC notification via HTTP POST.
func (t *SSETransport) SendNotification(notif *Notification) error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return fmt.Errorf("transport is closed")
	}
	postURL := t.postURL
	t.mu.Unlock()

	if postURL == "" {
		postURL = t.baseURL
	}

	data, err := json.Marshal(notif)
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}

	httpResp, err := t.client.Post(postURL, "application/json", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("POST notification: %w", err)
	}
	defer httpResp.Body.Close()

	return nil
}

// Receive reads the next JSON-RPC response from the SSE event stream.
func (t *SSETransport) Receive() (*Response, error) {
	resp, ok := <-t.responseCh
	if !ok {
		return nil, fmt.Errorf("transport closed")
	}
	return resp, nil
}

// Close shuts down the SSE connection.
func (t *SSETransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil
	}
	t.closed = true
	close(t.done)
	return nil
}
```

- [ ] **Step 4: Run tests**

Run: `cd /Users/kusanagi/Development/celeste-cli && go test ./cmd/celeste/tools/mcp/ -v -run TestSSE -timeout 10s`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/celeste/tools/mcp/sse.go cmd/celeste/tools/mcp/sse_test.go
git commit -m "feat(mcp): add SSETransport for HTTP-based MCP servers"
```

---

### Task 5: MCP Client

**Files:**
- Create: `cmd/celeste/tools/mcp/client.go`
- Test: `cmd/celeste/tools/mcp/client_test.go`

- [ ] **Step 1: Write the test**

```go
// cmd/celeste/tools/mcp/client_test.go
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockTransport implements Transport for testing the Client.
type mockTransport struct {
	sent      []*Request
	notifs    []*Notification
	responses []*Response
	idx       int
	closed    bool
}

func (m *mockTransport) Send(req *Request) error {
	if m.closed {
		return fmt.Errorf("closed")
	}
	m.sent = append(m.sent, req)
	return nil
}

func (m *mockTransport) SendNotification(notif *Notification) error {
	if m.closed {
		return fmt.Errorf("closed")
	}
	m.notifs = append(m.notifs, notif)
	return nil
}

func (m *mockTransport) Receive() (*Response, error) {
	if m.closed {
		return nil, fmt.Errorf("closed")
	}
	if m.idx >= len(m.responses) {
		return nil, fmt.Errorf("no more responses")
	}
	resp := m.responses[m.idx]
	m.idx++
	return resp, nil
}

func (m *mockTransport) Close() error {
	m.closed = true
	return nil
}

func TestClient_Initialize(t *testing.T) {
	transport := &mockTransport{
		responses: []*Response{
			{
				JSONRPC: "2.0",
				ID:      json.Number("1"),
				Result:  json.RawMessage(`{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"test-server","version":"1.0"}}`),
			},
		},
	}

	client := NewClient(transport, "celeste", "1.7.0")
	err := client.Initialize(context.Background())
	require.NoError(t, err)

	// Verify the initialize request was sent
	require.Len(t, transport.sent, 1)
	assert.Equal(t, "initialize", transport.sent[0].Method)

	// Verify notifications/initialized was sent
	require.Len(t, transport.notifs, 1)
	assert.Equal(t, "notifications/initialized", transport.notifs[0].Method)

	assert.Equal(t, "test-server", client.ServerName())
}

func TestClient_Initialize_VersionMismatch(t *testing.T) {
	transport := &mockTransport{
		responses: []*Response{
			{
				JSONRPC: "2.0",
				ID:      json.Number("1"),
				Result:  json.RawMessage(`{"protocolVersion":"2099-01-01","capabilities":{},"serverInfo":{"name":"future"}}`),
			},
		},
	}

	client := NewClient(transport, "celeste", "1.7.0")
	err := client.Initialize(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "protocol version")
}

func TestClient_ListTools(t *testing.T) {
	transport := &mockTransport{
		responses: []*Response{
			// Initialize response
			{
				JSONRPC: "2.0",
				ID:      json.Number("1"),
				Result:  json.RawMessage(`{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"test"}}`),
			},
			// tools/list response
			{
				JSONRPC: "2.0",
				ID:      json.Number("2"),
				Result:  json.RawMessage(`{"tools":[{"name":"get_weather","description":"Get weather for a location","inputSchema":{"type":"object","properties":{"location":{"type":"string"}},"required":["location"]}}]}`),
			},
		},
	}

	client := NewClient(transport, "celeste", "1.7.0")
	require.NoError(t, client.Initialize(context.Background()))

	tools, err := client.ListTools(context.Background())
	require.NoError(t, err)
	require.Len(t, tools, 1)
	assert.Equal(t, "get_weather", tools[0].Name)
	assert.Equal(t, "Get weather for a location", tools[0].Description)
}

func TestClient_CallTool(t *testing.T) {
	transport := &mockTransport{
		responses: []*Response{
			// Initialize response
			{
				JSONRPC: "2.0",
				ID:      json.Number("1"),
				Result:  json.RawMessage(`{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"test"}}`),
			},
			// tools/call response
			{
				JSONRPC: "2.0",
				ID:      json.Number("2"),
				Result:  json.RawMessage(`{"content":[{"type":"text","text":"Sunny, 72F"}]}`),
			},
		},
	}

	client := NewClient(transport, "celeste", "1.7.0")
	require.NoError(t, client.Initialize(context.Background()))

	result, err := client.CallTool(context.Background(), "get_weather", map[string]any{"location": "NYC"})
	require.NoError(t, err)
	assert.Equal(t, "Sunny, 72F", result)
}

func TestClient_CallTool_ErrorResponse(t *testing.T) {
	transport := &mockTransport{
		responses: []*Response{
			// Initialize response
			{
				JSONRPC: "2.0",
				ID:      json.Number("1"),
				Result:  json.RawMessage(`{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"test"}}`),
			},
			// tools/call error response
			{
				JSONRPC: "2.0",
				ID:      json.Number("2"),
				Error:   &ErrorObject{Code: -32000, Message: "tool execution failed"},
			},
		},
	}

	client := NewClient(transport, "celeste", "1.7.0")
	require.NoError(t, client.Initialize(context.Background()))

	_, err := client.CallTool(context.Background(), "bad_tool", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "tool execution failed")
}

func TestClient_CallTool_MultipleContentBlocks(t *testing.T) {
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
				Result:  json.RawMessage(`{"content":[{"type":"text","text":"Line 1"},{"type":"text","text":"Line 2"}]}`),
			},
		},
	}

	client := NewClient(transport, "celeste", "1.7.0")
	require.NoError(t, client.Initialize(context.Background()))

	result, err := client.CallTool(context.Background(), "multi", nil)
	require.NoError(t, err)
	assert.Equal(t, "Line 1\nLine 2", result)
}

func TestClient_Close(t *testing.T) {
	transport := &mockTransport{}
	client := NewClient(transport, "celeste", "1.7.0")
	err := client.Close()
	assert.NoError(t, err)
	assert.True(t, transport.closed)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/kusanagi/Development/celeste-cli && go test ./cmd/celeste/tools/mcp/ -v -run TestClient`
Expected: FAIL -- `NewClient` not defined

- [ ] **Step 3: Write the MCP client**

```go
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

// toolCallResult is the server's response to tools/call.
type toolCallResult struct {
	Content []contentBlock `json:"content"`
}

// contentBlock is a single content item in a tool call response.
type contentBlock struct {
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

	var result toolCallResult
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
```

- [ ] **Step 4: Run tests**

Run: `cd /Users/kusanagi/Development/celeste-cli && go test ./cmd/celeste/tools/mcp/ -v -run TestClient`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/celeste/tools/mcp/client.go cmd/celeste/tools/mcp/client_test.go
git commit -m "feat(mcp): add MCP Client with initialize, tools/list, tools/call"
```

---

### Task 6: Tool Adapter

**Files:**
- Create: `cmd/celeste/tools/mcp/adapter.go`
- Test: `cmd/celeste/tools/mcp/adapter_test.go`

- [ ] **Step 1: Write the test**

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/kusanagi/Development/celeste-cli && go test ./cmd/celeste/tools/mcp/ -v -run TestMCPTool`
Expected: FAIL -- `MCPTool` and `NewMCPTool` not defined

- [ ] **Step 3: Write the adapter**

```go
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
```

- [ ] **Step 4: Run tests**

Run: `cd /Users/kusanagi/Development/celeste-cli && go test ./cmd/celeste/tools/mcp/ -v -run TestMCPTool`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/celeste/tools/mcp/adapter.go cmd/celeste/tools/mcp/adapter_test.go
git commit -m "feat(mcp): add MCPTool adapter bridging MCP tools to Tool interface"
```

---

### Task 7: Discovery and Registration

**Files:**
- Create: `cmd/celeste/tools/mcp/discovery.go`
- Test: `cmd/celeste/tools/mcp/discovery_test.go`

- [ ] **Step 1: Write the test**

```go
// cmd/celeste/tools/mcp/discovery_test.go
package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

func TestDiscoverAndRegister(t *testing.T) {
	transport := &mockTransport{
		responses: []*Response{
			// Initialize
			{
				JSONRPC: "2.0",
				ID:      json.Number("1"),
				Result:  json.RawMessage(`{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"test-server"}}`),
			},
			// tools/list with 2 tools
			{
				JSONRPC: "2.0",
				ID:      json.Number("2"),
				Result: json.RawMessage(`{"tools":[
					{"name":"tool_a","description":"Tool A","inputSchema":{"type":"object"}},
					{"name":"tool_b","description":"Tool B","inputSchema":{"type":"object","properties":{"x":{"type":"string"}}}}
				]}`),
			},
		},
	}

	client := NewClient(transport, "celeste", "1.7.0")
	require.NoError(t, client.Initialize(context.Background()))

	registry := tools.NewRegistry()
	err := DiscoverAndRegister(context.Background(), client, registry, "test-server")
	require.NoError(t, err)

	// Both tools should be registered
	assert.Equal(t, 2, registry.Count())

	toolA, ok := registry.Get("tool_a")
	require.True(t, ok)
	assert.Equal(t, "Tool A", toolA.Description())

	toolB, ok := registry.Get("tool_b")
	require.True(t, ok)
	assert.Equal(t, "Tool B", toolB.Description())
}

func TestDiscoverAndRegister_NoTools(t *testing.T) {
	transport := &mockTransport{
		responses: []*Response{
			{
				JSONRPC: "2.0",
				ID:      json.Number("1"),
				Result:  json.RawMessage(`{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"empty"}}`),
			},
			{
				JSONRPC: "2.0",
				ID:      json.Number("2"),
				Result:  json.RawMessage(`{"tools":[]}`),
			},
		},
	}

	client := NewClient(transport, "celeste", "1.7.0")
	require.NoError(t, client.Initialize(context.Background()))

	registry := tools.NewRegistry()
	err := DiscoverAndRegister(context.Background(), client, registry, "empty-server")
	require.NoError(t, err)
	assert.Equal(t, 0, registry.Count())
}

func TestDiscoverAndRegister_ListError(t *testing.T) {
	transport := &mockTransport{
		responses: []*Response{
			{
				JSONRPC: "2.0",
				ID:      json.Number("1"),
				Result:  json.RawMessage(`{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"err"}}`),
			},
			{
				JSONRPC: "2.0",
				ID:      json.Number("2"),
				Error:   &ErrorObject{Code: -32601, Message: "method not found"},
			},
		},
	}

	client := NewClient(transport, "celeste", "1.7.0")
	require.NoError(t, client.Initialize(context.Background()))

	registry := tools.NewRegistry()
	err := DiscoverAndRegister(context.Background(), client, registry, "bad-server")
	assert.Error(t, err)
	assert.Equal(t, 0, registry.Count())
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/kusanagi/Development/celeste-cli && go test ./cmd/celeste/tools/mcp/ -v -run TestDiscover`
Expected: FAIL -- `DiscoverAndRegister` not defined

- [ ] **Step 3: Write the discovery function**

```go
// cmd/celeste/tools/mcp/discovery.go
package mcp

import (
	"context"
	"fmt"
	"log"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// DiscoverAndRegister queries the MCP server for available tools via tools/list,
// creates an MCPTool adapter for each, and registers them in the registry.
// The serverName is recorded in each tool's metadata for debugging and display.
func DiscoverAndRegister(ctx context.Context, client *Client, registry *tools.Registry, serverName string) error {
	defs, err := client.ListTools(ctx)
	if err != nil {
		return fmt.Errorf("discover tools from %s: %w", serverName, err)
	}

	for _, def := range defs {
		tool := NewMCPTool(def, client, serverName)
		registry.Register(tool)
		log.Printf("[mcp] registered tool %q from server %q", def.Name, serverName)
	}

	if len(defs) > 0 {
		log.Printf("[mcp] discovered %d tools from server %q", len(defs), serverName)
	}

	return nil
}
```

- [ ] **Step 4: Run tests**

Run: `cd /Users/kusanagi/Development/celeste-cli && go test ./cmd/celeste/tools/mcp/ -v -run TestDiscover`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/celeste/tools/mcp/discovery.go cmd/celeste/tools/mcp/discovery_test.go
git commit -m "feat(mcp): add DiscoverAndRegister for MCP tool discovery"
```

---

### Task 8: Config

**Files:**
- Create: `cmd/celeste/tools/mcp/config.go`
- Test: `cmd/celeste/tools/mcp/config_test.go`

- [ ] **Step 1: Write the test**

```go
// cmd/celeste/tools/mcp/config_test.go
package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig_ValidStdio(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp.json")

	configJSON := `{
		"mcpServers": {
			"weather": {
				"transport": "stdio",
				"command": "npx",
				"args": ["-y", "@weather/mcp-server"],
				"env": {
					"API_KEY": "test-key"
				}
			}
		}
	}`

	require.NoError(t, os.WriteFile(configPath, []byte(configJSON), 0644))

	config, err := LoadConfig(configPath)
	require.NoError(t, err)
	require.Len(t, config.Servers, 1)

	server := config.Servers["weather"]
	assert.Equal(t, "stdio", server.Transport)
	assert.Equal(t, "npx", server.Command)
	assert.Equal(t, []string{"-y", "@weather/mcp-server"}, server.Args)
	assert.Equal(t, "test-key", server.Env["API_KEY"])
}

func TestLoadConfig_ValidSSE(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp.json")

	configJSON := `{
		"mcpServers": {
			"remote": {
				"transport": "sse",
				"url": "http://localhost:3000/sse"
			}
		}
	}`

	require.NoError(t, os.WriteFile(configPath, []byte(configJSON), 0644))

	config, err := LoadConfig(configPath)
	require.NoError(t, err)
	require.Len(t, config.Servers, 1)

	server := config.Servers["remote"]
	assert.Equal(t, "sse", server.Transport)
	assert.Equal(t, "http://localhost:3000/sse", server.URL)
}

func TestLoadConfig_MultipleServers(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp.json")

	configJSON := `{
		"mcpServers": {
			"server1": {
				"transport": "stdio",
				"command": "node",
				"args": ["server1.js"]
			},
			"server2": {
				"transport": "sse",
				"url": "http://example.com/sse"
			}
		}
	}`

	require.NoError(t, os.WriteFile(configPath, []byte(configJSON), 0644))

	config, err := LoadConfig(configPath)
	require.NoError(t, err)
	assert.Len(t, config.Servers, 2)
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	config, err := LoadConfig("/nonexistent/mcp.json")
	assert.NoError(t, err) // Missing config is not an error -- just no servers
	assert.NotNil(t, config)
	assert.Empty(t, config.Servers)
}

func TestLoadConfig_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp.json")
	require.NoError(t, os.WriteFile(configPath, []byte(`{bad json`), 0644))

	_, err := LoadConfig(configPath)
	assert.Error(t, err)
}

func TestLoadConfig_EnvExpansion(t *testing.T) {
	t.Setenv("MCP_SECRET_KEY", "secret123")

	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp.json")

	configJSON := `{
		"mcpServers": {
			"secure": {
				"transport": "stdio",
				"command": "secure-server",
				"env": {
					"API_KEY": "${MCP_SECRET_KEY}"
				}
			}
		}
	}`

	require.NoError(t, os.WriteFile(configPath, []byte(configJSON), 0644))

	config, err := LoadConfig(configPath)
	require.NoError(t, err)

	// env expansion happens at transport creation time, not at config load time
	// so the raw value should still contain the ${...} reference
	assert.Equal(t, "${MCP_SECRET_KEY}", config.Servers["secure"].Env["API_KEY"])
}

func TestLoadConfig_DefaultTransport(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp.json")

	// No "transport" field -- should default to "stdio"
	configJSON := `{
		"mcpServers": {
			"simple": {
				"command": "my-server"
			}
		}
	}`

	require.NoError(t, os.WriteFile(configPath, []byte(configJSON), 0644))

	config, err := LoadConfig(configPath)
	require.NoError(t, err)

	server := config.Servers["simple"]
	assert.Equal(t, "stdio", server.Transport)
}

func TestDefaultConfigPath(t *testing.T) {
	path := DefaultConfigPath()
	assert.Contains(t, path, ".celeste")
	assert.Contains(t, path, "mcp.json")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/kusanagi/Development/celeste-cli && go test ./cmd/celeste/tools/mcp/ -v -run TestLoadConfig`
Expected: FAIL -- `LoadConfig` not defined

- [ ] **Step 3: Write the config loader**

```go
// cmd/celeste/tools/mcp/config.go
package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// MCPConfig is the top-level configuration for MCP servers.
// Loaded from ~/.celeste/mcp.json.
type MCPConfig struct {
	Servers map[string]ServerConfig `json:"mcpServers"`
}

// ServerConfig defines how to connect to a single MCP server.
type ServerConfig struct {
	// Transport is "stdio" or "sse". Defaults to "stdio" if not set.
	Transport string `json:"transport"`

	// Command is the executable to spawn (stdio transport only).
	Command string `json:"command,omitempty"`

	// Args are command-line arguments (stdio transport only).
	Args []string `json:"args,omitempty"`

	// URL is the SSE endpoint URL (sse transport only).
	URL string `json:"url,omitempty"`

	// Env is a map of environment variables passed to the child process.
	// Values support ${VAR} expansion from the host environment.
	Env map[string]string `json:"env,omitempty"`
}

// DefaultConfigPath returns the default path for the MCP configuration file.
func DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".celeste", "mcp.json")
}

// LoadConfig reads and parses the MCP configuration from a JSON file.
// If the file does not exist, returns an empty config (not an error).
// This allows celeste to start without any MCP servers configured.
func LoadConfig(path string) (*MCPConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &MCPConfig{Servers: make(map[string]ServerConfig)}, nil
		}
		return nil, fmt.Errorf("read MCP config %s: %w", path, err)
	}

	var config MCPConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parse MCP config %s: %w", path, err)
	}

	if config.Servers == nil {
		config.Servers = make(map[string]ServerConfig)
	}

	// Apply defaults
	for name, server := range config.Servers {
		if server.Transport == "" {
			server.Transport = "stdio"
			config.Servers[name] = server
		}
	}

	return &config, nil
}
```

- [ ] **Step 4: Run tests**

Run: `cd /Users/kusanagi/Development/celeste-cli && go test ./cmd/celeste/tools/mcp/ -v -run "TestLoadConfig|TestDefaultConfig"`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/celeste/tools/mcp/config.go cmd/celeste/tools/mcp/config_test.go
git commit -m "feat(mcp): add config loader for ~/.celeste/mcp.json"
```

---

### Task 9: Manager

**Files:**
- Create: `cmd/celeste/tools/mcp/manager.go`
- Test: `cmd/celeste/tools/mcp/manager_test.go`

- [ ] **Step 1: Write the test**

```go
// cmd/celeste/tools/mcp/manager_test.go
package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

func TestNewManager(t *testing.T) {
	registry := tools.NewRegistry()
	m := NewManager("/nonexistent/path.json", registry)
	assert.NotNil(t, m)
}

func TestManager_Start_NoConfig(t *testing.T) {
	registry := tools.NewRegistry()
	m := NewManager("/nonexistent/mcp.json", registry)

	// Start should succeed even with no config file (no servers to connect to)
	err := m.Start(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, 0, registry.Count())
}

func TestManager_Stop_NoServers(t *testing.T) {
	registry := tools.NewRegistry()
	m := NewManager("/nonexistent/mcp.json", registry)
	require.NoError(t, m.Start(context.Background()))

	err := m.Stop()
	assert.NoError(t, err)
}

func TestManager_Start_InvalidConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp.json")
	require.NoError(t, os.WriteFile(configPath, []byte(`{bad`), 0644))

	registry := tools.NewRegistry()
	m := NewManager(configPath, registry)

	err := m.Start(context.Background())
	assert.Error(t, err)
}

func TestManager_ServerCount(t *testing.T) {
	registry := tools.NewRegistry()
	m := NewManager("/nonexistent/mcp.json", registry)
	require.NoError(t, m.Start(context.Background()))
	assert.Equal(t, 0, m.ServerCount())
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/kusanagi/Development/celeste-cli && go test ./cmd/celeste/tools/mcp/ -v -run TestManager`
Expected: FAIL -- `NewManager` not defined

- [ ] **Step 3: Write the manager**

```go
// cmd/celeste/tools/mcp/manager.go
package mcp

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// Manager handles the lifecycle of all MCP server connections.
// It loads the config, creates transports, initializes clients,
// discovers tools, and registers them in the tool registry.
type Manager struct {
	configPath string
	registry   *tools.Registry
	clients    map[string]*Client
	mu         sync.Mutex
	cancel     context.CancelFunc
}

// NewManager creates a new MCP manager.
func NewManager(configPath string, registry *tools.Registry) *Manager {
	return &Manager{
		configPath: configPath,
		registry:   registry,
		clients:    make(map[string]*Client),
	}
}

// Start loads the MCP config and connects to all configured servers.
// For each server, it creates a transport, initializes the MCP client,
// discovers tools, and registers them in the registry.
// Errors connecting to individual servers are logged but do not prevent
// other servers from starting. Returns an error only if config loading fails.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ctx, m.cancel = context.WithCancel(ctx)

	config, err := LoadConfig(m.configPath)
	if err != nil {
		return fmt.Errorf("load MCP config: %w", err)
	}

	if len(config.Servers) == 0 {
		return nil
	}

	log.Printf("[mcp] connecting to %d MCP server(s)...", len(config.Servers))

	for name, serverCfg := range config.Servers {
		if err := m.connectServer(ctx, name, serverCfg); err != nil {
			log.Printf("[mcp] WARNING: failed to connect to server %q: %v", name, err)
			continue
		}
	}

	// Start health monitoring in the background
	go m.healthLoop(ctx, config)

	return nil
}

// connectServer creates a transport, initializes a client, and discovers tools
// for a single MCP server.
func (m *Manager) connectServer(ctx context.Context, name string, cfg ServerConfig) error {
	var transport Transport
	var err error

	switch cfg.Transport {
	case "stdio":
		transport, err = NewStdioTransport(cfg.Command, cfg.Args, cfg.Env)
		if err != nil {
			return fmt.Errorf("create stdio transport for %s: %w", name, err)
		}

	case "sse":
		transport, err = NewSSETransport(cfg.URL)
		if err != nil {
			return fmt.Errorf("create SSE transport for %s: %w", name, err)
		}

	default:
		return fmt.Errorf("unknown transport type %q for server %s", cfg.Transport, name)
	}

	client := NewClient(transport, "celeste", "1.7.0")

	initCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := client.Initialize(initCtx); err != nil {
		transport.Close()
		return fmt.Errorf("initialize server %s: %w", name, err)
	}

	if err := DiscoverAndRegister(ctx, client, m.registry, name); err != nil {
		client.Close()
		return fmt.Errorf("discover tools from %s: %w", name, err)
	}

	m.clients[name] = client
	log.Printf("[mcp] connected to server %q (%s)", name, client.ServerName())
	return nil
}

// healthLoop periodically checks server connectivity and reconnects if needed.
func (m *Manager) healthLoop(ctx context.Context, config *MCPConfig) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.mu.Lock()
			for name, serverCfg := range config.Servers {
				if _, exists := m.clients[name]; !exists {
					log.Printf("[mcp] attempting reconnect to server %q...", name)
					if err := m.connectServer(ctx, name, serverCfg); err != nil {
						log.Printf("[mcp] reconnect to %q failed: %v", name, err)
					}
				}
			}
			m.mu.Unlock()
		}
	}
}

// Stop shuts down all MCP server connections.
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cancel != nil {
		m.cancel()
	}

	var firstErr error
	for name, client := range m.clients {
		if err := client.Close(); err != nil {
			log.Printf("[mcp] error closing server %q: %v", name, err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	m.clients = make(map[string]*Client)

	return firstErr
}

// ServerCount returns the number of currently connected MCP servers.
func (m *Manager) ServerCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.clients)
}
```

- [ ] **Step 4: Run tests**

Run: `cd /Users/kusanagi/Development/celeste-cli && go test ./cmd/celeste/tools/mcp/ -v -run TestManager`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/celeste/tools/mcp/manager.go cmd/celeste/tools/mcp/manager_test.go
git commit -m "feat(mcp): add Manager for MCP server lifecycle and health monitoring"
```

---

### Task 10: Wire into main.go

**Files:**
- Modify: `cmd/celeste/main.go`

This task integrates the MCP manager into the application startup/shutdown flow.

- [ ] **Step 1: Add MCP manager initialization to main.go**

Add the following to the application initialization section of `cmd/celeste/main.go`, after the tool registry is created and built-in tools are registered (from Plan 1):

```go
// --- Add these imports ---
import (
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools/mcp"
)

// --- Add after registry creation and builtin tool registration ---

// MCP: Connect to external tool servers
mcpConfigPath := mcp.DefaultConfigPath()
mcpManager := mcp.NewManager(mcpConfigPath, registry)
if err := mcpManager.Start(ctx); err != nil {
	log.Printf("WARNING: MCP initialization failed: %v", err)
	// Non-fatal: celeste works without MCP servers
}
```

- [ ] **Step 2: Add MCP manager shutdown**

Add cleanup to the application shutdown path (where deferred cleanup runs or in the shutdown handler):

```go
// --- Add to shutdown/cleanup section ---
defer func() {
	if err := mcpManager.Stop(); err != nil {
		log.Printf("WARNING: MCP shutdown error: %v", err)
	}
}()
```

- [ ] **Step 3: Verify the integration compiles**

Run: `cd /Users/kusanagi/Development/celeste-cli && go build ./cmd/celeste/`
Expected: success

- [ ] **Step 4: Manual integration test**

Create a test config to verify end-to-end:

```bash
# Create a minimal mcp.json for testing (no real servers needed)
mkdir -p ~/.celeste
cat > ~/.celeste/mcp.json.test << 'EOF'
{
  "mcpServers": {}
}
EOF

# Build and run -- should start without errors even with no servers
cd /Users/kusanagi/Development/celeste-cli && go build -o celeste-test ./cmd/celeste/
# The binary should start normally (MCP loads empty config, logs nothing)
```

- [ ] **Step 5: Commit**

```bash
git add cmd/celeste/main.go
git commit -m "feat(mcp): wire MCP manager into application startup and shutdown"
```

---

### Task 11: Final Verification

- [ ] **Step 1: Run all MCP package tests**

```bash
cd /Users/kusanagi/Development/celeste-cli && go test ./cmd/celeste/tools/mcp/ -v -count=1
```

Expected: all tests PASS

- [ ] **Step 2: Run tests with race detector**

```bash
cd /Users/kusanagi/Development/celeste-cli && go test ./cmd/celeste/tools/mcp/ -race -count=1
```

Expected: no race conditions detected

- [ ] **Step 3: Verify the full build succeeds**

```bash
cd /Users/kusanagi/Development/celeste-cli && go build ./cmd/celeste/
```

Expected: clean build, no errors

- [ ] **Step 4: Verify no regressions in existing tests**

```bash
cd /Users/kusanagi/Development/celeste-cli && go test ./... -count=1
```

Expected: all existing tests still pass

- [ ] **Step 5: Check test coverage**

```bash
cd /Users/kusanagi/Development/celeste-cli && go test ./cmd/celeste/tools/mcp/ -coverprofile=coverage_mcp.out -covermode=atomic
go tool cover -func=coverage_mcp.out | tail -1
```

Expected: coverage above 70%

---

## Example `~/.celeste/mcp.json`

```json
{
  "mcpServers": {
    "filesystem": {
      "transport": "stdio",
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/home/user/documents"],
      "env": {}
    },
    "github": {
      "transport": "stdio",
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-github"],
      "env": {
        "GITHUB_PERSONAL_ACCESS_TOKEN": "${GITHUB_TOKEN}"
      }
    },
    "remote-api": {
      "transport": "sse",
      "url": "http://localhost:8080/sse"
    }
  }
}
```

---

## Summary of Deliverables

| Task | File(s) | What it delivers |
|------|---------|-----------------|
| 1 | `jsonrpc.go`, `jsonrpc_test.go` | JSON-RPC 2.0 Request, Response, Notification, ErrorObject types |
| 2 | `transport.go` | Transport interface (Send, SendNotification, Receive, Close) |
| 3 | `stdio.go`, `stdio_test.go` | StdioTransport: spawn child process, JSON-RPC over stdin/stdout |
| 4 | `sse.go`, `sse_test.go` | SSETransport: HTTP POST + SSE event stream |
| 5 | `client.go`, `client_test.go` | MCP Client: initialize handshake, tools/list, tools/call |
| 6 | `adapter.go`, `adapter_test.go` | MCPTool: bridges MCP tool definitions to tools.Tool interface |
| 7 | `discovery.go`, `discovery_test.go` | DiscoverAndRegister: auto-register MCP tools in the registry |
| 8 | `config.go`, `config_test.go` | LoadConfig: parse ~/.celeste/mcp.json with env var support |
| 9 | `manager.go`, `manager_test.go` | Manager: lifecycle, multi-server, health monitoring, reconnect |
| 10 | `main.go` (modified) | Wire MCP into app startup/shutdown |
| 11 | (verification only) | Tests pass, race-free, builds clean |
