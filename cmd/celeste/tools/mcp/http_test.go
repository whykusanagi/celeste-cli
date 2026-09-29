package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

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

// #221 over Streamable HTTP: the server answers inside the POST, so a call
// to a server that never answers must be cut off by ctx, and must not leave
// the client locked for the next call.
func TestClient_HTTPCallToolHonoursContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     *int64 `json:"id"`
			Method string `json:"method"`
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		if req.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		if req.Params.Name == "hang" {
			<-r.Context().Done()
			return
		}
		var result any = map[string]any{"content": []map[string]any{{"type": "text", "text": "answer to " + req.Params.Name}}}
		if req.Method == "initialize" {
			result = map[string]any{"protocolVersion": preferredProtocolVersion, "capabilities": map[string]any{},
				"serverInfo": map[string]any{"name": "stub", "version": "1"}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": *req.ID, "result": result})
	}))
	defer srv.Close()

	tr, err := NewHTTPTransport(srv.URL)
	require.NoError(t, err)
	c := NewClient(tr, "celeste", "test")
	defer c.Close()
	require.NoError(t, c.Initialize(context.Background()))

	for i := range 3 {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		start := time.Now()
		_, err := c.CallTool(ctx, "hang", nil)
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("hung call err = %v, want DeadlineExceeded", err)
		}
		require.Less(t, time.Since(start), time.Second)

		ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
		name := fmt.Sprintf("next%d", i)
		text, err := c.CallTool(ctx, name, nil)
		cancel()
		require.NoError(t, err)
		require.Equal(t, "answer to "+name, text)
	}
}
