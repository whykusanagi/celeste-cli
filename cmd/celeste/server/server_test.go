package server

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools/mcp"
)

func TestRegisterTool(t *testing.T) {
	srv := New(DefaultConfig())
	def := mcp.MCPToolDef{
		Name:        "test_tool",
		Description: "A test tool",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}
	srv.RegisterTool(def, func(ctx context.Context, args map[string]any) ([]ContentBlock, error) {
		return []ContentBlock{{Type: "text", Text: "ok"}}, nil
	})

	srv.mu.RLock()
	defer srv.mu.RUnlock()
	if len(srv.tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(srv.tools))
	}
	if srv.tools[0].Name != "test_tool" {
		t.Fatalf("expected tool name 'test_tool', got %q", srv.tools[0].Name)
	}
	if _, ok := srv.handlers["test_tool"]; !ok {
		t.Fatal("handler not registered")
	}
}

func TestHandleInitialize(t *testing.T) {
	srv := New(DefaultConfig())
	req := &mcp.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params:  json.RawMessage(`{"protocolVersion":"2024-11-05"}`),
	}
	resp, err := srv.handleInitialize(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error in response: %v", resp.Error)
	}

	var result map[string]any
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result["protocolVersion"] != "2024-11-05" {
		t.Fatalf("expected protocol version 2024-11-05, got %v", result["protocolVersion"])
	}
	serverInfo := result["serverInfo"].(map[string]any)
	if serverInfo["name"] != "celeste" {
		t.Fatalf("expected server name 'celeste', got %v", serverInfo["name"])
	}
	if serverInfo["version"] != "1.8.0" {
		t.Fatalf("expected server version '1.8.0', got %v", serverInfo["version"])
	}
}

func TestHandleListTools(t *testing.T) {
	srv := New(DefaultConfig())
	srv.RegisterTool(mcp.MCPToolDef{
		Name:        "tool_a",
		Description: "Tool A",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}, func(ctx context.Context, args map[string]any) ([]ContentBlock, error) {
		return nil, nil
	})
	srv.RegisterTool(mcp.MCPToolDef{
		Name:        "tool_b",
		Description: "Tool B",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}, func(ctx context.Context, args map[string]any) ([]ContentBlock, error) {
		return nil, nil
	})

	req := &mcp.Request{JSONRPC: "2.0", ID: 2, Method: "tools/list"}
	resp, err := srv.handleListTools(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result struct {
		Tools []mcp.MCPToolDef `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(result.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(result.Tools))
	}
}

func TestHandleCallTool(t *testing.T) {
	srv := New(DefaultConfig())
	srv.RegisterTool(mcp.MCPToolDef{
		Name:        "echo",
		Description: "Echo tool",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"msg":{"type":"string"}}}`),
	}, func(ctx context.Context, args map[string]any) ([]ContentBlock, error) {
		msg, _ := args["msg"].(string)
		return []ContentBlock{{Type: "text", Text: msg}}, nil
	})

	params, _ := json.Marshal(map[string]any{
		"name":      "echo",
		"arguments": map[string]any{"msg": "hello"},
	})
	req := &mcp.Request{JSONRPC: "2.0", ID: 3, Method: "tools/call", Params: params}
	resp, err := srv.handleCallTool(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result struct {
		Content []mcp.ContentBlock `json:"content"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(result.Content) != 1 || result.Content[0].Text != "hello" {
		t.Fatalf("unexpected content: %+v", result.Content)
	}
}

func TestHandleCallToolUnknown(t *testing.T) {
	srv := New(DefaultConfig())
	params, _ := json.Marshal(map[string]any{
		"name":      "nonexistent",
		"arguments": map[string]any{},
	})
	req := &mcp.Request{JSONRPC: "2.0", ID: 4, Method: "tools/call", Params: params}
	resp, err := srv.handleCallTool(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error response for unknown tool")
	}
	if resp.Error.Code != -32601 {
		t.Fatalf("expected code -32601, got %d", resp.Error.Code)
	}
}

func TestDispatchRouting(t *testing.T) {
	srv := New(DefaultConfig())
	ctx := context.Background()

	// Test unknown method
	req := &mcp.Request{JSONRPC: "2.0", ID: 10, Method: "unknown/method"}
	resp, err := srv.dispatch(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != -32601 {
		t.Fatal("expected -32601 for unknown method")
	}

	// Test notification returns nil
	notifReq := &mcp.Request{JSONRPC: "2.0", ID: 0, Method: "notifications/initialized"}
	resp, err = srv.dispatch(ctx, notifReq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != nil {
		t.Fatal("expected nil response for notification")
	}
}

func TestDispatchInitialize(t *testing.T) {
	srv := New(DefaultConfig())
	req := &mcp.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params:  json.RawMessage(`{}`),
	}
	resp, err := srv.dispatch(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil || resp.Error != nil {
		t.Fatal("expected successful initialize response")
	}
}
