package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

func TestReadOnlyHintOnlyForTrustedServers(t *testing.T) {
	hint := true
	def := MCPToolDef{Name: "list", Annotations: &ToolAnnotations{ReadOnlyHint: &hint}}
	untrusted := NewMCPTool(def, nil, "srv")
	trusted := NewMCPTool(def, nil, "srv")
	trusted.trusted = true
	if untrusted.IsReadOnly() {
		t.Fatal("an untrusted server's readOnlyHint must be ignored")
	}
	if !trusted.IsReadOnly() {
		t.Fatal("a trusted server's readOnlyHint is honoured")
	}
	no := false
	for _, a := range []*ToolAnnotations{nil, {}, {ReadOnlyHint: &no}} {
		tool := NewMCPTool(MCPToolDef{Name: "w", Annotations: a}, nil, "srv")
		tool.trusted = true
		if tool.IsReadOnly() {
			t.Fatalf("annotations %+v: a trusted tool without readOnlyHint=true is not read-only", a)
		}
	}
}

// tools/list annotations are parsed and kept, including the hints celeste
// does not use yet.
func TestToolsListParsesAnnotations(t *testing.T) {
	var res toolsListResult
	raw := `{"tools":[{"name":"rm","inputSchema":{"type":"object"},
		"annotations":{"readOnlyHint":false,"destructiveHint":true,"idempotentHint":true,"openWorldHint":false}}]}`
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		t.Fatal(err)
	}
	a := res.Tools[0].Annotations
	if a == nil || a.ReadOnlyHint == nil || *a.ReadOnlyHint || a.DestructiveHint == nil || !*a.DestructiveHint ||
		a.IdempotentHint == nil || !*a.IdempotentHint || a.OpenWorldHint == nil || *a.OpenWorldHint {
		t.Fatalf("annotations = %+v", a)
	}
}

func TestServerConfigTrustedParses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	body := `{"mcpServers":{"mine":{"command":"x","trusted":true},"theirs":{"command":"y"}}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Servers["mine"].Trusted || cfg.Servers["theirs"].Trusted {
		t.Fatalf("servers = %+v", cfg.Servers)
	}
}

// "trusted" auto-approves a server's read-only tools, so only the user's
// home-level configs can set it: a repository's .mcp.json cannot vouch for
// its own server (F0 repo content trust).
func TestTrustedOnlyFromAGlobalConfig(t *testing.T) {
	home := t.TempDir()
	m := NewManager("", tools.NewRegistry())
	m.home = home
	cases := []struct {
		origin string
		set    bool
		want   bool
	}{
		{filepath.Join(home, ".celeste", "mcp.json"), true, true},
		{filepath.Join(home, ".claude", "mcp.json"), true, true},
		{filepath.Join(home, ".celeste", "mcp.json"), false, false},
		{filepath.Join(t.TempDir(), ".mcp.json"), true, false},
		{filepath.Join(t.TempDir(), ".celeste", "mcp.json"), true, false},
		{"", true, false},
	}
	for _, c := range cases {
		if got := m.trusts(ServerConfig{Trusted: c.set, Origin: c.origin}); got != c.want {
			t.Errorf("trusts(trusted=%v, origin=%q) = %v, want %v", c.set, c.origin, got, c.want)
		}
	}
}

func TestConnectClientMarksTrustedTools(t *testing.T) {
	registry := tools.NewRegistry()
	mgr := NewManager("", registry)
	mt := &mockTransport{responses: []*Response{
		makeInitResponse(),
		{JSONRPC: "2.0", ID: json.Number("2"), Result: json.RawMessage(
			`{"tools":[{"name":"ls","inputSchema":{"type":"object"},"annotations":{"readOnlyHint":true}}]}`)},
	}}
	if err := mgr.connectClient(context.Background(), "srv", NewClient(mt, "celeste", "1.0"), "stdio", true); err != nil {
		t.Fatal(err)
	}
	tool, ok := registry.Get(ToolName("srv", "ls"))
	if !ok || !tool.IsReadOnly() {
		t.Fatalf("a trusted server's read-only tool: ok=%v", ok)
	}
}

// Review Focus 5: "a.b" and "a_b" sanitize to the same mcp__a_b__t.
func TestMCPToolCollisionIsAWarning(t *testing.T) {
	reg := tools.NewRegistry()
	first := NewMCPTool(MCPToolDef{Name: "t"}, nil, "a.b")
	second := NewMCPTool(MCPToolDef{Name: "t"}, nil, "a_b")
	if first.Name() != second.Name() {
		t.Fatalf("test setup: %s != %s", first.Name(), second.Name())
	}
	var warns []string
	warn := func(s string) { warns = append(warns, s) }
	if !registerTool(reg, first, warn) {
		t.Fatal("the first tool registers")
	}
	if registerTool(reg, second, warn) {
		t.Fatal("the second must be refused")
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "a_b") || !strings.Contains(warns[0], "a.b") {
		t.Fatalf("warnings = %v, want one naming both servers", warns)
	}
	if got, _ := reg.Get(first.Name()); got != tools.Tool(first) {
		t.Fatal("the first server's tool must stay")
	}
}

// A refused tool is not the server's: disconnecting the server that lost
// the collision must not remove the winner's tool.
func TestCollisionLoserDisconnectKeepsWinner(t *testing.T) {
	registry := tools.NewRegistry()
	mgr := NewManager("", registry)
	connect := func(server string) {
		mt := &mockTransport{responses: []*Response{makeInitResponse(), makeToolsListResponse("t")}}
		if err := mgr.connectClient(context.Background(), server, NewClient(mt, "celeste", "1.0"), "stdio", false); err != nil {
			t.Fatal(err)
		}
	}
	connect("a.b")
	connect("a_b")
	if n := len(mgr.toolNames["a_b"]); n != 0 {
		t.Fatalf("the refused server owns %d tools", n)
	}
	if err := mgr.Disconnect("a_b"); err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.Get(ToolName("a.b", "t")); !ok {
		t.Fatal("disconnecting the loser removed the winner's tool")
	}
}

// A nested registry never has a mirrored MCP tool replace one it holds.
func TestRegisterIntoNeverReplaces(t *testing.T) {
	src := tools.NewRegistry()
	m := NewManager("", src)
	tool := NewMCPTool(MCPToolDef{Name: "echo", InputSchema: json.RawMessage(`{"type":"object"}`)}, nil, "probe")
	src.Register(tool)
	m.toolNames["probe"] = []string{tool.Name()}

	dst := tools.NewRegistry()
	other := NewMCPTool(MCPToolDef{Name: "echo"}, nil, "probe")
	dst.Register(other)
	if n := m.RegisterInto(dst); n != 0 {
		t.Fatalf("RegisterInto = %d, want 0", n)
	}
	if got, _ := dst.Get(tool.Name()); got != tools.Tool(other) {
		t.Fatal("RegisterInto replaced a tool the registry held")
	}
}

// A second connect of a server while the first is still in flight is
// refused and closes its own client: two clients for one name would leave
// tools that Disconnect never removes.
func TestConnectClientRefusesAConnectInFlight(t *testing.T) {
	registry := tools.NewRegistry()
	mgr := NewManager("", registry)
	mgr.connecting["srv"] = true
	mt := &mockTransport{responses: []*Response{makeInitResponse(), makeToolsListResponse("t")}}
	if err := mgr.connectClient(context.Background(), "srv", NewClient(mt, "celeste", "1.0"), "stdio", false); err == nil {
		t.Fatal("a connect while one is in flight must fail")
	}
	if !mt.closed || registry.Count() != 0 || mgr.IsConnected("srv") {
		t.Fatalf("closed=%v tools=%d connected=%v", mt.closed, registry.Count(), mgr.IsConnected("srv"))
	}
}

// The single-file config (NewManager(path)) records each server's origin,
// so "trusted" in the home config is honoured there too.
func TestSingleConfigStampsOrigin(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".celeste", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"mcpServers":{"mine":{"command":"x","trusted":true}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	m := NewManager(path, tools.NewRegistry())
	m.home = home
	cfg, err := m.loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if sc := cfg.Servers["mine"]; sc.Origin != path || !m.trusts(sc) {
		t.Fatalf("server = %+v, trusted=%v", sc, m.trusts(sc))
	}
}

// Start connects enabled servers in name order, so which server keeps a
// colliding tool name is the same on every launch ("a.b" sorts before "a_b").
func TestStartOrderIsSortedAndSkipsDisabled(t *testing.T) {
	servers := map[string]ServerConfig{
		"a_b": {Enabled: true},
		"off": {Enabled: false},
		"a.b": {Enabled: true},
		"z":   {Enabled: true},
	}
	for i := 0; i < 20; i++ {
		got := startOrder(servers)
		want := []string{"a.b", "a_b", "z"}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("startOrder = %v, want %v", got, want)
		}
	}
}
