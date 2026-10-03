package agent

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/hooktest"
)

// writeHooks writes the trusted global ~/.celeste/hooks.json (F0 Task 1 shape).
func writeHooks(t *testing.T, home string, defs ...map[string]any) {
	t.Helper()
	b, err := json.Marshal(map[string]any{"hooks": defs})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".celeste"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".celeste", "hooks.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func toJSONString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// Every mode gets hooks (2.0 F2): a trusted global PreToolUse hook blocks a
// write in an agent run. Before F2 only the TUI loaded hooks.
func TestAgentRunsGlobalHooks(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "w", Name: "write_file", Args: `{"path":"out.txt","content":"hi"}`}}},
		fakeprovider.Turn{Text: "TASK_COMPLETE: tried"},
	)
	r, ws := fakeRunner(t, srv, func(*Options) {
		home, _ := os.UserHomeDir() // fakeRunner already pointed HOME at a temp dir
		writeHooks(t, home, map[string]any{"event": "PreToolUse", "matcher": "write_file", "command": hooktest.Command(t, "deny", "frozen")})
	})
	if _, err := r.RunGoal(context.Background(), "write"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ws, "out.txt")); err == nil {
		t.Fatal("the hook should have blocked the write")
	}
	if !strings.Contains(toJSONString(srv.Requests()[1].Body["messages"]), "Blocked by pre-tool hook: frozen") {
		t.Fatal("the model should see the hook's denial")
	}
}

// SessionStart fires for a top-level run only; a nested runner (subagent,
// orchestrator lane) skips it. Its context reaches the system prompt.
func TestAgentSessionStartTopLevelOnly(t *testing.T) {
	for _, nested := range []bool{false, true} {
		srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "TASK_COMPLETE: ok"})
		r, _ := fakeRunner(t, srv, func(o *Options) {
			o.Nested = nested
			home, _ := os.UserHomeDir()
			writeHooks(t, home, map[string]any{"event": "SessionStart", "command": hooktest.Command(t, "context", "session-probe")})
		})
		if _, err := r.RunGoal(context.Background(), "go"); err != nil {
			t.Fatal(err)
		}
		got := strings.Contains(toJSONString(srv.Requests()[0].Body["messages"]), "session-probe")
		if got == nested {
			t.Fatalf("nested=%v: SessionStart context present=%v", nested, got)
		}
	}
}

// Hook warnings reach the adopter's sink, not stderr.
func TestAgentHookWarningsGoToOptionsWarn(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "TASK_COMPLETE: ok"})
	var mu sync.Mutex
	var got []string
	r, _ := fakeRunner(t, srv, func(o *Options) {
		o.Warn = func(s string) { mu.Lock(); got = append(got, s); mu.Unlock() }
		ws := o.Workspace
		if err := os.MkdirAll(filepath.Join(ws, ".celeste"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(ws, ".celeste", "hooks.json"),
			[]byte(`{"hooks":[{"event":"Stop","command":"x"}]}`), 0o644); err != nil { // untrusted repo hooks
			t.Fatal(err)
		}
	})
	if _, err := r.RunGoal(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(strings.Join(got, "\n"), "skipping") {
		t.Fatalf("warnings = %v, want the skipped repo hooks reported through Options.Warn", got)
	}
}

// A hook warns from a tool goroutine, and a cancelled run abandons that
// goroutine, so the warning can land while the event consumer reports
// progress or after RunGoal returns. Adopters (realAgentRunner,
// runGoalAccumStats, the TUI) share state between Warn and OnProgress
// without a lock, so the runner must serialize them and stop calling
// either once Close returns. Fails under -race without the gate.
func TestAgentCallbacksSerializedAndSilentAfterClose(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{
			{ID: "a", Name: "read_file", Args: `{"path":"a.txt"}`},
			{ID: "b", Name: "read_file", Args: `{"path":"b.txt"}`},
		}},
		fakeprovider.Turn{Text: "TASK_COMPLETE: ok"},
	)
	var events []string // deliberately unsynchronized, like the adopters
	r, _ := fakeRunner(t, srv, func(o *Options) {
		// The hook sleeps past ToolTimeout, but since Task 10 ToolTimeout
		// bounds the tool, not its hooks (the watchdog adds HookBudget), so
		// the run's deadline below is what abandons the hook goroutine.
		o.ToolTimeout = 50 * time.Millisecond
		o.Warn = func(s string) { events = append(events, "warn: "+s) }
		o.OnProgress = func(_ ProgressKind, text string, _, _ int) { events = append(events, text) }
		home, _ := os.UserHomeDir()
		writeHooks(t, home, map[string]any{"event": "PreToolUse", "matcher": "read_file", "command": hooktest.Command(t, "sleep")})
	})
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_, _ = r.RunGoal(ctx, "read both")
	r.Close()
	n := len(events)
	// The abandoned hooks are killed now; any warning they raise must be
	// dropped, not written into events.
	time.Sleep(1500 * time.Millisecond)
	if len(events) != n {
		t.Fatalf("callbacks after Close: %v", events[n:])
	}
}

// A run refused by FailOnBlockedTools never starts, so SessionStart must not
// fire for it.
func TestAgentRefusedRunSkipsSessionStart(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	marker := filepath.Join(t.TempDir(), "session-start")
	writeHooks(t, home, map[string]any{"event": "SessionStart", "command": hooktest.Command(t, "record", marker)})
	opts := DefaultOptions()
	opts.Workspace = t.TempDir()
	opts.FailOnBlockedTools = true
	cfg := &config.Config{APIKey: "k", BaseURL: "http://127.0.0.1:1", Model: "fake-model", Timeout: 10}
	r, err := NewRunner(cfg, opts, io.Discard, io.Discard)
	if err == nil {
		r.Close()
		t.Fatal("expected FailOnBlockedTools to refuse the run")
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("SessionStart fired for a refused run")
	}
}
