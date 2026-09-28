package loop

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/permissions"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

func execLoop(t *testing.T, ts ...tools.Tool) *Loop {
	t.Helper()
	return &Loop{Tools: newRegistry(ts...), Limits: DefaultLimits(), SpillDir: t.TempDir(), SessionID: "s"}
}

func run(l *Loop, calls ...llm.ToolCallResult) callsOutcome {
	return l.runCalls(context.Background(), calls, l.Limits.withDefaults())
}

func TestRunCallsKeepsCallOrder(t *testing.T) {
	slow := &fakeTool{name: "slow", safe: true, readOnly: true, run: func(context.Context, map[string]any) (tools.ToolResult, error) {
		time.Sleep(50 * time.Millisecond)
		return tools.ToolResult{Content: "slow done"}, nil
	}}
	fast := &fakeTool{name: "fast", safe: true, readOnly: true}
	out := run(execLoop(t, slow, fast),
		llm.ToolCallResult{ID: "c1", Name: "slow", Arguments: `{}`},
		llm.ToolCallResult{ID: "c2", Name: "fast", Arguments: `{}`})
	if len(out.messages) != 2 || out.messages[0].ToolCallID != "c1" || out.messages[1].ToolCallID != "c2" {
		t.Fatalf("messages = %+v, want c1 then c2", out.messages)
	}
	if out.messages[0].Role != "tool" || out.messages[0].Content != "slow done" || out.messages[1].Content != "ok:fast" {
		t.Fatalf("messages = %+v", out.messages)
	}
}

// Concurrency-safe calls run together: each waits until the other started.
func TestRunCallsRunsSafeCallsInParallel(t *testing.T) {
	var started atomic.Int32
	wait := func(context.Context, map[string]any) (tools.ToolResult, error) {
		started.Add(1)
		deadline := time.Now().Add(2 * time.Second)
		for started.Load() < 2 {
			if time.Now().After(deadline) {
				return tools.ToolResult{Content: "ran alone", Error: true}, nil
			}
			time.Sleep(time.Millisecond)
		}
		return tools.ToolResult{Content: "ran together"}, nil
	}
	a := &fakeTool{name: "a", safe: true, readOnly: true, run: wait}
	b := &fakeTool{name: "b", safe: true, readOnly: true, run: wait}
	out := run(execLoop(t, a, b),
		llm.ToolCallResult{ID: "1", Name: "a", Arguments: `{}`},
		llm.ToolCallResult{ID: "2", Name: "b", Arguments: `{}`})
	for _, m := range out.messages {
		if m.Content != "ran together" {
			t.Fatalf("got %q, want both safe calls to run in parallel", m.Content)
		}
	}
}

// A call that is not concurrency-safe finishes before any later call starts.
func TestRunCallsSerializesUnsafeCalls(t *testing.T) {
	var written atomic.Bool
	w := &fakeTool{name: "w", run: func(context.Context, map[string]any) (tools.ToolResult, error) {
		time.Sleep(30 * time.Millisecond)
		written.Store(true)
		return tools.ToolResult{Content: "wrote"}, nil
	}}
	r := &fakeTool{name: "r", safe: true, readOnly: true, run: func(context.Context, map[string]any) (tools.ToolResult, error) {
		if !written.Load() {
			return tools.ToolResult{Content: "read before write"}, nil
		}
		return tools.ToolResult{Content: "read after write"}, nil
	}}
	out := run(execLoop(t, w, r),
		llm.ToolCallResult{ID: "1", Name: "w", Arguments: `{}`},
		llm.ToolCallResult{ID: "2", Name: "r", Arguments: `{}`})
	if out.messages[1].Content != "read after write" {
		t.Fatalf("got %q", out.messages[1].Content)
	}
}

func TestRunCallsBadArguments(t *testing.T) {
	var ran atomic.Bool
	x := &fakeTool{name: "x", run: func(context.Context, map[string]any) (tools.ToolResult, error) {
		ran.Store(true)
		return tools.ToolResult{}, nil
	}}
	out := run(execLoop(t, x),
		llm.ToolCallResult{ID: "1", Name: "x", ArgsError: "dropped delta"},
		llm.ToolCallResult{ID: "2", Name: "x", Arguments: `{"k":`})
	if ran.Load() {
		t.Fatal("a call with bad arguments must not run")
	}
	if !out.anyInvalid {
		t.Fatal("anyInvalid = false")
	}
	if got, want := out.messages[0].Content, `{"error": true, "message": "stream-corrupted tool arguments", "detail": "dropped delta", "tool": "x"}`; got != want {
		t.Fatalf("got %s\nwant %s", got, want)
	}
	if got, want := out.messages[1].Content, `{"error": true, "message": "invalid tool arguments JSON", "tool": "x"}`; got != want {
		t.Fatalf("got %s\nwant %s", got, want)
	}
}

func TestRunCallsEmptyArgumentsMeanNoArguments(t *testing.T) {
	out := run(execLoop(t, &fakeTool{name: "x"}), llm.ToolCallResult{ID: "1", Name: "x", Arguments: ""})
	if out.anyInvalid || out.messages[0].Content != "ok:x" {
		t.Fatalf("got %+v", out)
	}
}

func TestRunCallsUnknownToolAndToolError(t *testing.T) {
	bad := &fakeTool{name: "bad", run: func(context.Context, map[string]any) (tools.ToolResult, error) {
		return tools.ToolResult{Content: "boom", Error: true}, nil
	}}
	out := run(execLoop(t, bad),
		llm.ToolCallResult{ID: "1", Name: "nope", Arguments: `{}`},
		llm.ToolCallResult{ID: "2", Name: "bad", Arguments: `{}`})
	if got, want := out.messages[0].Content, errorEnvelope("nope", "tool 'nope' not found"); got != want {
		t.Fatalf("got %s want %s", got, want)
	}
	if got, want := out.messages[1].Content, `{"error":true,"message":"boom","tool":"bad"}`; got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

// Regression for task 349f1f14: a tool that ignores its context must not
// hold the run past its deadline.
func TestLoopAbandonsUncooperativeTool(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	stuck := &fakeTool{name: "stuck", run: func(context.Context, map[string]any) (tools.ToolResult, error) {
		<-release
		return tools.ToolResult{}, nil
	}}
	l := execLoop(t, stuck)
	l.Limits.ToolTimeout = 50 * time.Millisecond
	l.Limits.HookBudget = 10 * time.Millisecond
	start := time.Now()
	out := run(l, llm.ToolCallResult{ID: "1", Name: "stuck", Arguments: `{}`})
	if time.Since(start) > 2*time.Second {
		t.Fatalf("runCalls blocked %v on an uncooperative tool", time.Since(start))
	}
	if !strings.Contains(out.messages[0].Content, "tool execution exceeded timeout") {
		t.Fatalf("got %s", out.messages[0].Content)
	}
}

// slowHooks is a tools.HookRunner whose PreToolUse takes a while.
type slowHooks struct{ d time.Duration }

func (h slowHooks) PreToolUse(context.Context, string, map[string]any) tools.PreToolHookResult {
	time.Sleep(h.d)
	return tools.PreToolHookResult{Decision: "allow"}
}
func (slowHooks) PostToolUse(context.Context, string, map[string]any, tools.ToolResult) string {
	return ""
}

// A PreToolUse hook slower than the tool's timeout must not eat it: the
// registry starts the tool's timeout after the hooks (F0 hookCtx).
func TestLoopSlowHookDoesNotEatToolTimeout(t *testing.T) {
	quick := &fakeTool{name: "q", run: func(ctx context.Context, _ map[string]any) (tools.ToolResult, error) {
		select {
		case <-ctx.Done():
			return tools.ToolResult{}, ctx.Err()
		case <-time.After(20 * time.Millisecond):
			return tools.ToolResult{Content: "finished"}, nil
		}
	}}
	l := execLoop(t, quick)
	l.Tools.SetHookRunner(slowHooks{d: 150 * time.Millisecond})
	l.Limits.ToolTimeout = 50 * time.Millisecond
	out := run(l, llm.ToolCallResult{ID: "1", Name: "q", Arguments: `{}`})
	if out.messages[0].Content != "finished" {
		t.Fatalf("got %s, want the tool to run to completion after the slow hook", out.messages[0].Content)
	}
}

func TestPromptGateSerializesAsks(t *testing.T) {
	var inFlight, maxInFlight atomic.Int32
	g := PromptGate(func(tools.PermissionRequest) tools.PermissionResponse {
		n := inFlight.Add(1)
		if n > maxInFlight.Load() {
			maxInFlight.Store(n)
		}
		time.Sleep(20 * time.Millisecond)
		inFlight.Add(-1)
		return tools.PermissionResponse{Decision: "allow_once"}
	})
	done := make(chan struct{})
	for i := 0; i < 3; i++ {
		go func() { g.Ask(context.Background(), tools.PermissionRequest{}); done <- struct{}{} }()
	}
	for i := 0; i < 3; i++ {
		<-done
	}
	if maxInFlight.Load() != 1 {
		t.Fatalf("%d asks ran at once, want 1", maxInFlight.Load())
	}
}

func TestLoopToolTimeouterOverridesDefault(t *testing.T) {
	waits := &fakeTool{name: "w", timeout: 50 * time.Millisecond, run: func(ctx context.Context, _ map[string]any) (tools.ToolResult, error) {
		<-ctx.Done()
		return tools.ToolResult{}, ctx.Err()
	}}
	l := execLoop(t, waits)
	l.Limits.ToolTimeout = time.Hour
	start := time.Now()
	out := run(l, llm.ToolCallResult{ID: "1", Name: "w", Arguments: `{}`})
	if time.Since(start) > 2*time.Second || !strings.Contains(out.messages[0].Content, "deadline exceeded") {
		t.Fatalf("after %v got %s, want the tool's own 50ms timeout", time.Since(start), out.messages[0].Content)
	}
}

func TestLoopSpillsLargeResult(t *testing.T) {
	big := &fakeTool{name: "big", run: func(context.Context, map[string]any) (tools.ToolResult, error) {
		return tools.ToolResult{Content: strings.Repeat("x", 200*1024)}, nil
	}}
	l := execLoop(t, big)
	out := run(l, llm.ToolCallResult{ID: "c1", Name: "big", Arguments: `{}`})
	got := out.messages[0].Content
	if len(got) >= 200*1024 || !strings.Contains(got, "TRUNCATED") {
		t.Fatalf("result was not capped: %d bytes", len(got))
	}
	files, _ := filepath.Glob(filepath.Join(l.SpillDir, "s", "*.txt"))
	if len(files) != 1 {
		t.Fatalf("spill files = %v, want one under the session dir", files)
	}
	if b, _ := os.ReadFile(files[0]); len(b) != 200*1024 {
		t.Fatalf("spilled %d bytes, want the full result", len(b))
	}
}

func TestLoopSpillFileNameIsSanitized(t *testing.T) {
	big := &fakeTool{name: "big", run: func(context.Context, map[string]any) (tools.ToolResult, error) {
		return tools.ToolResult{Content: strings.Repeat("y", 200*1024)}, nil
	}}
	l := execLoop(t, big)
	run(l,
		llm.ToolCallResult{ID: "../../evil", Name: "big", Arguments: `{}`},
		llm.ToolCallResult{ID: "", Name: "big", Arguments: `{}`})
	var outside []string
	_ = filepath.Walk(l.SpillDir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && filepath.Dir(p) != filepath.Join(l.SpillDir, "s") {
			outside = append(outside, p)
		}
		return nil
	})
	if len(outside) != 0 {
		t.Fatalf("spill files outside the session dir: %v", outside)
	}
	if files, _ := filepath.Glob(filepath.Join(l.SpillDir, "s", "*.txt")); len(files) != 2 {
		t.Fatalf("spill files = %v, want 2", files)
	}
}

func gatedLoop(t *testing.T) *Loop {
	l := execLoop(t, &fakeTool{name: "w"}) // not read-only: the default policy asks
	l.Tools.SetPermissionChecker(permissions.NewChecker(permissions.DefaultConfig()))
	return l
}

func TestLoopGateAnswersAsk(t *testing.T) {
	l := gatedLoop(t)
	var asked int
	l.Gate = GateFunc(func(_ context.Context, req tools.PermissionRequest) tools.PermissionResponse {
		asked++
		return tools.PermissionResponse{Decision: "allow_once"}
	})
	out := run(l, llm.ToolCallResult{ID: "1", Name: "w", Arguments: `{}`})
	if asked != 1 || out.messages[0].Content != "ok:w" {
		t.Fatalf("asked=%d content=%s", asked, out.messages[0].Content)
	}
}

func TestLoopWithoutGateDeniesAsk(t *testing.T) {
	out := run(gatedLoop(t), llm.ToolCallResult{ID: "1", Name: "w", Arguments: `{}`})
	if !strings.Contains(out.messages[0].Content, "no prompt is configured") {
		t.Fatalf("got %s", out.messages[0].Content)
	}
}

func TestPromptGateNilIsNoGate(t *testing.T) {
	if PromptGate(nil) != nil {
		t.Fatal("PromptGate(nil) must be a nil Gate so Ask means deny")
	}
}

func TestRunCallsTextToolCallResultIsUserMessage(t *testing.T) {
	out := run(execLoop(t, &fakeTool{name: "x"}), llm.ToolCallResult{ID: "text-tc-0", Name: "x", Arguments: `{}`})
	if m := out.messages[0]; m.Role != "user" || m.Content != "[Tool Result: x]\nok:x" {
		t.Fatalf("got %+v", m)
	}
}
