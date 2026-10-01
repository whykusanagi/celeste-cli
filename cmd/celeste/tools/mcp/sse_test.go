// cmd/celeste/tools/mcp/sse_test.go
package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockMCPSSEServer simulates an MCP server over SSE.
// POST requests enqueue a response; the SSE stream delivers them as events.
type mockMCPSSEServer struct {
	eventCh chan string
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

// Close ends a Receive left running on a goroutine and cancels the GET
// stream, so neither outlives the transport.
func TestSSETransport_CloseEndsReceiveAndStream(t *testing.T) {
	streamOpen, streamEnded := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", r.URL.Path)
		w.(http.Flusher).Flush()
		close(streamOpen)
		<-r.Context().Done()
		close(streamEnded)
	}))
	defer server.Close()

	transport, err := NewSSETransport(server.URL)
	require.NoError(t, err)

	received := make(chan error, 1)
	go func() {
		_, err := transport.Receive()
		received <- err
	}()

	select {
	case <-streamOpen:
	case <-time.After(2 * time.Second):
		t.Fatal("SSE GET stream never opened")
	}
	// Let the reader take the endpoint event and block on the idle stream,
	// where only cancelling the GET can end it.
	for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		transport.mu.Lock()
		got := transport.postURL
		transport.mu.Unlock()
		if got != "" || time.Now().After(deadline) {
			break
		}
	}
	time.Sleep(50 * time.Millisecond)
	require.NoError(t, transport.Close())
	select {
	case err := <-received:
		assert.Error(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Receive still blocked after Close")
	}
	select {
	case <-streamEnded:
	case <-time.After(2 * time.Second):
		t.Fatal("SSE GET stream still open after Close")
	}
}
