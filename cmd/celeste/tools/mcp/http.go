package mcp

import (
	"bufio"
	"bytes"
	"context"
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
	return &HTTPTransport{url: url, client: newMCPHTTPClient()}, nil
}

// SetProtocolVersion sets the value sent as the MCP-Protocol-Version header.
func (t *HTTPTransport) SetProtocolVersion(v string) {
	t.mu.Lock()
	t.protoVer = v
	t.mu.Unlock()
}

func (t *HTTPTransport) newPost(ctx context.Context, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(body))
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

func (t *HTTPTransport) post(ctx context.Context, body []byte) error {
	req, err := t.newPost(ctx, body)
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
	data, err := readLimited(resp.Body)
	if err != nil {
		return fmt.Errorf("read http response: %w", err)
	}
	var r Response
	if err := json.Unmarshal(data, &r); err != nil {
		return fmt.Errorf("decode http response: %w", err)
	}
	return t.enqueue(&r)
}

// drainSSE reads an SSE stream, queuing each JSON-RPC response carried on a
// `data:` line. Non-response events (notifications/pings) are skipped.
//
// The stream is capped at maxResponseBytes in all, and at maxQueuedResponses
// queued responses (Aikido 806869944).
func (t *HTTPTransport) drainSSE(body io.Reader) error {
	lr := &io.LimitedReader{R: body, N: int64(maxResponseBytes) + 1}
	sc := bufio.NewScanner(lr)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		if lr.N <= 0 {
			return errResponseTooLarge()
		}
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
		if err := t.enqueue(&r); err != nil {
			return err
		}
	}
	if lr.N <= 0 {
		return errResponseTooLarge()
	}
	return sc.Err()
}

// enqueue queues r, failing once maxQueuedResponses are waiting.
func (t *HTTPTransport) enqueue(r *Response) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.queue) >= maxQueuedResponses {
		return fmt.Errorf("MCP server sent more than %d unread responses", maxQueuedResponses)
	}
	t.queue = append(t.queue, r)
	return nil
}

// Send POSTs a request and queues the resulting response(s).
func (t *HTTPTransport) Send(req *Request) error {
	return t.SendContext(context.Background(), req)
}

// SendContext is Send, abandoned when ctx is done. The server answers inside
// the POST, so this bounds the whole call.
func (t *HTTPTransport) SendContext(ctx context.Context, req *Request) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	return t.post(ctx, body)
}

// SendNotification POSTs a notification; no response is queued.
func (t *HTTPTransport) SendNotification(notif *Notification) error {
	return t.SendNotificationContext(context.Background(), notif)
}

// SendNotificationContext is SendNotification, abandoned when ctx is done.
func (t *HTTPTransport) SendNotificationContext(ctx context.Context, notif *Notification) error {
	body, err := json.Marshal(notif)
	if err != nil {
		return err
	}
	req, err := t.newPost(ctx, body)
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
