package server

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/hooktest"
)

// MCP mode:"agent" has no terminal, so Setup warnings (here: a skipped repo
// MCP config) must come back in the tool result.
func TestExecAgentReturnsSetupWarnings(t *testing.T) {
	llm := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "done"})
	cfg, ws := contractCfg(t, llm)
	repoCfg := filepath.Join(ws, ".mcp.json")
	if err := os.WriteFile(repoCfg, []byte(`{"mcpServers":{"probe":{"enabled":true,"command":"true"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := execAgent(context.Background(), cfg.CelesteConfig, "go", ws)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"## Warnings", strconv.Quote(repoCfg), "non-interactive"} {
		if !strings.Contains(out.Text, want) {
			t.Errorf("result lacks %q:\n%s", want, out.Text)
		}
	}
	if i, j := strings.Index(out.Text, "## Warnings"), strings.Index(out.Text, "_Agent:"); i < 0 || j < i {
		t.Errorf("warnings must precede the metadata footer:\n%s", out.Text)
	}
}

func TestFormatWarningsEmpty(t *testing.T) {
	if got := formatWarnings(nil); got != "" {
		t.Errorf("formatWarnings(nil) = %q", got)
	}
}

// MCP mode:"agent"'s prompt is the caller's goal: UserPromptSubmit sees it
// once, and a deny refuses the call before any model call (2.0 F2e).
func TestExecAgentGoalBlockedByUserPromptSubmit(t *testing.T) {
	llm := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "never sent"})
	cfg, ws := contractCfg(t, llm)
	globalHooks(t, hookDef("UserPromptSubmit", "", hooktest.Command(t, "deny", "no secrets")))
	_, err := execAgent(context.Background(), cfg.CelesteConfig, "my password is hunter2", ws)
	if err == nil || err.Error() != "agent error: goal blocked by a UserPromptSubmit hook: no secrets" {
		t.Fatalf("err = %v", err)
	}
	if n := len(llm.Requests()); n != 0 {
		t.Fatalf("requests = %d, want 0", n)
	}
}
