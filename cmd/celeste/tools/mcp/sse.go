// cmd/celeste/tools/mcp/sse.go
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// SSETransport communicates with an MCP server over HTTP Server-Sent Events.
// Requests are sent via POST to the server's endpoint URL.
// Responses are received via an SSE event stream from a GET connection.
type SSETransport struct {
	baseURL    string
	postURL    string // discovered from SSE endpoint event
	client     *http.Client
	responseCh chan *Response
	mu         sync.Mutex
	closed     bool
	done       chan struct{}
	cancel     context.CancelFunc // ends the GET stream started by connectSSE
	// endpointErr is set when the server announced a POST endpoint on
	// another origin; Send then fails rather than POST there.
	endpointErr error
}

// NewSSETransport connects to an MCP server's SSE endpoint.
// url is the base URL of the MCP server (e.g., "http://localhost:3000/sse").
func NewSSETransport(url string) (*SSETransport, error) {
	ctx, cancel := context.WithCancel(context.Background())
	t := &SSETransport{
		baseURL:    url,
		client:     newMCPHTTPClient(),
		responseCh: make(chan *Response, 100),
		done:       make(chan struct{}),
		cancel:     cancel,
	}

	// Connect to the SSE stream in a goroutine
	go t.connectSSE(ctx)

	return t, nil
}

// connectSSE establishes the SSE connection and reads events.
func (t *SSETransport) connectSSE(ctx context.Context) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.baseURL, nil)
	if err != nil {
		return
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), maxResponseBytes)
	var eventType string

	for scanner.Scan() {
		select {
		case <-t.done:
			return
		default:
		}

		line := scanner.Text()

		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
			continue
		}

		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")

			switch eventType {
			case "endpoint":
				// The server tells us where to POST requests
				// The server tells us where to POST. Only an endpoint on
				// the base URL's own origin is used (Aikido 806869691).
				postURL, err := resolveEndpoint(t.baseURL, data)
				t.mu.Lock()
				t.postURL, t.endpointErr = postURL, err
				t.mu.Unlock()

			case "message":
				var rpcResp Response
				if err := json.Unmarshal([]byte(data), &rpcResp); err == nil {
					select {
					case t.responseCh <- &rpcResp:
					case <-t.done:
						return
					}
				}
			}

			eventType = ""
			continue
		}
	}
}

// Send sends a JSON-RPC request via HTTP POST to the server's endpoint.
func (t *SSETransport) Send(req *Request) error {
	return t.SendContext(context.Background(), req)
}

// SendContext is Send, abandoned when ctx is done.
func (t *SSETransport) SendContext(ctx context.Context, req *Request) error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return fmt.Errorf("transport is closed")
	}
	postURL, endpointErr := t.postURL, t.endpointErr
	t.mu.Unlock()
	if endpointErr != nil {
		return endpointErr
	}

	if postURL == "" {
		// If we have not yet received the endpoint event, POST to base URL
		postURL = t.baseURL
	}

	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	httpResp, err := t.post(ctx, postURL, data)
	if err != nil {
		return fmt.Errorf("POST request: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode >= 400 {
		return fmt.Errorf("POST returned status %d", httpResp.StatusCode)
	}

	return nil
}

// SendNotification sends a JSON-RPC notification via HTTP POST.
func (t *SSETransport) SendNotification(notif *Notification) error {
	return t.SendNotificationContext(context.Background(), notif)
}

// SendNotificationContext is SendNotification, abandoned when ctx is done.
func (t *SSETransport) SendNotificationContext(ctx context.Context, notif *Notification) error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return fmt.Errorf("transport is closed")
	}
	postURL, endpointErr := t.postURL, t.endpointErr
	t.mu.Unlock()
	if endpointErr != nil {
		return endpointErr
	}

	if postURL == "" {
		postURL = t.baseURL
	}

	data, err := json.Marshal(notif)
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}

	httpResp, err := t.post(ctx, postURL, data)
	if err != nil {
		return fmt.Errorf("POST notification: %w", err)
	}
	defer httpResp.Body.Close()

	return nil
}

func (t *SSETransport) post(ctx context.Context, url string, data []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return t.client.Do(req)
}

// Receive reads the next JSON-RPC response from the SSE event stream.
// It also returns once the transport is closed, so a Receive the client left
// running for a cancelled call doesn't outlive Close.
func (t *SSETransport) Receive() (*Response, error) {
	select {
	case resp, ok := <-t.responseCh:
		if !ok {
			return nil, fmt.Errorf("transport closed")
		}
		return resp, nil
	case <-t.done:
		return nil, fmt.Errorf("transport closed")
	}
}

// Close shuts down the SSE connection.
func (t *SSETransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil
	}
	t.closed = true
	close(t.done)
	t.cancel()
	return nil
}

// resolveEndpoint resolves the endpoint event's data against base and
// returns it only when it is on base's origin (scheme and host).
func resolveEndpoint(base, endpoint string) (string, error) {
	b, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("parse MCP SSE URL: %w", err)
	}
	e, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return "", fmt.Errorf("MCP SSE server sent an invalid endpoint: %w", err)
	}
	r := b.ResolveReference(e)
	if !sameOrigin(b, r) {
		return "", fmt.Errorf("refusing the MCP SSE server's POST endpoint on another origin (%s)", r.Host)
	}
	return r.String(), nil
}
