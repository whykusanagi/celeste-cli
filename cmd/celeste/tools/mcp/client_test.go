// cmd/celeste/tools/mcp/client_test.go
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"testing"
	"time"

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

// Server negotiates a supported version different from the one we proposed
// (e.g. api.x.com/mcp speaks 2025-06-18). The client must accept it and
// record the negotiated version, not reject on inequality.
func TestClient_Initialize_NegotiatesSupportedVersion(t *testing.T) {
	for _, version := range supportedProtocolVersions {
		t.Run(version, func(t *testing.T) {
			result := fmt.Sprintf(`{"protocolVersion":%q,"capabilities":{},"serverInfo":{"name":"xmcp"}}`, version)
			transport := &mockTransport{
				responses: []*Response{
					{JSONRPC: "2.0", ID: json.Number("1"), Result: json.RawMessage(result)},
				},
			}

			client := NewClient(transport, "celeste", "1.7.0")
			require.NoError(t, client.Initialize(context.Background()))
			assert.Equal(t, version, client.ProtocolVersion())
			require.Len(t, transport.notifs, 1)
		})
	}
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

// stallTransport answers only when the test says so, like a hung MCP server.
type stallTransport struct {
	sent    chan *Request
	replies chan *Response
}

func (s *stallTransport) Send(req *Request) error              { s.sent <- req; return nil }
func (s *stallTransport) SendNotification(*Notification) error { return nil }
func (s *stallTransport) Receive() (*Response, error) {
	r, ok := <-s.replies
	if !ok {
		return nil, fmt.Errorf("closed")
	}
	return r, nil
}
func (s *stallTransport) Close() error { close(s.replies); return nil }

func newStallClient(t *testing.T) (*Client, *stallTransport) {
	t.Helper()
	tr := &stallTransport{sent: make(chan *Request, 64), replies: make(chan *Response, 64)}
	c := NewClient(tr, "celeste", "test")
	c.initialized = true
	t.Cleanup(func() { _ = c.Close() })
	return c, tr
}

func textReply(id int64, text string) *Response {
	res, _ := json.Marshal(map[string]any{"content": []map[string]any{{"type": "text", "text": text}}})
	return &Response{JSONRPC: "2.0", ID: json.Number(strconv.FormatInt(id, 10)), Result: res}
}

type callOut struct {
	text string
	err  error
}

func callAsync(c *Client, name string) <-chan callOut {
	done := make(chan callOut, 1)
	go func() {
		text, err := c.CallTool(context.Background(), name, nil)
		done <- callOut{text, err}
	}()
	return done
}

func cancelledCall(t *testing.T, c *Client) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.CallTool(ctx, "hung", nil)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 2*time.Second {
		t.Fatalf("cancelled call: err=%v after %s", err, time.Since(start))
	}
}

func awaitSent(t *testing.T, tr *stallTransport) *Request {
	t.Helper()
	select {
	case r := <-tr.sent:
		return r
	case <-time.After(2 * time.Second):
		t.Fatal("the next call is blocked behind the cancelled one")
		return nil
	}
}

func awaitCall(t *testing.T, done <-chan callOut, want string) {
	t.Helper()
	select {
	case o := <-done:
		if o.err != nil || o.text != want {
			t.Fatalf("next call = %q, %v; want %q", o.text, o.err, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the next call never returned")
	}
}

// #221: a cancelled CallTool returns promptly, doesn't hold the client, and
// its late answer is not mistaken for the next call's.
func TestClient_CallToolHonoursContext(t *testing.T) {
	c, tr := newStallClient(t)

	cancelledCall(t, c)
	first := <-tr.sent

	done := callAsync(c, "next")
	second := awaitSent(t, tr)
	tr.replies <- textReply(first.ID, "late answer to the cancelled call")
	tr.replies <- textReply(second.ID, "answer to the next call")
	awaitCall(t, done, "answer to the next call")
}

// #221: a server that never answers the cancelled call doesn't stop the
// next calls from getting their own answers.
func TestClient_CallToolAfterAnUnansweredCancel(t *testing.T) {
	c, tr := newStallClient(t)

	cancelledCall(t, c)
	<-tr.sent

	for i := range 3 {
		want := fmt.Sprintf("answer %d", i)
		done := callAsync(c, "next")
		req := awaitSent(t, tr)
		tr.replies <- textReply(req.ID, want)
		awaitCall(t, done, want)
	}
}

// A late answer that arrives after later calls already succeeded is still
// dropped, and notifications between answers are skipped.
func TestClient_CallToolDropsLateAnswersAndSkipsNotifications(t *testing.T) {
	c, tr := newStallClient(t)

	cancelledCall(t, c)
	first := <-tr.sent
	cancelledCall(t, c)
	second := <-tr.sent

	done := callAsync(c, "next")
	third := awaitSent(t, tr)
	tr.replies <- &Response{JSONRPC: "2.0"} // notifications/progress, say
	tr.replies <- textReply(second.ID, "late answer to the second call")
	tr.replies <- textReply(third.ID, "answer to the third call")
	awaitCall(t, done, "answer to the third call")

	done = callAsync(c, "next")
	fourth := awaitSent(t, tr)
	tr.replies <- textReply(first.ID, "very late answer to the first call")
	tr.replies <- textReply(fourth.ID, "answer to the fourth call")
	awaitCall(t, done, "answer to the fourth call")
}

// Cancelled calls share one outstanding Receive instead of starting one each.
func TestClient_CancelledCallsLeaveOneReader(t *testing.T) {
	c, tr := newStallClient(t)

	cancelledCall(t, c)
	<-tr.sent
	before := runtime.NumGoroutine()
	for range 10 {
		cancelledCall(t, c)
		<-tr.sent
	}
	// Allow a moment for timer goroutines from the cancelled contexts to exit.
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if after := runtime.NumGoroutine(); after > before {
		t.Fatalf("goroutines grew from %d to %d over 10 cancelled calls", before, after)
	}
}

// stubServerEnv makes the test binary act as a stdio MCP server (see
// TestMCPStubServer).
const stubServerEnv = "CELESTE_MCP_STUB_SERVER"

// TestMCPStubServer is not a test: run with stubServerEnv set, it is an MCP
// server over stdio that never answers tools/call "hang". Before answering
// any other call, it sends a progress notification and then the late
// answers to every "hang" call so far.
func TestMCPStubServer(t *testing.T) {
	if os.Getenv(stubServerEnv) != "1" {
		t.Skip("helper process for the stdio cancellation tests")
	}
	out := bufio.NewWriter(os.Stdout)
	write := func(v any) {
		b, _ := json.Marshal(v)
		out.Write(append(b, '\n'))
		out.Flush()
	}
	var hung []int64
	in := bufio.NewScanner(os.Stdin)
	for in.Scan() {
		var req struct {
			ID     *int64 `json:"id"`
			Method string `json:"method"`
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		if json.Unmarshal(in.Bytes(), &req) != nil || req.ID == nil {
			continue
		}
		switch {
		case req.Method == "initialize":
			write(map[string]any{"jsonrpc": "2.0", "id": *req.ID, "result": map[string]any{
				"protocolVersion": preferredProtocolVersion, "capabilities": map[string]any{},
				"serverInfo": map[string]any{"name": "stub", "version": "1"}}})
		case req.Params.Name == "hang":
			hung = append(hung, *req.ID)
		default:
			write(map[string]any{"jsonrpc": "2.0", "method": "notifications/progress",
				"params": map[string]any{"progress": 1}})
			for _, id := range hung {
				write(textReply(id, "late answer"))
			}
			hung = nil
			write(textReply(*req.ID, "answer to "+req.Params.Name))
		}
	}
	os.Exit(0)
}

// #221 over a real stdio server: a hung call is cancelled promptly and the
// next call gets its own answer, not the late one sent just before it.
func TestClient_StdioCallToolHonoursContext(t *testing.T) {
	t.Setenv(stubServerEnv, "1")
	tr, err := NewStdioTransport(os.Args[0], []string{"-test.run=^TestMCPStubServer$"}, nil)
	require.NoError(t, err)
	c := NewClient(tr, "celeste", "test")
	t.Cleanup(func() { _ = c.Close() })
	require.NoError(t, c.Initialize(context.Background()))

	for i := range 3 {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		start := time.Now()
		_, err := c.CallTool(ctx, "hang", nil)
		cancel()
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Less(t, time.Since(start), 2*time.Second)

		ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
		name := fmt.Sprintf("next%d", i)
		text, err := c.CallTool(ctx, name, nil)
		cancel()
		require.NoError(t, err)
		require.Equal(t, "answer to "+name, text)
	}
}
