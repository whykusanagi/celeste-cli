package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools/mcp"
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
	// celeste_status looks for the workspace's code graph under
	// ~/.celeste/projects; keep that out of the real home.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

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
	if !strings.Contains(text, serverVersion) {
		t.Fatal("status should contain server version")
	}
	if !strings.Contains(text, "test-model") {
		t.Fatal("status should contain model")
	}
}

// #144 removed the CLI runtime mode, not the MCP celeste tool's mode
// argument: the schema keeps chat|agent, and a legacy "claw" or "classic"
// value still routes to chat (the handler's default arm), never an error.
func TestCelesteToolModeArgumentSurvivesRuntimeModeRemoval(t *testing.T) {
	var schema struct {
		Properties map[string]struct {
			Enum []string `json:"enum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(celesteToolDef().InputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	if got := schema.Properties["mode"].Enum; len(got) != 2 || got[0] != "chat" || got[1] != "agent" {
		t.Fatalf("mode enum = %v, want [chat agent]", got)
	}
	for _, mode := range []string{"chat", "claw", "classic"} {
		llm := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "ok from " + mode})
		cfg, ws := contractCfg(t, llm)
		res := call(t, cfg, rpc{1, "tools/call", map[string]any{"name": "celeste", "arguments": map[string]any{"prompt": "hi", "mode": mode, "workspace": ws}}})
		if !strings.Contains(string(res[1]), "ok from "+mode) || strings.Contains(string(res[1]), `"isError":true`) {
			t.Fatalf("mode %q did not run chat: %s", mode, res[1])
		}
	}
}
