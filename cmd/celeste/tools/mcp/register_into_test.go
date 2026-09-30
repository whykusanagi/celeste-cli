package mcp

import (
	"encoding/json"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// A nested run's registry shares the manager's connected tools instead of
// starting its own servers; they stay hidden until find_tools activates them,
// exactly as DiscoverAndRegister leaves them.
func TestManagerRegisterIntoSharesConnectedTools(t *testing.T) {
	src := tools.NewRegistry()
	m := NewManager("", src)
	tool := NewMCPTool(MCPToolDef{Name: "echo", Description: "e", InputSchema: json.RawMessage(`{"type":"object"}`)}, nil, "probe")
	src.Register(tool)
	m.toolNames["probe"] = []string{tool.Name()}

	dst := tools.NewRegistry()
	if n := m.RegisterInto(dst); n != 1 {
		t.Fatalf("RegisterInto = %d, want 1", n)
	}
	got, ok := dst.Get(tool.Name())
	if !ok || got != tools.Tool(tool) {
		t.Fatalf("dst has %v (ok=%v), want the manager's own tool (same client)", got, ok)
	}
	dst.SetDiscoveryMode(true)
	for _, x := range dst.GetTools(tools.ModeAgent) {
		if x.Name() == tool.Name() {
			t.Fatal("a mirrored MCP tool must be hidden until find_tools activates it")
		}
	}
}
