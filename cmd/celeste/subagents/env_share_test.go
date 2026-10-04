package subagents

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/hooktest"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/loop"
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

// fakeManager is a manager on a fake provider with a hermetic HOME, wired
// as the chat wires it (2.0 F2e): its subagents nest under a chat Env with
// the chat's session ID, and their warnings go to w. before runs ahead of
// the chat's Setup, to write config the chat loads.
func fakeManager(t *testing.T, srv *fakeprovider.Server, before ...func(home, ws string)) (m *Manager, home, ws string, w *warnSink) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	ws = t.TempDir()
	for _, f := range before {
		f(home, ws)
	}
	cfg := &config.Config{APIKey: "k", BaseURL: srv.BaseURL(), Model: "fake-model", Timeout: 10}
	w = &warnSink{}
	chat, err := loop.Setup(loop.ModeChat, cfg, ws, loop.SetupOptions{SessionID: "chat-session-1", Warn: w.add})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(chat.Close)
	m = NewManager(cfg, ws, false)
	m.UseParent(chat, w.add)
	t.Cleanup(m.Close)
	return m, home, ws, w
}

// Two subagents, one environment: the chat's. Repo hooks are loaded (and
// reported as untrusted) once, when the chat starts, never per subagent.
func TestSubagentsShareOneEnvironment(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "first done"}, fakeprovider.Turn{Text: "second done"})
	m, _, ws, w := fakeManager(t, srv, func(_, ws string) {
		writeJSON(t, filepath.Join(ws, ".celeste", "hooks.json"), `{"hooks":[{"event":"Stop","command":"x"}]}`) // untrusted
	})
	for _, goal := range []string{"one", "two"} {
		run, err := m.Spawn(context.Background(), goal, ws)
		if err != nil || run.Status != "completed" {
			t.Fatalf("spawn %q: status=%v err=%v", goal, run, err)
		}
	}
	if n := strings.Count(w.all(), "hooks: skipping"); n != 1 {
		t.Fatalf("hooks loaded %d times, want once, by the chat's environment:\n%s", n, w.all())
	}
}

// countingNester counts the children nested under a parent.
type countingNester struct {
	inner loop.Nester
	mu    sync.Mutex
	n     int
}

func (c *countingNester) Nested(o loop.NestedOptions) (*loop.Env, error) {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
	return c.inner.Nested(o)
}

// Every subagent nests under the parent UseParent supplied, and closing the
// manager leaves that parent open: the chat owns it.
func TestSubagentsNestUnderTheSuppliedParent(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "first done"}, fakeprovider.Turn{Text: "second done"})
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	ws := t.TempDir()
	cfg := &config.Config{APIKey: "k", BaseURL: srv.BaseURL(), Model: "fake-model", Timeout: 10}
	chat, err := loop.Setup(loop.ModeChat, cfg, ws, loop.SetupOptions{Warn: func(string) {}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(chat.Close)
	parent := &countingNester{inner: chat}
	m := NewManager(cfg, ws, false)
	m.UseParent(parent, func(string) {})
	for _, goal := range []string{"one", "two"} {
		if _, err := m.Spawn(context.Background(), goal, ws); err != nil {
			t.Fatal(err)
		}
	}
	if parent.n != 2 {
		t.Fatalf("subagents nested under the supplied parent %d times, want 2", parent.n)
	}
	m.Close()
	child, err := chat.Nested(loop.NestedOptions{})
	if err != nil {
		t.Fatalf("closing the manager closed the chat's Env: %v", err)
	}
	child.Close()
	if _, err := m.Spawn(context.Background(), "three", ws); err == nil {
		t.Fatal("a spawn after Close must fail")
	}
}

// M1: a background subagent blocked in a tool must not keep the chat's
// shared Env open forever. Close cancels it and waits (bounded) for it to
// release its nested Env, so that when the owner closes its own Env right
// after (exactly as main.go's defer order does), the shared refcount
// actually reaches zero and closeAll (MCP.Stop, code graph Close) runs.
// Before the fix, Close never cancelled in-flight runs, so a detached
// background run (context.Background()) held its reference forever and
// closeAll never ran.
func TestManagerClose_CancelsBackgroundRunAndFreesTheEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	ws := t.TempDir()
	cfg := &config.Config{APIKey: "k", BaseURL: "http://127.0.0.1:1", Model: "fake-model", Timeout: 10}
	chat, err := loop.Setup(loop.ModeChat, cfg, ws, loop.SetupOptions{Warn: func(string) {}})
	if err != nil {
		t.Fatal(err)
	}
	if chat.Indexer == nil {
		t.Fatal("chat Env has no code graph to check closeAll against")
	}

	m := NewManager(cfg, ws, false)
	m.UseParent(chat, func(string) {})

	started := make(chan struct{})
	m.execFn = func(ctx context.Context, run *SubagentRun, _, workspace string, _ TurnCallback, _ int, _ bool) (*SubagentRun, error) {
		// Mirrors executeSubagent + runner.Close(): nest under the shared
		// parent (acquiring a reference), block as if stuck in a tool call,
		// then release the reference once cancelled.
		parent, perr := m.parentEnv()
		if perr != nil {
			return run, perr
		}
		nested, nerr := parent.Nested(loop.NestedOptions{Workspace: workspace})
		if nerr != nil {
			return run, nerr
		}
		close(started)
		<-ctx.Done()
		nested.Close()
		run.Status = "failed"
		run.Error = "cancelled"
		run.EndedAt = time.Now()
		return run, ctx.Err()
	}

	if _, err := m.SpawnWithOptions(context.Background(), "stuck task", ws, SpawnOptions{
		BackgroundAfter: time.Millisecond,
	}); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	<-started // the background run is nested and blocked, holding a reference

	done := make(chan struct{})
	go func() {
		m.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Manager.Close did not return: it must cancel the background run instead of waiting on it forever")
	}

	// The owner closes its own Env last, exactly as main.go's defer order.
	chat.Close()

	// Stats() swallows query errors, so use a store method that surfaces
	// them: a closed sqlite handle returns "sql: database is closed".
	if _, err := chat.Indexer.Store().SearchSymbolsByName("x"); err == nil {
		t.Fatal("chat Env's code graph is still open after Close: the background run's reference on the shared Env was never released")
	}
}

// Review Focus 2 end to end: a permissions change applies to the next
// subagent (each subagent reloads permissions.json, also under the chat's
// Env).
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
	record := filepath.Join(t.TempDir(), "substop.json")
	m, _, ws, _ := fakeManager(t, srv, func(home, _ string) {
		writeJSON(t, filepath.Join(home, ".celeste", "hooks.json"), substopHooks(t, record))
	})
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
	record := filepath.Join(t.TempDir(), "substop.json")
	m, _, ws, _ := fakeManager(t, srv, func(home, _ string) {
		writeJSON(t, filepath.Join(home, ".celeste", "hooks.json"), substopHooks(t, record))
	})
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
