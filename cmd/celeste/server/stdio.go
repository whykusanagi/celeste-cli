package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools/mcp"
)

// serveStdio runs the MCP server over stdin/stdout.
// Each line on stdin is a JSON-RPC request. Responses are written as
// single-line JSON to stdout. This is the transport used when Celeste is
// launched as a child process by Claude Code or similar MCP clients.
func (s *Server) serveStdio(ctx context.Context) error {
	return s.serveStdioStreams(ctx, os.Stdin, os.Stdout)
}

// serveStdioStreams is the testable core of the stdio transport.
// It reads from r and writes to w, allowing tests to substitute buffers.
func (s *Server) serveStdioStreams(ctx context.Context, r io.Reader, w io.Writer) error {
	scanner := bufio.NewScanner(r)
	// MCP messages can be large (tool results with file contents).
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // up to 1MB per line

	log.Printf("[mcp-server] stdio transport started")

	for {
		select {
		case <-ctx.Done():
			log.Printf("[mcp-server] stdio transport shutting down")
			return ctx.Err()
		default:
		}

		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return fmt.Errorf("stdin read error: %w", err)
			}
			// EOF -- client disconnected
			log.Printf("[mcp-server] stdin closed (EOF)")
			return nil
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue // skip empty lines
		}

		// Parse the incoming JSON-RPC message. It could be a Request (has ID)
		// or a Notification (no ID). We try Request first since that is the
		// common case, then fall back to Notification.
		var req mcp.Request
		if err := json.Unmarshal(line, &req); err != nil {
			// Write parse error response
			errResp := s.errorResponse(0, -32700, "parse error", nil)
			_ = s.writeJSON(w, errResp)
			continue
		}

		// Notifications have Method set but ID == 0 and no response is expected.
		if req.Method == "notifications/initialized" || req.Method == "notifications/cancelled" {
			// Handle as notification -- dispatch but ignore response
			_, _ = s.dispatch(ctx, &req)
			continue
		}

		resp, err := s.dispatch(ctx, &req)
		if err != nil {
			errResp := s.errorResponse(req.ID, -32603, err.Error(), nil)
			_ = s.writeJSON(w, errResp)
			continue
		}

		// dispatch returns nil for notifications
		if resp == nil {
			continue
		}

		if err := s.writeJSON(w, resp); err != nil {
			return fmt.Errorf("stdout write error: %w", err)
		}
	}
}

// writeJSON marshals v as a single JSON line and writes it to w.
func (s *Server) writeJSON(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal response: %w", err)
	}
	data = append(data, '\n')
	_, err = w.Write(data)
	return err
}
