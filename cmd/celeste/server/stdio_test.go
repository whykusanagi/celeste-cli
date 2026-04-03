package server

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools/mcp"
)

func TestStdioFullCycle(t *testing.T) {
	srv := New(DefaultConfig())
	srv.RegisterTool(mcp.MCPToolDef{
		Name:        "test_echo",
		Description: "Echo",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"msg":{"type":"string"}}}`),
	}, func(ctx context.Context, args map[string]any) ([]ContentBlock, error) {
		msg, _ := args["msg"].(string)
		return []ContentBlock{{Type: "text", Text: msg}}, nil
	})

	// Build input: initialize, tools/list, tools/call
	var input bytes.Buffer
	writeRequest := func(id int64, method string, params any) {
		req := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
		if params != nil {
			p, _ := json.Marshal(params)
			req["params"] = json.RawMessage(p)
		}
		data, _ := json.Marshal(req)
		input.Write(data)
		input.WriteByte('\n')
	}

	writeRequest(1, "initialize", map[string]any{"protocolVersion": "2024-11-05"})
	writeRequest(2, "tools/list", map[string]any{})
	writeRequest(3, "tools/call", map[string]any{"name": "test_echo", "arguments": map[string]any{"msg": "hello"}})

	var output bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := srv.serveStdioStreams(ctx, &input, &output)
	if err != nil {
		t.Fatalf("serveStdioStreams error: %v", err)
	}

	// Parse responses
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 response lines, got %d: %s", len(lines), output.String())
	}

	// Check initialize response
	var initResp mcp.Response
	json.Unmarshal([]byte(lines[0]), &initResp)
	if initResp.Error != nil {
		t.Fatalf("initialize failed: %v", initResp.Error)
	}

	// Check tools/list response
	var listResp mcp.Response
	json.Unmarshal([]byte(lines[1]), &listResp)
	var toolsList struct {
		Tools []mcp.MCPToolDef `json:"tools"`
	}
	json.Unmarshal(listResp.Result, &toolsList)
	if len(toolsList.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(toolsList.Tools))
	}

	// Check tools/call response
	var callResp mcp.Response
	json.Unmarshal([]byte(lines[2]), &callResp)
	var callResult struct {
		Content []mcp.ContentBlock `json:"content"`
	}
	json.Unmarshal(callResp.Result, &callResult)
	if len(callResult.Content) != 1 || callResult.Content[0].Text != "hello" {
		t.Fatalf("unexpected call result: %+v", callResult)
	}
}

func TestStdioNotification(t *testing.T) {
	srv := New(DefaultConfig())

	var input bytes.Buffer
	notif := map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}
	data, _ := json.Marshal(notif)
	input.Write(data)
	input.WriteByte('\n')

	var output bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := srv.serveStdioStreams(ctx, &input, &output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Notifications should produce no output
	if output.Len() != 0 {
		t.Fatalf("expected no output for notification, got: %s", output.String())
	}
}

func TestStdioMalformedJSON(t *testing.T) {
	srv := New(DefaultConfig())

	var input bytes.Buffer
	input.WriteString("not valid json\n")

	var output bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := srv.serveStdioStreams(ctx, &input, &output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should get a parse error response
	var resp mcp.Response
	if err := json.Unmarshal([]byte(strings.TrimSpace(output.String())), &resp); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != -32700 {
		t.Fatalf("expected parse error (-32700), got: %+v", resp.Error)
	}
}

func TestStdioEOF(t *testing.T) {
	srv := New(DefaultConfig())

	// Empty input = immediate EOF
	var input bytes.Buffer
	var output bytes.Buffer

	err := srv.serveStdioStreams(context.Background(), &input, &output)
	if err != nil {
		t.Fatalf("expected nil error on EOF, got: %v", err)
	}
}

func TestStdioContextCancel(t *testing.T) {
	srv := New(DefaultConfig())

	// Use a reader that blocks forever
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		// blockingReader never returns
		done <- srv.serveStdioStreams(ctx, &blockingReader{}, &bytes.Buffer{})
	}()

	cancel()

	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("expected context.Canceled, got: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for shutdown")
	}
}

// blockingReader blocks on Read until context is presumably cancelled.
// The server checks ctx.Done() before calling Scan(), so this works
// because after cancel the select hits ctx.Done().
type blockingReader struct{}

func (r *blockingReader) Read(p []byte) (int, error) {
	// Block indefinitely; the goroutine will be abandoned when test ends.
	select {}
}
