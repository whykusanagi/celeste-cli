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
