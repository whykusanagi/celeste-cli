// cmd/celeste/tools/mcp/sse.go
package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
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
}

// NewSSETransport connects to an MCP server's SSE endpoint.
// url is the base URL of the MCP server (e.g., "http://localhost:3000/sse").
func NewSSETransport(url string) (*SSETransport, error) {
	t := &SSETransport{
		baseURL:    url,
		client:     &http.Client{},
		responseCh: make(chan *Response, 100),
		done:       make(chan struct{}),
	}

	// Connect to the SSE stream in a goroutine
	go t.connectSSE()

	return t, nil
}

// connectSSE establishes the SSE connection and reads events.
func (t *SSETransport) connectSSE() {
	resp, err := t.client.Get(t.baseURL)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
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
				t.mu.Lock()
				if strings.HasPrefix(data, "/") {
					// Relative path -- combine with base URL
					// Extract scheme+host from baseURL
					parts := strings.SplitN(t.baseURL, "://", 2)
					if len(parts) == 2 {
						hostEnd := strings.Index(parts[1], "/")
						if hostEnd == -1 {
							t.postURL = t.baseURL + data
						} else {
							t.postURL = parts[0] + "://" + parts[1][:hostEnd] + data
						}
					}
				} else {
					t.postURL = data
				}
				t.mu.Unlock()

			case "message":
				var rpcResp Response
				if err := json.Unmarshal([]byte(data), &rpcResp); err == nil {
					t.responseCh <- &rpcResp
				}
			}

			eventType = ""
			continue
		}
	}
}

// Send sends a JSON-RPC request via HTTP POST to the server's endpoint.
func (t *SSETransport) Send(req *Request) error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return fmt.Errorf("transport is closed")
	}
	postURL := t.postURL
	t.mu.Unlock()

	if postURL == "" {
		// If we have not yet received the endpoint event, POST to base URL
		postURL = t.baseURL
	}

	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	httpResp, err := t.client.Post(postURL, "application/json", bytes.NewReader(data))
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
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return fmt.Errorf("transport is closed")
	}
	postURL := t.postURL
	t.mu.Unlock()

	if postURL == "" {
		postURL = t.baseURL
	}

	data, err := json.Marshal(notif)
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}

	httpResp, err := t.client.Post(postURL, "application/json", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("POST notification: %w", err)
	}
	defer httpResp.Body.Close()

	return nil
}

// Receive reads the next JSON-RPC response from the SSE event stream.
func (t *SSETransport) Receive() (*Response, error) {
	resp, ok := <-t.responseCh
	if !ok {
		return nil, fmt.Errorf("transport closed")
	}
	return resp, nil
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
	return nil
}
