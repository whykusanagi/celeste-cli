package acp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	if got, _ := s.store.Metadata["workspace"].(string); got != filepath.Clean(ws) {
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
