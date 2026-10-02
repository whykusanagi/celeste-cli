package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// A tool mirrored into a nested run's registry keeps working after its
// server is disconnected and connected again (/mcp): it calls the server's
// current client, found by name through the manager, not the client it was
// registered with (2.0 F2e). Before, it held the closed client.
func TestMirroredToolUsesTheReconnectedClient(t *testing.T) {
	src := tools.NewRegistry()
	mgr := NewManager("", src)
	connect := func(extra ...*Response) *mockTransport {
		mt := &mockTransport{responses: append([]*Response{makeInitResponse(), makeToolsListResponse("echo")}, extra...)}
		require.NoError(t, mgr.connectClient(context.Background(), "srv", NewClient(mt, "celeste", "1.0"), "stdio", false))
		return mt
	}
	old := connect()
	dst := tools.NewRegistry()
	require.Equal(t, 1, mgr.RegisterInto(dst))
	mirrored, ok := dst.Get(ToolName("srv", "echo"))
	require.True(t, ok)

	require.NoError(t, mgr.Disconnect("srv"))
	require.True(t, old.closed)
	res, err := mirrored.Execute(context.Background(), nil, nil)
	require.NoError(t, err)
	assert.True(t, res.Error, "a call to a disconnected server succeeded")
	assert.Contains(t, res.Content, "not connected")

	connect(textReply(0, "from the new client"))
	res, err = mirrored.Execute(context.Background(), nil, nil)
	require.NoError(t, err)
	assert.False(t, res.Error, "the mirrored tool kept the closed client: %s", res.Content)
	assert.Equal(t, "from the new client", res.Content)
}

// A tool built outside a manager (NewMCPTool) still calls its own client.
func TestStandaloneToolCallsItsOwnClient(t *testing.T) {
	mt := &mockTransport{responses: []*Response{textReply(0, "own")}}
	c := NewClient(mt, "celeste", "1.0")
	c.initialized = true
	tool := NewMCPTool(MCPToolDef{Name: "echo"}, c, "srv")
	res, err := tool.Execute(context.Background(), nil, nil)
	require.NoError(t, err)
	assert.False(t, res.Error, res.Content)
	assert.Equal(t, "own", res.Content)
}
