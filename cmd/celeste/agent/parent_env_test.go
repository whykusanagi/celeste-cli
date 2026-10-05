package agent

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/hooktest"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/loop"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/permissions"
)

func fakeCfg(srv *fakeprovider.Server) *config.Config {
	return &config.Config{APIKey: "k", BaseURL: srv.BaseURL(), Model: "fake-model", Timeout: 10}
}

func mustParentEnv(t *testing.T, cfg *config.Config, ws string, warn func(string)) *loop.Env {
	t.Helper()
	env, err := loop.Setup(loop.ModeAgent, cfg, ws, loop.SetupOptions{Warn: warn})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(env.Close)
	return env
}

func nestedOpts(ws string, parent loop.Nester, warn func(string)) Options {
	opts := DefaultOptions()
	opts.Workspace = ws
	opts.EnablePlanning = false
	opts.RequireVerification = false
	opts.RequestTimeout = 10 * time.Second
	opts.ParentEnv = parent
	opts.Warn = warn
	return opts
}

// Two runners under one parent: hooks load once (by the parent), each runner
// has its own registry but the parent's code graph, a ParentEnv run skips
// SessionStart, and closing the runners leaves the parent usable.
func TestAgentParentEnvIsSharedNotRebuilt(t *testing.T) {
	home := isolateHome(t)
	record := filepath.Join(t.TempDir(), "start.json")
	writeHooks(t, home, map[string]any{"event": "SessionStart", "command": hooktest.Command(t, "record", record)})
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, ".celeste"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".celeste", "hooks.json"), []byte(`{"hooks":[{"event":"Stop","command":"x"}]}`), 0o644); err != nil { // untrusted
		t.Fatal(err)
	}
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "TASK_COMPLETE: one"}, fakeprovider.Turn{Text: "TASK_COMPLETE: two"})
	warns := &sink{}
	parent := mustParentEnv(t, fakeCfg(srv), ws, warns.add)

	for i := 0; i < 2; i++ {
		r, err := NewRunner(fakeCfg(srv), nestedOpts(ws, parent, warns.add), io.Discard, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		if r.env.Registry == parent.Registry || r.env.Indexer != parent.Indexer {
			r.Close()
			t.Fatal("a ParentEnv runner must have its own registry and the parent's code graph")
		}
		if _, err := r.RunGoal(context.Background(), "go"); err != nil {
			r.Close()
			t.Fatal(err)
		}
		r.Close()
	}
	if n := strings.Count(warns.all(), "hooks: skipping"); n != 1 {
		t.Fatalf("hooks loaded %d times, want once:\n%s", n, warns.all())
	}
	if _, err := os.Stat(record); !os.IsNotExist(err) {
		t.Fatalf("SessionStart fired for a runner with a ParentEnv (stat err=%v)", err)
	}
	child, err := parent.Nested(loop.NestedOptions{})
	if err != nil {
		t.Fatalf("closing a runner closed its ParentEnv: %v", err)
	}
	child.Close()
}

// AutoApproveTools trusts the runner's own checker, not the parent's.
func TestAgentParentEnvTrustIsPerRunner(t *testing.T) {
	isolateHome(t)
	ws := t.TempDir()
	srv := fakeprovider.NewOpenAI(t)
	parent := mustParentEnv(t, fakeCfg(srv), ws, func(string) {})
	opts := nestedOpts(ws, parent, func(string) {})
	opts.AutoApproveTools = true
	r, err := NewRunner(fakeCfg(srv), opts, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if r.env.Checker.Mode() != permissions.ModeTrust {
		t.Fatal("AutoApproveTools did not trust the runner's checker")
	}
	if parent.Checker.Mode() == permissions.ModeTrust {
		t.Fatal("AutoApproveTools on a nested runner trusted its parent")
	}
}

// Review Focus 6: the shared hooks runner reports a hook failure to the
// runner whose run fired it (the ctx sink runState sets), not to the
// parent's sink.
func TestAgentParentEnvHookWarningsFollowTheRunner(t *testing.T) {
	home := isolateHome(t)
	writeHooks(t, home, map[string]any{"event": "PostToolUse", "command": hooktest.Command(t, "exit", "3", "boom")})
	ws := t.TempDir()
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "w", Name: "write_file", Args: `{"path":"out.txt","content":"hi"}`}}},
		fakeprovider.Turn{Text: "TASK_COMPLETE: wrote it"},
	)
	parentWarns, runWarns := &sink{}, &sink{}
	parent := mustParentEnv(t, fakeCfg(srv), ws, parentWarns.add)
	opts := nestedOpts(ws, parent, runWarns.add)
	opts.AutoApproveTools = true
	r, err := NewRunner(fakeCfg(srv), opts, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err := r.RunGoal(context.Background(), "write"); err != nil {
		t.Fatal(err)
	}
	if got := runWarns.all(); !strings.Contains(got, "PostToolUse hook") || !strings.Contains(got, "boom") {
		t.Fatalf("runner warnings = %q, want the failed PostToolUse hook", got)
	}
	if strings.Contains(parentWarns.all(), "PostToolUse") {
		t.Fatalf("a nested run's hook failure reached the parent's sink:\n%s", parentWarns.all())
	}
}

// The F2a after-Close rule (TestAgentCallbacksSerializedAndSilentAfterClose)
// holds with a shared hooks runner: a hook the run abandoned warns into
// neither the runner's sink nor the parent's once the runner is closed. It
// also pins the ctx routing itself (runState's hooks.WithWarn(ctx, r.warn)):
// the abandoned hook's failure must reach the runner's own sink, never the
// parent's shared one, whether that happens before or after Close.
func TestAgentParentEnvSilentAfterClose(t *testing.T) {
	home := isolateHome(t)
	writeHooks(t, home, map[string]any{"event": "PreToolUse", "matcher": "read_file", "command": hooktest.Command(t, "sleep")})
	ws := t.TempDir()
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "a", Name: "read_file", Args: `{"path":"a.txt"}`}}},
		fakeprovider.Turn{Text: "TASK_COMPLETE: ok"},
	)
	parentWarns, runWarns := &sink{}, &sink{}
	parent := mustParentEnv(t, fakeCfg(srv), ws, parentWarns.add)
	opts := nestedOpts(ws, parent, runWarns.add)
	opts.AutoApproveTools = true
	opts.ToolTimeout = 50 * time.Millisecond
	r, err := NewRunner(fakeCfg(srv), opts, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_, _ = r.RunGoal(ctx, "read it")
	r.Close()
	run, par := runWarns.all(), parentWarns.all()
	time.Sleep(1500 * time.Millisecond) // the abandoned hook is killed and fails meanwhile
	if runWarns.all() != run || parentWarns.all() != par {
		t.Fatalf("warnings after Close:\nrunner: %q\nparent: %q", strings.TrimPrefix(runWarns.all(), run), strings.TrimPrefix(parentWarns.all(), par))
	}
	// Whether the killed hook's failure lands before Close is timing
	// (taskkill on Windows is slow), so only assert it never reaches the
	// parent. TestAgentParentEnvHookWarningsFollowTheRunner pins the
	// ctx routing itself deterministically.
	if strings.Contains(parentWarns.all(), "PreToolUse hook") {
		t.Fatalf("the abandoned hook's failure reached the parent's shared sink instead of the runner's own (ctx routing broken): par=%q", parentWarns.all())
	}
}
