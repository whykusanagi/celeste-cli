package server

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
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
