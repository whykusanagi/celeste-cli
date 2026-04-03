package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools/mcp"
)

func TestCelesteToolDef(t *testing.T) {
	def := celesteToolDef()
	if def.Name != "celeste" {
		t.Fatalf("expected name 'celeste', got %q", def.Name)
	}

	var schema map[string]any
	if err := json.Unmarshal(def.InputSchema, &schema); err != nil {
		t.Fatalf("invalid schema JSON: %v", err)
	}
	props := schema["properties"].(map[string]any)
	if _, ok := props["prompt"]; !ok {
		t.Fatal("schema missing 'prompt' property")
	}
	required := schema["required"].([]any)
	if len(required) != 1 || required[0] != "prompt" {
		t.Fatalf("expected required=[prompt], got %v", required)
	}
}

func TestCelesteContentToolDef(t *testing.T) {
	def := celesteContentToolDef()
	if def.Name != "celeste_content" {
		t.Fatalf("expected name 'celeste_content', got %q", def.Name)
	}

	var schema map[string]any
	if err := json.Unmarshal(def.InputSchema, &schema); err != nil {
		t.Fatalf("invalid schema JSON: %v", err)
	}
	props := schema["properties"].(map[string]any)
	if _, ok := props["prompt"]; !ok {
		t.Fatal("schema missing 'prompt' property")
	}
}

func TestCelesteStatusToolDef(t *testing.T) {
	def := celesteStatusToolDef()
	if def.Name != "celeste_status" {
		t.Fatalf("expected name 'celeste_status', got %q", def.Name)
	}
}

func TestCelesteHandlerRejectsEmptyPrompt(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CelesteConfig = &config.Config{APIKey: "test"}
	srv := New(cfg)
	RegisterHandlers(srv)

	params, _ := json.Marshal(map[string]any{
		"name":      "celeste",
		"arguments": map[string]any{"prompt": ""},
	})
	req := &mcp.Request{JSONRPC: "2.0", ID: 1, Method: "tools/call", Params: params}
	resp, err := srv.handleCallTool(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result struct {
		IsError bool               `json:"isError"`
		Content []mcp.ContentBlock `json:"content"`
	}
	json.Unmarshal(resp.Result, &result)
	if !result.IsError {
		t.Fatal("expected isError=true for empty prompt")
	}
}

func TestCelesteContentHandlerRejectsEmptyPrompt(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CelesteConfig = &config.Config{APIKey: "test"}
	srv := New(cfg)
	RegisterHandlers(srv)

	params, _ := json.Marshal(map[string]any{
		"name":      "celeste_content",
		"arguments": map[string]any{},
	})
	req := &mcp.Request{JSONRPC: "2.0", ID: 2, Method: "tools/call", Params: params}
	resp, err := srv.handleCallTool(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result struct {
		IsError bool `json:"isError"`
	}
	json.Unmarshal(resp.Result, &result)
	if !result.IsError {
		t.Fatal("expected isError=true for empty prompt")
	}
}

func TestCelesteStatusHandler(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CelesteConfig = &config.Config{
		APIKey:  "test-key",
		BaseURL: "https://api.test.com/v1",
		Model:   "test-model",
	}
	cfg.Workspace = t.TempDir()
	srv := New(cfg)
	RegisterHandlers(srv)

	params, _ := json.Marshal(map[string]any{
		"name":      "celeste_status",
		"arguments": map[string]any{},
	})
	req := &mcp.Request{JSONRPC: "2.0", ID: 3, Method: "tools/call", Params: params}
	resp, err := srv.handleCallTool(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result struct {
		Content []mcp.ContentBlock `json:"content"`
	}
	json.Unmarshal(resp.Result, &result)
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(result.Content))
	}

	text := result.Content[0].Text
	if !strings.Contains(text, "celeste") {
		t.Fatal("status should contain server name")
	}
	if !strings.Contains(text, "1.8.0") {
		t.Fatal("status should contain server version")
	}
	if !strings.Contains(text, "test-model") {
		t.Fatal("status should contain model")
	}
}
