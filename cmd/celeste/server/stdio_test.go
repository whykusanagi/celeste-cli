package server

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools/mcp"
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
	ctx, cancel := context.WithCancel(context.Background())

	// The cancel comes only once the server is parked in a Read that never
	// returns, the way an idle stdin is: shutdown must not wait for input.
	reading := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- srv.serveStdioStreams(ctx, &blockingReader{reading: reading}, &bytes.Buffer{})
	}()
	<-reading
	cancel()

	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("expected context.Canceled, got: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a cancel while stdin is idle did not stop the server")
	}
}

// blockingReader signals its first Read, then blocks forever like an idle
// stdin; the goroutine is abandoned when the test ends.
type blockingReader struct {
	reading chan struct{}
	once    sync.Once
}

func (r *blockingReader) Read(p []byte) (int, error) {
	r.once.Do(func() { close(r.reading) })
	select {}
}
