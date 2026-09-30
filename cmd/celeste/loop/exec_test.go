package loop

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
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

// A safe group, an unsafe call, then another safe group: each barrier must
// fully finish before the next group starts, in both directions.
func TestRunCallsSafeUnsafeSafeBarrier(t *testing.T) {
	var mu sync.Mutex
	var safe1Done, safe2Done, unsafeStarted, unsafeDone, safe3Started, safe4Started bool
	var group1Err, group2Err atomic.Bool

	finish := func(flag *bool) { mu.Lock(); *flag = true; mu.Unlock() }
	isSet := func(flag *bool) bool { mu.Lock(); defer mu.Unlock(); return *flag }

	safeFirst := func(done *bool) func(context.Context, map[string]any) (tools.ToolResult, error) {
		return func(context.Context, map[string]any) (tools.ToolResult, error) {
			time.Sleep(20 * time.Millisecond)
			finish(done)
			return tools.ToolResult{Content: "ok"}, nil
		}
	}
	safe1 := &fakeTool{name: "safe1", safe: true, readOnly: true, run: safeFirst(&safe1Done)}
	safe2 := &fakeTool{name: "safe2", safe: true, readOnly: true, run: safeFirst(&safe2Done)}
	unsafe := &fakeTool{name: "unsafe", run: func(context.Context, map[string]any) (tools.ToolResult, error) {
		finish(&unsafeStarted)
		if !isSet(&safe1Done) || !isSet(&safe2Done) {
			group1Err.Store(true)
		}
		time.Sleep(20 * time.Millisecond)
		finish(&unsafeDone)
		return tools.ToolResult{Content: "ok"}, nil
	}}
	safeSecond := func(started *bool) func(context.Context, map[string]any) (tools.ToolResult, error) {
		return func(context.Context, map[string]any) (tools.ToolResult, error) {
			finish(started)
			if !isSet(&unsafeDone) {
				group2Err.Store(true)
			}
			return tools.ToolResult{Content: "ok"}, nil
		}
	}
	safe3 := &fakeTool{name: "safe3", safe: true, readOnly: true, run: safeSecond(&safe3Started)}
	safe4 := &fakeTool{name: "safe4", safe: true, readOnly: true, run: safeSecond(&safe4Started)}

	out := run(execLoop(t, safe1, safe2, unsafe, safe3, safe4),
		llm.ToolCallResult{ID: "1", Name: "safe1", Arguments: `{}`},
		llm.ToolCallResult{ID: "2", Name: "safe2", Arguments: `{}`},
		llm.ToolCallResult{ID: "3", Name: "unsafe", Arguments: `{}`},
		llm.ToolCallResult{ID: "4", Name: "safe3", Arguments: `{}`},
		llm.ToolCallResult{ID: "5", Name: "safe4", Arguments: `{}`})

	for _, m := range out.messages {
		if m.Content != "ok" {
			t.Fatalf("messages = %+v, want every call to succeed", out.messages)
		}
	}
	if group1Err.Load() {
		t.Fatal("the unsafe call started before both safe1 and safe2 finished")
	}
	if group2Err.Load() {
		t.Fatal("safe3/safe4 started before the unsafe call finished")
	}
	if !isSet(&unsafeStarted) || !isSet(&safe3Started) || !isSet(&safe4Started) {
		t.Fatal("not every tool ran")
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

	// Run on its own goroutine with a short local deadline: if the watchdog
	// regresses and abandonAfter never returns, this fails in ~2s instead of
	// hanging the whole test binary to go test's default 10m timeout.
	start := time.Now()
	done := make(chan callsOutcome, 1)
	go func() { done <- run(l, llm.ToolCallResult{ID: "1", Name: "stuck", Arguments: `{}`}) }()

	var out callsOutcome
	select {
	case out = <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("runCalls blocked past %v on an uncooperative tool: the watchdog did not abandon it", time.Since(start))
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
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(files[0]); err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("spill file mode = %v (err %v), want 0600", info.Mode().Perm(), err)
		}
		if info, err := os.Stat(filepath.Join(l.SpillDir, "s")); err != nil || info.Mode().Perm() != 0700 {
			t.Fatalf("spill dir mode = %v (err %v), want 0700", info.Mode().Perm(), err)
		}
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
	hermetic(t)                            // permissions.NewChecker reads $HOME
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

// A raw GateFunc (not wrapped by PromptGate) must still be serialized by the
// Loop: two concurrency-safe calls that both Ask must not reach the Gate at
// the same time.
func TestLoopSerializesGateForParallelSafeCalls(t *testing.T) {
	hermetic(t) // permissions.NewChecker reads $HOME
	w1 := &fakeTool{name: "w1", safe: true}
	w2 := &fakeTool{name: "w2", safe: true}
	l := execLoop(t, w1, w2)
	l.Tools.SetPermissionChecker(permissions.NewChecker(permissions.DefaultConfig()))

	var inFlight, maxInFlight atomic.Int32
	l.Gate = GateFunc(func(_ context.Context, req tools.PermissionRequest) tools.PermissionResponse {
		n := inFlight.Add(1)
		for {
			cur := maxInFlight.Load()
			if n <= cur || maxInFlight.CompareAndSwap(cur, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		inFlight.Add(-1)
		return tools.PermissionResponse{Decision: "allow_once"}
	})

	out := run(l,
		llm.ToolCallResult{ID: "1", Name: "w1", Arguments: `{}`},
		llm.ToolCallResult{ID: "2", Name: "w2", Arguments: `{}`})
	for _, m := range out.messages {
		if strings.Contains(m.Content, "denied") {
			t.Fatalf("messages = %+v, want both allowed", out.messages)
		}
	}
	if maxInFlight.Load() != 1 {
		t.Fatalf("%d Gate.Ask calls ran at once through a raw GateFunc, want 1 (Loop must serialize)", maxInFlight.Load())
	}
}

func TestLoopWithoutGateDeniesAsk(t *testing.T) {
	out := run(gatedLoop(t), llm.ToolCallResult{ID: "1", Name: "w", Arguments: `{}`})
	// The headless denial text, as before the loop (agent/orchestrator F1
	// characterization pins it).
	if !strings.Contains(out.messages[0].Content, "no prompt is configured") {
		t.Fatalf("got %s", out.messages[0].Content)
	}
}

// A nil Gate must deny an Ask outright, never fall back to the registry's
// own promptFn (an adopter may set one for its own use, e.g. a TUI modal
// wired independently of the loop's per-run Gate).
func TestLoopNilGateDeniesRatherThanRegistryPromptFn(t *testing.T) {
	l := gatedLoop(t)
	var promptCalls int
	l.Tools.SetPromptFunc(func(tools.PermissionRequest) tools.PermissionResponse {
		promptCalls++
		return tools.PermissionResponse{Decision: "allow_once"}
	})
	// l.Gate is intentionally left nil.
	out := run(l, llm.ToolCallResult{ID: "1", Name: "w", Arguments: `{}`})
	if !strings.Contains(out.messages[0].Content, "Permission denied") {
		t.Fatalf("got %s, want denied without a Gate", out.messages[0].Content)
	}
	if promptCalls != 0 {
		t.Fatalf("registry promptFn was called %d times; a nil Gate must deny without it", promptCalls)
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

// F2a's known limitation, fixed in F2d: a gated run abandons a tool that
// ignores its context, and the clock starts after approval, so the ask
// (longer than the whole budget here) never counts against the tool.
func TestLoopGatedRunAbandonsUncooperativeToolAfterApproval(t *testing.T) {
	hermetic(t) // permissions.NewChecker reads $HOME
	release := make(chan struct{})
	defer close(release)
	var started atomic.Bool
	stuck := &fakeTool{name: "w", run: func(context.Context, map[string]any) (tools.ToolResult, error) {
		started.Store(true)
		<-release
		return tools.ToolResult{}, nil
	}}
	l := execLoop(t, stuck)
	l.Tools.SetPermissionChecker(permissions.NewChecker(permissions.DefaultConfig()))
	l.Limits.ToolTimeout = 200 * time.Millisecond
	l.Limits.HookBudget = 50 * time.Millisecond
	l.Gate = GateFunc(func(context.Context, tools.PermissionRequest) tools.PermissionResponse {
		time.Sleep(600 * time.Millisecond) // 3× the tool timeout
		return tools.PermissionResponse{Decision: "allow_once"}
	})

	done := make(chan callsOutcome, 1)
	go func() { done <- run(l, llm.ToolCallResult{ID: "1", Name: "w", Arguments: `{}`}) }()
	select {
	case out := <-done:
		if !started.Load() {
			t.Fatal("the tool never ran: the approval wait counted against the watchdog")
		}
		if !strings.Contains(out.messages[0].Content, "tool execution exceeded timeout") {
			t.Fatalf("got %s", out.messages[0].Content)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a gated run never abandoned a tool that ignores its context")
	}
}

// The ask itself is not the tool's time: a cooperative tool approved after
// a long wait still runs to completion.
func TestLoopGateWaitDoesNotCountAgainstTheToolBudget(t *testing.T) {
	l := gatedLoop(t)
	l.Limits.ToolTimeout = 200 * time.Millisecond
	l.Limits.HookBudget = 50 * time.Millisecond
	l.Gate = GateFunc(func(context.Context, tools.PermissionRequest) tools.PermissionResponse {
		time.Sleep(600 * time.Millisecond) // 3× the tool timeout
		return tools.PermissionResponse{Decision: "allow_once"}
	})
	out := run(l, llm.ToolCallResult{ID: "1", Name: "w", Arguments: `{}`})
	if out.messages[0].Content != "ok:w" {
		t.Fatalf("got %s", out.messages[0].Content)
	}
}

// The Gate is asked with the call's own context, not the run's: once the
// call returns (or is abandoned), a late ask from its goroutine sees a
// cancelled context and PromptGate denies it without opening the modal. A
// parallel sibling keeps the batch (and its executor context) alive after
// the gated call returns, so only the call's own context can have ended.
func TestLoopGateIsAskedWithTheCallsContext(t *testing.T) {
	hermetic(t) // permissions.NewChecker reads $HOME
	asked := make(chan context.Context, 1)
	var endedWithTheCall atomic.Bool
	gated := &fakeTool{name: "w", safe: true} // not read-only: the default policy asks
	sibling := &fakeTool{name: "r", safe: true, readOnly: true, run: func(context.Context, map[string]any) (tools.ToolResult, error) {
		var askCtx context.Context
		select {
		case askCtx = <-asked:
		case <-time.After(5 * time.Second):
			return tools.ToolResult{Content: "never asked"}, nil
		}
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if askCtx.Err() != nil {
				endedWithTheCall.Store(true)
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		return tools.ToolResult{Content: "ok:r"}, nil
	}}
	l := execLoop(t, gated, sibling)
	l.Tools.SetPermissionChecker(permissions.NewChecker(permissions.DefaultConfig()))
	l.Gate = GateFunc(func(ctx context.Context, _ tools.PermissionRequest) tools.PermissionResponse {
		asked <- ctx
		return tools.PermissionResponse{Decision: "allow_once"}
	})
	out := run(l,
		llm.ToolCallResult{ID: "1", Name: "w", Arguments: `{}`},
		llm.ToolCallResult{ID: "2", Name: "r", Arguments: `{}`})
	if out.messages[0].Content != "ok:w" || out.messages[1].Content != "ok:r" {
		t.Fatalf("got %q, %q", out.messages[0].Content, out.messages[1].Content)
	}
	if !endedWithTheCall.Load() {
		t.Fatal("the Gate's context outlives the call: an abandoned call could still open the modal")
	}
}

// A cancelled run (Esc, or the chat quitting) must not wait on a modal
// nobody will answer.
func TestPromptGateGivesUpWhenTheRunIsCancelled(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	g := PromptGate(func(tools.PermissionRequest) tools.PermissionResponse {
		<-block
		return tools.PermissionResponse{Decision: "allow_once"}
	})
	ctx, cancel := context.WithCancel(context.Background())
	got := make(chan tools.PermissionResponse, 1)
	go func() { got <- g.Ask(ctx, tools.PermissionRequest{ToolName: "w"}) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case r := <-got:
		if r.Decision != "deny" {
			t.Fatalf("decision = %q, want deny", r.Decision)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("PromptGate kept waiting after its run was cancelled")
	}
}

// An ask cancelled while it waited behind a stale modal never opens one of
// its own when the stale modal closes.
func TestPromptGateSkipsAnAskCancelledWhileItWaited(t *testing.T) {
	release := make(chan struct{})
	var calls atomic.Int32
	g := PromptGate(func(tools.PermissionRequest) tools.PermissionResponse {
		if calls.Add(1) == 1 {
			<-release // the stale modal
		}
		return tools.PermissionResponse{Decision: "allow_once"}
	})
	first := make(chan tools.PermissionResponse, 1)
	go func() { first <- g.Ask(context.Background(), tools.PermissionRequest{ToolName: "a"}) }()
	deadline := time.Now().Add(5 * time.Second)
	for calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	ctx, cancel := context.WithCancel(context.Background())
	second := make(chan tools.PermissionResponse, 1)
	go func() { second <- g.Ask(ctx, tools.PermissionRequest{ToolName: "b"}) }()
	time.Sleep(50 * time.Millisecond) // the second ask now waits for the lock
	cancel()
	select {
	case r := <-second:
		if r.Decision != "deny" {
			t.Fatalf("second decision = %q, want deny", r.Decision)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the cancelled ask kept waiting")
	}
	close(release)
	<-first
	time.Sleep(200 * time.Millisecond) // the second ask's goroutine takes the lock
	if n := calls.Load(); n != 1 {
		t.Fatalf("prompt opened %d times, want 1: a cancelled ask opened the modal", n)
	}
}

// The recorded tool_calls message is in the consumer's hands before any
// call runs (events are unbuffered: receiving event N+1 means N was handled).
func TestLoopCallsRecordedBeforeToolsRun(t *testing.T) {
	var seen atomic.Bool
	var sawBeforeRun bool
	tool := &fakeTool{name: "t", run: func(context.Context, map[string]any) (tools.ToolResult, error) {
		sawBeforeRun = seen.Load()
		return tools.ToolResult{Content: "ok"}, nil
	}}
	stub := &stubLLM{reply: func(n int, _ context.Context, cb llm.StreamEventCallback) error {
		if n == 0 {
			callTool(cb, "c1", "t", `{}`)
			return nil
		}
		sayText(cb, "done", nil)
		return nil
	}}
	l := &Loop{Client: stub, Tools: newRegistry(tool), Limits: DefaultLimits()}
	ch := l.Events()
	var snap []Message
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range ch {
			if ev.Kind == EventCallsRecorded {
				snap = ev.History
				seen.Store(true)
			}
			if ev.Kind == EventDone {
				return
			}
		}
	}()
	if _, _, err := l.Run(context.Background(), userMsg("go")); err != nil {
		t.Fatal(err)
	}
	<-done
	if !sawBeforeRun {
		t.Fatal("the tool ran before EventCallsRecorded was delivered")
	}
	last := snap[len(snap)-1]
	if last.Role != "assistant" || len(last.ToolCalls) != 1 || last.ToolCalls[0].ID != "c1" {
		t.Fatalf("snapshot ends with %+v, want the assistant tool_calls message", last)
	}
}

// With KeepToolMetadata (the chat) an image result keeps its metadata and
// gets the marker vision models need; without it (agent, MCP) nothing
// changes. EventToolResult carries the metadata either way.
func TestLoopKeepToolMetadata(t *testing.T) {
	meta := map[string]any{"type": "image", "format": "png", "filename": "a.png"}
	img := &fakeTool{name: "img", run: func(context.Context, map[string]any) (tools.ToolResult, error) {
		return tools.ToolResult{Content: "read a.png", Metadata: meta}, nil
	}}
	for _, keep := range []bool{true, false} {
		l := execLoop(t, img)
		l.Limits.KeepToolMetadata = keep
		wait := collect(l) // collect stops at EventDone, which runCalls never emits
		var out callsOutcome
		go func() {
			out = l.runCalls(context.Background(), []llm.ToolCallResult{{ID: "1", Name: "img", Arguments: `{}`}}, l.Limits.withDefaults())
			l.emit(Event{Kind: EventDone})
		}()
		evs := wait() // returns after EventDone, so out is set
		msg := out.messages[0]
		hasMarker := strings.Contains(msg.Content, "[Image data available: format=png")
		if keep != (msg.Metadata != nil) || keep != hasMarker {
			t.Fatalf("keep=%v: metadata=%v content=%q", keep, msg.Metadata, msg.Content)
		}
		var evMeta map[string]any
		for _, e := range evs {
			if e.Kind == EventToolResult {
				evMeta = e.Metadata
			}
		}
		if evMeta["format"] != "png" {
			t.Fatalf("keep=%v: EventToolResult.Metadata = %v", keep, evMeta)
		}
	}
}

// Loops that share a SpillCounter (the chat's turns) never reuse a spill
// name, even for the same call ID.
func TestLoopSpillCounterSpansLoops(t *testing.T) {
	big := &fakeTool{name: "big", run: func(context.Context, map[string]any) (tools.ToolResult, error) {
		return tools.ToolResult{Content: strings.Repeat("z", 200*1024)}, nil
	}}
	var n atomic.Int64
	dir := t.TempDir()
	for i := 0; i < 2; i++ {
		l := execLoop(t, big)
		l.SpillDir, l.SpillCounter = dir, &n
		run(l, llm.ToolCallResult{ID: "c1", Name: "big", Arguments: `{}`})
	}
	for _, name := range []string{"c1-1.txt", "c1-2.txt"} {
		if _, err := os.Stat(filepath.Join(dir, "s", name)); err != nil {
			t.Fatalf("%s: %v (the second turn overwrote the first spill)", name, err)
		}
	}
}

// Only image metadata rides on the tool message, as the chat did before the
// loop: other metadata (a subagent's name) reaches renderers through
// EventToolResult, never the provider. (Added in implementation.)
func TestLoopKeepToolMetadataOnlyForImages(t *testing.T) {
	other := &fakeTool{name: "other", run: func(context.Context, map[string]any) (tools.ToolResult, error) {
		return tools.ToolResult{Content: "spawned", Metadata: map[string]any{"agent": "x", "is_error": true}}, nil
	}}
	l := execLoop(t, other)
	l.Limits.KeepToolMetadata = true
	out := run(l, llm.ToolCallResult{ID: "1", Name: "other", Arguments: `{}`})
	if msg := out.messages[0]; msg.Metadata != nil || msg.Content != "spawned" {
		t.Fatalf("tool message = %+v, want plain text", msg)
	}
}
