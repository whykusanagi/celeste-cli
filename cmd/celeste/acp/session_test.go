package acp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
)

func TestNewSessionBuildsAnEnvForCwd(t *testing.T) {
	c := newTestClient(t, testConfig(nil, 0))
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, ".grimoire"), []byte("## Bindings\n- acp-grimoire-marker\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sid := c.newSession(ws)
	if sid == "" {
		t.Fatal("session/new returned no sessionId")
	}
	// The ACP sessionId is the celeste session's (ruling 3), saved at once.
	if _, err := os.Stat(filepath.Join(c.home, ".celeste", "sessions", sid+".json")); err != nil {
		t.Fatalf("no session file for %s: %v", sid, err)
	}
	s := c.agent.session(sid)
	if s == nil {
		t.Fatalf("agent has no session %s", sid)
	}
	if !strings.Contains(s.systemPrompt, "acp-grimoire-marker") {
		t.Fatalf("system prompt lacks the workspace grimoire:\n%s", s.systemPrompt)
	}
	if got := s.store.GetWorkspace(); got != filepath.Clean(ws) {
		t.Fatalf("session workspace = %q, want %q", got, ws)
	}
}

func TestNewSessionRejectsABadCwd(t *testing.T) {
	c := newTestClient(t, testConfig(nil, 0))
	for _, cwd := range []string{"", "relative/dir", filepath.Join(t.TempDir(), "missing")} {
		_, err := c.call("session/new", map[string]any{"cwd": cwd, "mcpServers": []any{}})
		if err == nil || err.Code != CodeInvalidParams {
			t.Fatalf("cwd %q: error = %v, want %d", cwd, err, CodeInvalidParams)
		}
	}
}

// Client MCP servers join the session's registry (ruling 4); a server that
// fails to start is logged and the session goes on; http and sse ones are
// ignored.
func TestNewSessionConnectsClientMCPServers(t *testing.T) {
	c := newTestClient(t, testConfig(nil, 0))
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ws := t.TempDir()
	res, rerr := c.call("session/new", map[string]any{"cwd": ws, "mcpServers": []any{
		map[string]any{"name": "stub", "command": exe, "args": []string{}, "env": []any{map[string]any{"name": mcpStubEnv, "value": "1"}}},
		map[string]any{"name": "broken", "command": filepath.Join(ws, "no-such-binary"), "args": []string{}, "env": []any{}},
		map[string]any{"type": "http", "name": "remote", "url": "https://example.invalid/mcp", "headers": []any{}},
	}})
	if rerr != nil {
		t.Fatal(rerr)
	}
	sid := sessionIDOf(t, res)
	s := c.agent.session(sid)
	if _, ok := s.env.Registry.Get("mcp__stub__echo"); !ok {
		var names []string
		for _, tl := range s.env.Registry.GetAll() {
			names = append(names, tl.Name())
		}
		t.Fatalf("the client's MCP tool is not registered; tools: %v", names)
	}
}

// A repository's MCP config never starts a server in an ACP session: the
// editor cannot be asked first, and passes its own servers instead. The
// user's global config still applies.
func TestNewSessionSkipsRepoMCPConfigs(t *testing.T) {
	c := newTestClient(t, testConfig(nil, 0))
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	server := func() map[string]any {
		return map[string]any{"enabled": true, "transport": "stdio", "command": exe, "env": map[string]string{mcpStubEnv: "1"}}
	}
	writeJSON := func(path string, v any) {
		t.Helper()
		b, _ := json.Marshal(v)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ws := t.TempDir()
	writeJSON(filepath.Join(ws, ".mcp.json"), map[string]any{"mcpServers": map[string]any{"repo": server()}})
	writeJSON(filepath.Join(c.home, ".celeste", "mcp.json"), map[string]any{"mcpServers": map[string]any{"mine": server()}})
	s := c.agent.session(c.newSession(ws))
	if _, ok := s.env.Registry.Get("mcp__repo__echo"); ok {
		t.Fatal("a repo MCP config started its server in an ACP session")
	}
	if _, ok := s.env.Registry.Get("mcp__mine__echo"); !ok {
		t.Fatal("the user's global MCP server is missing")
	}
}

// Each prompt's history is saved to the celeste session (ruling 11), so
// `celeste resume` of an editor thread shows it.
func TestPromptSavesHistory(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "acp-reply-marker"})
	c := newTestClient(t, testConfig(srv, 0))
	sid := c.newSession(t.TempDir())
	if _, err := c.call("session/prompt", textPrompt(sid, "acp-prompt-marker")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(c.home, ".celeste", "sessions", sid+".json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"acp-prompt-marker", "acp-reply-marker"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("saved session lacks %q:\n%s", want, data)
		}
	}
}

// The editor's MCP servers start in the session's folder, not in the
// directory the editor started celeste in.
func TestClientMCPServersStartInTheSessionCwd(t *testing.T) {
	c := newTestClient(t, testConfig(nil, 0))
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ws := t.TempDir()
	res, rerr := c.call("session/new", map[string]any{"cwd": ws, "mcpServers": []any{
		map[string]any{"name": "stub", "command": exe, "args": []string{}, "env": []any{map[string]any{"name": mcpStubEnv, "value": "1"}}},
	}})
	if rerr != nil {
		t.Fatal(rerr)
	}
	s := c.agent.session(sessionIDOf(t, res))
	tl, ok := s.env.Registry.Get("mcp__stub__echo")
	if !ok {
		t.Fatal("the client's MCP tool is not registered")
	}
	want, err := os.Stat(ws)
	if err != nil {
		t.Fatal(err)
	}
	_, got, found := strings.Cut(tl.Description(), "cwd=")
	fi, err := os.Stat(strings.TrimSpace(got))
	if !found || err != nil || !os.SameFile(fi, want) {
		t.Fatalf("MCP server cwd = %q, want %q", tl.Description(), ws)
	}
}

// An editor MCP server named like a global one is not started; the log
// says the editor's entry is ignored.
func TestClientMCPServerShadowedByGlobalIsLogged(t *testing.T) {
	c := newTestClient(t, testConfig(nil, 0))
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"github": map[string]any{
		"enabled": true, "transport": "stdio", "command": exe, "env": map[string]string{mcpStubEnv: "1"},
	}}})
	if err := os.MkdirAll(filepath.Join(c.home, ".celeste"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.home, ".celeste", "mcp.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	ws := t.TempDir()
	if _, rerr := c.call("session/new", map[string]any{"cwd": ws, "mcpServers": []any{
		map[string]any{"name": "github", "command": exe, "args": []string{}, "env": []any{map[string]any{"name": mcpStubEnv, "value": "1"}}},
	}}); rerr != nil {
		t.Fatal(rerr)
	}
	if !c.logged(`MCP server "github" is already configured globally; the editor's entry is ignored`) {
		t.Fatal("no log line for the editor's shadowed MCP server")
	}
}
