package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

// HTTPTransport speaks MCP over Streamable HTTP: each outbound message is POSTed
// to the endpoint; the response is either a single JSON object or an SSE stream
// of JSON-RPC messages. Decoded responses are queued for Receive to drain.
type HTTPTransport struct {
	url      string
	client   *http.Client
	protoVer string
	mu       sync.Mutex
	queue    []*Response
}

// NewHTTPTransport creates a Streamable-HTTP transport for the given endpoint.
func NewHTTPTransport(url string) (*HTTPTransport, error) {
	if url == "" {
		return nil, fmt.Errorf("http transport requires a URL")
	}
	return &HTTPTransport{url: url, client: &http.Client{}}, nil
}

// SetProtocolVersion sets the value sent as the MCP-Protocol-Version header.
func (t *HTTPTransport) SetProtocolVersion(v string) {
	t.mu.Lock()
	t.protoVer = v
	t.mu.Unlock()
}

func (t *HTTPTransport) newPost(body []byte) (*http.Request, error) {
	req, err := http.NewRequest(http.MethodPost, t.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	t.mu.Lock()
	if t.protoVer != "" {
		req.Header.Set("MCP-Protocol-Version", t.protoVer)
	}
	t.mu.Unlock()
	return req, nil
}

func (t *HTTPTransport) post(body []byte) error {
	req, err := t.newPost(body)
	if err != nil {
		return err
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "text/event-stream") {
		return t.drainSSE(resp.Body)
	}
	var r Response
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return fmt.Errorf("decode http response: %w", err)
	}
	t.enqueue(&r)
	return nil
}

// drainSSE reads an SSE stream, queuing each JSON-RPC response carried on a
// `data:` line. Non-response events (notifications/pings) are skipped.
func (t *HTTPTransport) drainSSE(body io.Reader) error {
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}
		var r Response
		if err := json.Unmarshal([]byte(payload), &r); err != nil {
			continue
		}
		t.enqueue(&r)
	}
	return sc.Err()
}

func (t *HTTPTransport) enqueue(r *Response) {
	t.mu.Lock()
	t.queue = append(t.queue, r)
	t.mu.Unlock()
}

// Send POSTs a request and queues the resulting response(s).
func (t *HTTPTransport) Send(req *Request) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	return t.post(body)
}

// SendNotification POSTs a notification; no response is queued.
func (t *HTTPTransport) SendNotification(notif *Notification) error {
	body, err := json.Marshal(notif)
	if err != nil {
		return err
	}
	req, err := t.newPost(body)
	if err != nil {
		return err
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

// Receive drains the next queued response.
func (t *HTTPTransport) Receive() (*Response, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.queue) == 0 {
		return nil, fmt.Errorf("no queued response")
	}
	r := t.queue[0]
	t.queue = t.queue[1:]
	return r, nil
}

// Close is a no-op for the stateless HTTP transport.
func (t *HTTPTransport) Close() error { return nil }
