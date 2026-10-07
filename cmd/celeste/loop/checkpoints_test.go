package loop

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools"
)

// Every call runs with its model call ID on the context (2.0 F4: the
// write tools record it as the checkpoint's message_id). Calls that run
// alone (not concurrency-safe) get theirs too.
func TestLoopPutsTheCallIDOnTheToolsContext(t *testing.T) {
	hermetic(t)
	var mu sync.Mutex
	var got []string
	record := func(ctx context.Context, _ map[string]any) (tools.ToolResult, error) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, tools.CallIDFromContext(ctx))
		return tools.ToolResult{Content: "ok"}, nil
	}
	probe := &fakeTool{name: "probe", safe: true, readOnly: true, run: record}
	writer := &fakeTool{name: "writer", run: record}
	run(execLoop(t, probe, writer),
		llm.ToolCallResult{ID: "call_a", Name: "probe", Arguments: `{}`},
		llm.ToolCallResult{ID: "call_b", Name: "probe", Arguments: `{}`},
		llm.ToolCallResult{ID: "call_c", Name: "writer", Arguments: `{}`})
	sort.Strings(got)
	if strings.Join(got, ",") != "call_a,call_b,call_c" {
		t.Fatalf("call IDs seen by the tool = %v", got)
	}
}

func samePathT(t *testing.T, a, b string) bool {
	t.Helper()
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	return err1 == nil && err2 == nil && ra == rb
}

// Checkpoints are filed under the run's session ID (ruling 9).
func TestSetupFilesCheckpointsUnderTheSessionID(t *testing.T) {
	home := setupHome(t)
	ws := t.TempDir()
	env, err := Setup(ModeAgent, testCfg(), ws, SetupOptions{SessionID: "chat-20261002", Warn: func(string) {}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(env.Close)
	want := filepath.Join(home, ".celeste", "checkpoints", "chat-20261002")
	if env.Snapshots.Dir() != want {
		t.Fatalf("checkpoint dir = %s, want %s", env.Snapshots.Dir(), want)
	}
	tool, ok := env.Registry.Get("write_file")
	if !ok {
		t.Fatal("no write_file")
	}
	res, err := tool.Execute(tools.WithCallID(context.Background(), "call_1"), map[string]any{"path": "a.txt", "content": "x"}, nil)
	if err != nil || res.Error {
		t.Fatalf("write_file: %v %s", err, res.Content)
	}
	entries := env.Snapshots.Entries()
	if len(entries) != 1 || entries[0].MessageID != "call_1" || !samePathT(t, entries[0].Path, filepath.Join(ws, "a.txt")) {
		t.Fatalf("entries = %+v", entries)
	}
}

// Without a session ID a run gets <mode>-<pid>-<start nanos>: the pid
// alone is reused by later processes, which would then share (and /undo)
// an old run's checkpoints.
func TestSetupDefaultSessionIsModePidAndStart(t *testing.T) {
	setupHome(t)
	a, _ := mustSetup(t, ModeAgent, t.TempDir())
	b, _ := mustSetup(t, ModeAgent, t.TempDir())
	base := filepath.Base(a.Snapshots.Dir())
	if !strings.HasPrefix(base, "agent-") || strings.Count(base, "-") != 2 {
		t.Fatalf("default session dir = %s, want agent-<pid>-<nanos>", base)
	}
	if a.Snapshots.Dir() == b.Snapshots.Dir() {
		t.Fatalf("two runs share the default session %s", base)
	}
}

// A nested Env (a subagent, /agent) files into its parent's session, in any
// workspace within the parent boundary, so the chat's /undo, /diff and files
// list include its changes.
func TestNestedSharesTheParentsCheckpoints(t *testing.T) {
	setupHome(t)
	ws := goWorkspace(t)
	parent, _ := mustSetup(t, ModeAgent, ws)
	// Create a subdirectory within the parent workspace
	other := filepath.Join(ws, "subdir")
	if err := os.MkdirAll(other, 0755); err != nil {
		t.Fatal(err)
	}
	child := mustNested(t, parent, NestedOptions{Workspace: other})
	if child.Snapshots != parent.Snapshots {
		t.Fatal("a nested Env must use its parent's checkpoint store")
	}
	tool, _ := child.Registry.Get("write_file")
	res, err := tool.Execute(tools.WithCallID(context.Background(), "call_sub"), map[string]any{"path": "sub.txt", "content": "x"}, nil)
	if err != nil || res.Error {
		t.Fatalf("write_file: %v %s", err, res.Content)
	}
	if e := parent.Snapshots.Entries(); len(e) != 1 || e[0].MessageID != "call_sub" {
		t.Fatalf("parent entries = %+v", e)
	}
}
