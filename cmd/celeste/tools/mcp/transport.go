// cmd/celeste/tools/mcp/transport.go
package mcp

// Transport is the abstraction over MCP communication channels.
// Implementations handle the physical sending and receiving of JSON-RPC
// messages over stdio (child process) or SSE (HTTP).
type Transport interface {
	// Send sends a JSON-RPC request to the server.
	// For requests, the caller should subsequently call Receive to get the response.
	Send(req *Request) error

	// SendNotification sends a JSON-RPC notification (no response expected).
	SendNotification(notif *Notification) error

	// Receive reads the next JSON-RPC response from the server.
	// Blocks until a response is available or the transport is closed.
	Receive() (*Response, error)

	// Close shuts down the transport, releasing all resources.
	Close() error
}
