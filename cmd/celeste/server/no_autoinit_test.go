package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
)

// Review Focus 3: MCP chat no longer writes .grimoire into the workspace.
func TestMCPChatNoLongerCreatesAGrimoire(t *testing.T) {
	llm := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "ok"})
	cfg, ws := contractCfg(t, llm)
	call(t, cfg, rpc{1, "tools/call", map[string]any{"name": "celeste", "arguments": map[string]any{"prompt": "hi", "mode": "chat", "workspace": ws}}})
	if _, err := os.Stat(filepath.Join(ws, ".grimoire")); !os.IsNotExist(err) {
		t.Fatalf(".grimoire was created: %v", err)
	}
}

// MCP mode:"agent" no longer writes .grimoire either (ruling 5).
func TestMCPAgentNoLongerCreatesAGrimoire(t *testing.T) {
	llm := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "TASK_COMPLETE: ok"})
	cfg, ws := contractCfg(t, llm)
	if _, err := execAgent(context.Background(), cfg.CelesteConfig, "say ok", ws); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ws, ".grimoire")); !os.IsNotExist(err) {
		t.Fatalf(".grimoire was created: %v", err)
	}
}
