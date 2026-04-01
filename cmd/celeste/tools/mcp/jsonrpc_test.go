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
