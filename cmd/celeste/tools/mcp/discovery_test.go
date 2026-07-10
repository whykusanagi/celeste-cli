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
	names, err := DiscoverAndRegister(context.Background(), client, registry, "test-server")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"tool_a", "tool_b"}, names)

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
	_, err := DiscoverAndRegister(context.Background(), client, registry, "empty-server")
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
	_, err := DiscoverAndRegister(context.Background(), client, registry, "bad-server")
	assert.Error(t, err)
	assert.Equal(t, 0, registry.Count())
}

func TestDiscoverAndRegister_MarksHidden(t *testing.T) {
	transport := &mockTransport{
		responses: []*Response{
			makeInitResponse(),
			makeToolsListResponse("remote_tool"),
		},
	}
	client := NewClient(transport, "celeste", "1.0")
	require.NoError(t, client.Initialize(context.Background()))

	registry := tools.NewRegistry()
	registry.SetDiscoveryMode(true)
	_, err := DiscoverAndRegister(context.Background(), client, registry, "srv")
	require.NoError(t, err)

	// Registered but hidden under discovery mode.
	assert.Contains(t, allNames(registry.GetAll()), "remote_tool")
	assert.NotContains(t, allNames(registry.GetTools(tools.ModeChat)), "remote_tool")
}

func allNames(ts []tools.Tool) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Name()
	}
	return out
}
