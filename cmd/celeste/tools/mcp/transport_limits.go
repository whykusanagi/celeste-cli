package mcp

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// maxResponseBytes caps one MCP response (a stdio line, an HTTP body, an SSE
// response stream), so a configured server cannot drive the client out of
// memory (Aikido 806869944). A var so tests can lower it.
var maxResponseBytes = 16 << 20

// maxQueuedResponses caps the responses one HTTP POST may queue.
const maxQueuedResponses = 256

// queuedResponse is a decoded response waiting for Receive, with the size
// of its encoding: the bytes queued unread are capped at maxResponseBytes in
// all, not only per response (Aikido 806869944).
type queuedResponse struct {
	resp *Response
	size int
}

// errTooManyUnread reports a server that sent more unread responses than
// the queue holds.
func errTooManyUnread() error {
	return fmt.Errorf("MCP server sent more than %d bytes of unread responses", maxResponseBytes)
}

// errResponseTooLarge reports a response over maxResponseBytes.
func errResponseTooLarge() error {
	return fmt.Errorf("MCP response too large (over %d bytes)", maxResponseBytes)
}

// readLimited reads r whole, failing once it passes maxResponseBytes.
func readLimited(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, int64(maxResponseBytes)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxResponseBytes {
		return nil, errResponseTooLarge()
	}
	return data, nil
}

// sameOrigin reports whether a and b have the same scheme and host:port.
func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host)
}

// newMCPHTTPClient is the HTTP client of the SSE and HTTP transports. It
// follows a redirect only on the original request's origin, so a server
// cannot bounce a POST to a host it could not reach itself
// (Aikido 806869691).
func newMCPHTTPClient() *http.Client {
	return &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		if !sameOrigin(via[0].URL, req.URL) {
			return fmt.Errorf("refusing redirect to another origin (%s)", req.URL.Host)
		}
		return nil
	}}
}
