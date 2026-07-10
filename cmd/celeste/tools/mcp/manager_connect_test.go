package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

func TestManager_connectClient_TracksNames(t *testing.T) {
	registry := tools.NewRegistry()
	mgr := NewManager("", registry)

	mt := &mockTransport{
		responses: []*Response{
			makeInitResponse(),
			makeToolsListResponse("srv__t1", "srv__t2"),
		},
	}
	client := NewClient(mt, "celeste", "1.0")

	err := mgr.connectClient(context.Background(), "srv", client, "stdio")
	require.NoError(t, err)

	assert.True(t, mgr.IsConnected("srv"))
	assert.Equal(t, 2, registry.Count())
	assert.ElementsMatch(t, []string{"srv__t1", "srv__t2"}, mgr.toolNames["srv"])
}

func TestManager_Disconnect_RemovesTools(t *testing.T) {
	registry := tools.NewRegistry()
	mgr := NewManager("", registry)

	mt := &mockTransport{responses: []*Response{
		makeInitResponse(),
		makeToolsListResponse("srv__t1", "srv__t2"),
	}}
	client := NewClient(mt, "celeste", "1.0")
	require.NoError(t, mgr.connectClient(context.Background(), "srv", client, "stdio"))
	require.Equal(t, 2, registry.Count())

	require.NoError(t, mgr.Disconnect("srv"))
	assert.False(t, mgr.IsConnected("srv"))
	assert.Equal(t, 0, registry.Count())
	assert.True(t, mt.closed)

	// Disconnecting an unknown server is a no-op.
	assert.NoError(t, mgr.Disconnect("ghost"))
}
