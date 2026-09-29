package subagents

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/hooktest"
)

type warnSink struct {
	mu   sync.Mutex
	list []string
}

func (w *warnSink) add(s string) { w.mu.Lock(); w.list = append(w.list, s); w.mu.Unlock() }
func (w *warnSink) all() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return strings.Join(w.list, "\n")
}

func writeJSON(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func jsonString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// substopHooks is a global hooks.json whose SubagentStop hook records its
// payload to record.
func substopHooks(t *testing.T, record string) string {
	return jsonString(map[string]any{"hooks": []any{map[string]any{
		"event": "SubagentStop", "command": hooktest.Command(t, "record", record),
	}}})
}

func readPayload(t *testing.T, record string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("SubagentStop did not fire: %v", err)
	}
	var p map[string]any
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatal(err)
	}
	return p
}

// fakeManager is a manager on a fake provider with a hermetic HOME, as the
// chat wires it (session ID and warning sink set).
func fakeManager(t *testing.T, srv *fakeprovider.Server) (m *Manager, home, ws string, w *warnSink) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	ws = t.TempDir()
	cfg := &config.Config{APIKey: "k", BaseURL: srv.BaseURL(), Model: "fake-model", Timeout: 10}
	m = NewManager(cfg, ws, false)
	w = &warnSink{}
	m.SetEnvOptions("chat-session-1", w.add)
	t.Cleanup(m.Close)
	return m, home, ws, w
}

// Two subagents, one environment: repo hooks are loaded (and reported as
// untrusted) once, and the report reaches the manager's sink.
func TestSubagentsShareOneEnvironment(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "first done"}, fakeprovider.Turn{Text: "second done"})
	m, _, ws, w := fakeManager(t, srv)
	writeJSON(t, filepath.Join(ws, ".celeste", "hooks.json"), `{"hooks":[{"event":"Stop","command":"x"}]}`) // untrusted
	for _, goal := range []string{"one", "two"} {
		run, err := m.Spawn(context.Background(), goal, ws)
		if err != nil || run.Status != "completed" {
			t.Fatalf("spawn %q: status=%v err=%v", goal, run, err)
		}
	}
	if n := strings.Count(w.all(), "skipping"); n != 1 {
		t.Fatalf("hooks loaded %d times, want once for the manager's shared environment:\n%s", n, w.all())
	}
}

// Review Focus 2 end to end: a permissions change applies to the next
// subagent (the shared environment is rebuilt, and each subagent reloads
// permissions.json).
func TestSubagentsPickUpPermissionChanges(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{Text: "first done"},
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "w", Name: "write_file", Args: `{"path":"out.txt","content":"hi"}`}}},
		fakeprovider.Turn{Text: "second done"},
	)
	m, home, ws, _ := fakeManager(t, srv)
	if _, err := m.Spawn(context.Background(), "one", ws); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(home, ".celeste", "permissions.json"), `{"mode":"default","always_deny":[{"tool_pattern":"write_file"}]}`)
	if _, err := m.Spawn(context.Background(), "two", ws); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ws, "out.txt")); err == nil {
		t.Fatal("the second subagent wrote out.txt although permissions.json now denies write_file")
	}
}

// A subagent fires SubagentStop with its run ID, under the chat's session.
func TestSubagentStopFiresForSpawnedSubagent(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "sub finished"})
	m, home, ws, _ := fakeManager(t, srv)
	record := filepath.Join(t.TempDir(), "substop.json")
	writeJSON(t, filepath.Join(home, ".celeste", "hooks.json"), substopHooks(t, record))
	run, err := m.Spawn(context.Background(), "do it", ws)
	if err != nil {
		t.Fatal(err)
	}
	p := readPayload(t, record)
	if p["agent_id"] != run.ID || p["last_message"] != "sub finished" || p["session_id"] != "chat-session-1" {
		t.Fatalf("payload = %v, want agent_id %q, the last message and the chat session", p, run.ID)
	}
}

// C5: a resumed subagent's SubagentStop carries the ID spawn_agent
// returned, not the checkpoint ID it was resumed from.
func TestSubagentStopKeepsTheAgentIDOnResume(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{Status: 400, Body: `{"error":{"message":"fake bad request","type":"invalid_request_error"}}`},
		fakeprovider.Turn{Text: "resumed and finished"},
	)
	m, home, ws, _ := fakeManager(t, srv)
	record := filepath.Join(t.TempDir(), "substop.json")
	writeJSON(t, filepath.Join(home, ".celeste", "hooks.json"), substopHooks(t, record))
	run, err := m.Spawn(context.Background(), "do it", ws)
	if err == nil || run.CheckpointID == "" || run.CheckpointID == run.ID {
		t.Fatalf("want a failed first run with its own checkpoint: run=%+v err=%v", run, err)
	}
	if _, err := m.Resume(context.Background(), run.CheckpointID, nil); err != nil {
		t.Fatal(err)
	}
	if p := readPayload(t, record); p["agent_id"] != run.ID {
		t.Fatalf("agent_id = %v, want the spawned subagent's ID %q", p["agent_id"], run.ID)
	}
}

// Subagents keep Trust mode under a shared environment, and never see
// spawn_agent (no recursion).
func TestSubagentKeepsTrustAndNoSpawnAgent(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "w", Name: "write_file", Args: `{"path":"out.txt","content":"hi"}`}}},
		fakeprovider.Turn{Text: "wrote it"},
	)
	m, _, ws, _ := fakeManager(t, srv)
	if _, err := m.Spawn(context.Background(), "write", ws); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ws, "out.txt")); err != nil {
		t.Fatalf("the subagent could not write (Trust mode lost): %v", err)
	}
	if strings.Contains(jsonString(srv.Requests()[0].Body["tools"]), "spawn_agent") {
		t.Fatal("a subagent was offered spawn_agent")
	}
}
