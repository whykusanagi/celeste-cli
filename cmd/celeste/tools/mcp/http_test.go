package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPTransport_JSONResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "2025-06-18", r.Header.Get("MCP-Protocol-Version"))
		var req Request
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Response{
			JSONRPC: "2.0",
			ID:      json.Number(strconv.FormatInt(req.ID, 10)),
			Result:  json.RawMessage(`{"ok":true}`),
		})
	}))
	defer srv.Close()

	tr, err := NewHTTPTransport(srv.URL)
	require.NoError(t, err)
	tr.SetProtocolVersion("2025-06-18")

	req, err := NewRequest("ping", map[string]any{})
	require.NoError(t, err)
	require.NoError(t, tr.Send(req))

	resp, err := tr.Receive()
	require.NoError(t, err)
	require.Nil(t, resp.Error)
	assert.JSONEq(t, `{"ok":true}`, string(resp.Result))
}

func TestHTTPTransport_SSEResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req Request
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "data: {\"jsonrpc\":\"2.0\",\"id\":%d,\"result\":{\"ok\":true}}\n\n", req.ID)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	defer srv.Close()

	tr, err := NewHTTPTransport(srv.URL)
	require.NoError(t, err)

	req, err := NewRequest("ping", map[string]any{})
	require.NoError(t, err)
	require.NoError(t, tr.Send(req))

	resp, err := tr.Receive()
	require.NoError(t, err)
	assert.JSONEq(t, `{"ok":true}`, string(resp.Result))
}
