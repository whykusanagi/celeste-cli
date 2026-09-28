package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/hooktest"
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
