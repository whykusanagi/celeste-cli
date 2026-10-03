package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
)

// Review Focus 2 through MCP chat (2.0 W4 ruling 6): a patch of a file the
// call never read is refused with the read hint and leaves the file alone;
// the model reads it and the next patch lands.
func TestMCPChatPatchAfterReadSucceeds(t *testing.T) {
	llm := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "p1", Name: "patch_file", Args: `{"path":"DOC.md","old_string":"old","new_string":"new"}`}}},
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "r1", Name: "read_file", Args: `{"path":"DOC.md"}`}}},
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "p2", Name: "patch_file", Args: `{"path":"DOC.md","old_string":"old","new_string":"new"}`}}},
		fakeprovider.Turn{Text: "patched"},
	)
	cfg, ws := contractCfg(t, llm)
	writeFile(t, filepath.Join(ws, "DOC.md"), "old\n")
	call(t, cfg, rpc{1, "tools/call", map[string]any{"name": "celeste", "arguments": map[string]any{"prompt": "patch DOC.md", "mode": "chat", "workspace": ws}}})
	if b, _ := os.ReadFile(filepath.Join(ws, "DOC.md")); string(b) != "new\n" {
		t.Fatalf("DOC.md = %q", b)
	}
	reqs := llm.Requests()
	if len(reqs) != 4 {
		t.Fatalf("requests = %d, want 4", len(reqs))
	}
	if got := toolContent(reqs[1], "p1"); !strings.Contains(got, "read_file DOC.md first") {
		t.Fatalf("the first patch should have been refused with the read hint: %q", got)
	}
}
