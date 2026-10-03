package mcp

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools"
)

// A nested run's registry shares the manager's connected tools instead of
// starting its own servers; they stay hidden until find_tools activates them,
// exactly as discoverAndRegister leaves them.
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

// A run nested under the chat mirrors only the servers configured in a
// global file (2.0 F2e): a repo's servers, and servers of unknown origin,
// stay the chat's.
func TestManagerRegisterGlobalIntoKeepsGlobalServers(t *testing.T) {
	home := t.TempDir()
	src := tools.NewRegistry()
	m := NewManager("", src)
	add := func(server, origin string) string {
		tool := NewMCPTool(MCPToolDef{Name: "echo", Description: "e", InputSchema: json.RawMessage(`{"type":"object"}`)}, nil, server)
		src.Register(tool)
		m.toolNames[server] = []string{tool.Name()}
		if origin != "" {
			m.origins[server] = origin
		}
		return tool.Name()
	}
	global := add("home", filepath.Join(home, ".celeste", "mcp.json"))
	claude := add("claude", filepath.Join(home, ".claude", "mcp.json"))
	repo := add("repo", filepath.Join(t.TempDir(), ".mcp.json"))
	unknown := add("lazy", "")

	dst := tools.NewRegistry()
	if n := m.RegisterGlobalInto(dst, home); n != 2 {
		t.Fatalf("RegisterGlobalInto = %d, want 2", n)
	}
	for _, name := range []string{global, claude} {
		if _, ok := dst.Get(name); !ok {
			t.Errorf("global server tool %s missing", name)
		}
	}
	for _, name := range []string{repo, unknown} {
		if _, ok := dst.Get(name); ok {
			t.Errorf("non-global server tool %s was mirrored", name)
		}
	}
}
