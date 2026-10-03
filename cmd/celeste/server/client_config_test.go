package server

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools/mcp"
)

// celeste_status reports the provider; a base URL with credentials or a
// query must not leak through it.
func TestStatusProviderIsRedacted(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	cfg := DefaultConfig()
	cfg.CelesteConfig = &config.Config{APIKey: "k", Model: "m", BaseURL: "https://user:hunter2@api.test.com/v1?api_key=sekrit"}
	cfg.Workspace = t.TempDir()
	srv := New(cfg)
	RegisterHandlers(srv)
	params, _ := json.Marshal(map[string]any{"name": "celeste_status", "arguments": map[string]any{}})
	resp, err := srv.handleCallTool(context.Background(), &mcp.Request{JSONRPC: "2.0", ID: 1, Method: "tools/call", Params: params})
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"hunter2", "sekrit"} {
		if strings.Contains(string(resp.Result), leak) {
			t.Errorf("celeste_status leaks %q: %s", leak, resp.Result)
		}
	}
	if !strings.Contains(string(resp.Result), "api.test.com/v1") {
		t.Errorf("the provider host should still show: %s", resp.Result)
	}
}
