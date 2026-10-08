package mcp

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withResponseLimit lowers maxResponseBytes for one test.
func withResponseLimit(t *testing.T, n int) {
	t.Helper()
	old := maxResponseBytes
	maxResponseBytes = n
	t.Cleanup(func() { maxResponseBytes = old })
}

func bigResult(n int) string {
	return `{"jsonrpc":"2.0","id":1,"result":"` + strings.Repeat("a", n) + `"}`
}

// TestHTTPTransportRejectsOversizedJSON: a JSON response over the limit
// fails the call instead of being decoded whole (Aikido 806869944).
func TestHTTPTransportRejectsOversizedJSON(t *testing.T) {
	withResponseLimit(t, 1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, bigResult(8192))
	}))
	defer srv.Close()
	tr, err := NewHTTPTransport(srv.URL)
	require.NoError(t, err)
	err = tr.Send(&Request{JSONRPC: "2.0", ID: 1, Method: "tools/list"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too large")
}

// TestHTTPTransportBoundsSSEStream: an SSE response stream over the byte
// limit, or carrying more responses than the queue holds, fails the call
// (Aikido 806869944).
func TestHTTPTransportBoundsSSEStream(t *testing.T) {
	withResponseLimit(t, 4096)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for i := 0; i < 200; i++ {
			fmt.Fprintf(w, "data: %s\n\n", bigResult(100))
		}
	}))
	defer srv.Close()
	tr, err := NewHTTPTransport(srv.URL)
	require.NoError(t, err)
	err = tr.Send(&Request{JSONRPC: "2.0", ID: 1, Method: "tools/list"})
	require.Error(t, err)
	tr.mu.Lock()
	n := len(tr.queue)
	tr.mu.Unlock()
	assert.LessOrEqual(t, n, maxQueuedResponses)
}

// sseEndpointServer serves an SSE stream announcing endpoint, and answers
// POSTs with postStatus (and a Location of redirect, when set).
func sseEndpointServer(t *testing.T, endpoint func(self string) string, redirect string) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", endpoint(srv.URL))
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		if redirect != "" {
			http.Redirect(w, r, redirect, http.StatusTemporaryRedirect)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func countingServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

// waitEndpoint waits until the transport has read the endpoint event.
func waitEndpoint(t *testing.T, tr *SSETransport) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		tr.mu.Lock()
		got := tr.postURL != "" || tr.endpointErr != nil
		tr.mu.Unlock()
		if got {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no endpoint event read")
}

// TestSSETransportRejectsCrossOriginEndpoint: a server-supplied POST
// endpoint on another origin is refused; nothing is POSTed there
// (Aikido 806869691).
func TestSSETransportRejectsCrossOriginEndpoint(t *testing.T) {
	other, hits := countingServer(t)
	srv := sseEndpointServer(t, func(string) string { return other.URL + "/message" }, "")
	tr, err := NewSSETransport(srv.URL + "/sse")
	require.NoError(t, err)
	defer tr.Close()
	waitEndpoint(t, tr)
	err = tr.Send(&Request{JSONRPC: "2.0", ID: 1, Method: "ping"})
	assert.Error(t, err)
	assert.Equal(t, int32(0), hits.Load(), "a request was POSTed to another origin")
}

// TestSSETransportAcceptsSameOriginAbsoluteEndpoint: an absolute endpoint on
// the server's own origin still works.
func TestSSETransportAcceptsSameOriginAbsoluteEndpoint(t *testing.T) {
	srv := sseEndpointServer(t, func(self string) string { return self + "/message?id=1" }, "")
	tr, err := NewSSETransport(srv.URL + "/sse")
	require.NoError(t, err)
	defer tr.Close()
	waitEndpoint(t, tr)
	require.NoError(t, tr.Send(&Request{JSONRPC: "2.0", ID: 1, Method: "ping"}))
	tr.mu.Lock()
	assert.Equal(t, srv.URL+"/message?id=1", tr.postURL)
	tr.mu.Unlock()
}

// TestMCPTransportsRefuseCrossOriginRedirects: a POST redirected to another
// origin is not followed, on the SSE and the HTTP transport
// (Aikido 806869691).
func TestMCPTransportsRefuseCrossOriginRedirects(t *testing.T) {
	other, hits := countingServer(t)

	srv := sseEndpointServer(t, func(string) string { return "/message" }, other.URL+"/x")
	tr, err := NewSSETransport(srv.URL + "/sse")
	require.NoError(t, err)
	defer tr.Close()
	waitEndpoint(t, tr)
	_ = tr.Send(&Request{JSONRPC: "2.0", ID: 1, Method: "ping"})

	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/y", http.StatusPermanentRedirect)
	}))
	defer hs.Close()
	ht, err := NewHTTPTransport(hs.URL)
	require.NoError(t, err)
	_ = ht.Send(&Request{JSONRPC: "2.0", ID: 1, Method: "ping"})

	assert.Equal(t, int32(0), hits.Load(), "a redirect to another origin was followed")
}

// TestSSETransportOversizedEventFailsReceive: an event line over the limit
// ends the stream, and Receive reports it instead of waiting forever
// (Aikido 806869944).
func TestSSETransportOversizedEventFailsReceive(t *testing.T) {
	withResponseLimit(t, 1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "event: endpoint\ndata: /message\n\n")
		fmt.Fprintf(w, "event: message\ndata: %s\n\n", bigResult(8192))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	tr, err := NewSSETransport(srv.URL + "/sse")
	require.NoError(t, err)
	defer tr.Close()
	done := make(chan error, 1)
	go func() { _, err := tr.Receive(); done <- err }()
	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Receive blocked after the stream ended on an oversized event")
	}
}

// TestSSETransportBoundsQueuedBytes: responses no call has read yet are
// capped at maxResponseBytes in all, not only per response; a server that
// pushes more ends the stream (Aikido 806869944).
func TestSSETransportBoundsQueuedBytes(t *testing.T) {
	withResponseLimit(t, 4096)
	event := bigResult(1000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "event: endpoint\ndata: /message\n\n")
		for i := 0; i < 150; i++ {
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", event)
		}
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	tr, err := NewSSETransport(srv.URL + "/sse")
	require.NoError(t, err)
	defer tr.Close()
	select {
	case <-tr.streamEnded:
	case <-time.After(2 * time.Second):
	}
	assert.LessOrEqual(t, len(tr.responseCh)*len(event), maxResponseBytes,
		"unread responses exceed the byte cap")
	// Every queued response is still delivered, then the stream's error.
	for {
		_, err := tr.Receive()
		if err != nil {
			assert.Contains(t, err.Error(), "unread")
			break
		}
	}
}

// TestHTTPTransportBoundsQueuedBytes: responses left unread across POSTs are
// capped at maxResponseBytes in all (Aikido 806869944).
func TestHTTPTransportBoundsQueuedBytes(t *testing.T) {
	withResponseLimit(t, 4096)
	body := bigResult(1000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}))
	defer srv.Close()
	tr, err := NewHTTPTransport(srv.URL)
	require.NoError(t, err)
	var sendErr error
	for i := 0; i < 10 && sendErr == nil; i++ {
		sendErr = tr.Send(&Request{JSONRPC: "2.0", ID: 1, Method: "tools/list"})
	}
	require.Error(t, sendErr, "unread responses were queued without a byte cap")
	tr.mu.Lock()
	n := len(tr.queue)
	tr.mu.Unlock()
	assert.LessOrEqual(t, n*len(body), maxResponseBytes)
	// Reading frees the room again.
	for {
		if _, err := tr.Receive(); err != nil {
			break
		}
	}
	require.NoError(t, tr.Send(&Request{JSONRPC: "2.0", ID: 1, Method: "tools/list"}))
}

// TestOriginChecksNormaliseDefaultPorts: an explicit default port is the
// same origin as none, and a same-host http to https upgrade redirect is
// followed; another host or port still is not.
func TestOriginChecksNormaliseDefaultPorts(t *testing.T) {
	u := func(s string) *url.URL {
		t.Helper()
		p, err := url.Parse(s)
		require.NoError(t, err)
		return p
	}
	assert.True(t, sameOrigin(u("https://h/sse"), u("https://h:443/msg")))
	assert.True(t, sameOrigin(u("http://H:80/sse"), u("http://h/msg")))
	assert.False(t, sameOrigin(u("https://h/sse"), u("https://h:8443/msg")))
	assert.False(t, sameOrigin(u("https://h/sse"), u("https://other/msg")))
	assert.False(t, sameOrigin(u("http://h/sse"), u("https://h/msg")))

	_, err := resolveEndpoint("https://h/sse", "https://h:443/msg")
	assert.NoError(t, err)

	assert.True(t, redirectAllowed(u("http://h/sse"), u("https://h/sse")), "same-host https upgrade")
	assert.True(t, redirectAllowed(u("http://h:80/sse"), u("https://h:443/sse")))
	assert.False(t, redirectAllowed(u("https://h/sse"), u("http://h/sse")), "a downgrade")
	assert.False(t, redirectAllowed(u("http://h/sse"), u("https://other/sse")))
	assert.False(t, redirectAllowed(u("http://h:8080/sse"), u("https://h:8443/sse")))
}
