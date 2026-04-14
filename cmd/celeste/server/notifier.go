// Progress notification plumbing for the MCP server. Tool handlers that
// need to stream status back to the client (long reindex runs, expensive
// codegraph queries, etc.) pull a Notifier out of the request context
// and call it with JSON-RPC method + params.
//
// The transport layer binds a concrete Notifier implementation to the
// context before dispatching the request — stdio writes notifications
// as single-line JSON to the same stdout the response will later use;
// SSE pushes them to the per-connection event stream. Handlers never
// talk to the transport directly.
//
// Handlers that don't need progress simply don't pull the notifier.
// The zero-cost path is a nil lookup.
package server

import (
	"context"
	"encoding/json"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools/mcp"
)

// Notifier sends a single JSON-RPC notification back to the MCP client.
// Used by tool handlers for progress updates and interim status.
type Notifier func(method string, params any) error

type notifierKey struct{}

// WithNotifier attaches a Notifier to the context so downstream tool
// handlers can stream progress back to the client.
func WithNotifier(ctx context.Context, n Notifier) context.Context {
	if n == nil {
		return ctx
	}
	return context.WithValue(ctx, notifierKey{}, n)
}

// NotifierFromContext returns the Notifier attached to ctx, or nil if
// none is present. Handlers MUST nil-check the return value.
func NotifierFromContext(ctx context.Context) Notifier {
	if v, ok := ctx.Value(notifierKey{}).(Notifier); ok {
		return v
	}
	return nil
}

// progressToken is the opaque token the client passes in request._meta
// so it can correlate progress notifications back to the originating
// tool call. Per the MCP progress spec, the server MUST only emit
// notifications/progress when a token was provided; without one we
// silently drop updates.
type progressToken = any

// progressTokenKey is a separate context key so handlers can look up
// the client-supplied progressToken and decide whether to stream.
type progressTokenKey struct{}

// WithProgressToken attaches the client-supplied progress token (if any)
// to the context.
func WithProgressToken(ctx context.Context, token progressToken) context.Context {
	if token == nil {
		return ctx
	}
	return context.WithValue(ctx, progressTokenKey{}, token)
}

// ProgressTokenFromContext returns the client-supplied progress token,
// or nil if the client didn't request progress.
func ProgressTokenFromContext(ctx context.Context) progressToken {
	return ctx.Value(progressTokenKey{})
}

// SendProgress is a convenience for tool handlers: if a notifier + token
// are both present, it dispatches a notifications/progress event with
// the given payload. Otherwise it's a no-op. Callers never need to
// branch on notifier presence themselves.
func SendProgress(ctx context.Context, message string, percent float64) {
	n := NotifierFromContext(ctx)
	token := ProgressTokenFromContext(ctx)
	if n == nil || token == nil {
		return
	}
	payload := map[string]any{
		"progressToken": token,
		"progress":      percent,
		"message":       message,
	}
	// Total is omitted — we don't generally know an upper bound.
	_ = n("notifications/progress", payload)
}

// makeStdioNotifier returns a Notifier that writes newline-delimited
// JSON to w. w must be the same stdout the transport loop uses for
// responses; since the stdio loop processes requests sequentially,
// notifications and responses interleave safely without locking.
func makeStdioNotifier(write func(v any) error) Notifier {
	return func(method string, params any) error {
		var raw json.RawMessage
		if params != nil {
			data, err := json.Marshal(params)
			if err != nil {
				return err
			}
			raw = data
		}
		notif := mcp.Notification{
			JSONRPC: "2.0",
			Method:  method,
			Params:  raw,
		}
		return write(notif)
	}
}
