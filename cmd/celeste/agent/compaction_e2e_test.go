package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/compact"
	ctxmgr "github.com/whykusanagi/celeste-cli/cmd/celeste/context"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// windowBackend is a fake model with a hard context window. It reads a new
// file every turn until it has made `turns` tool calls, then finishes. A
// request larger than its window fails the way providers do.
type windowBackend struct {
	mu        sync.Mutex
	window    int
	turns     int
	requests  int
	overflows int
	maxSeen   int
}

func (b *windowBackend) SendMessageStreamEvents(_ context.Context, msgs []tui.ChatMessage, _ []tui.SkillDefinition, cb llm.StreamEventCallback) error {
	b.mu.Lock()
	b.requests++
	size := compact.Estimate(msgs)
	if size > b.maxSeen {
		b.maxSeen = size
	}
	if size > b.window {
		b.overflows++
		b.mu.Unlock()
		return fmt.Errorf("prompt is too long: %d tokens > %d maximum", size, b.window)
	}
	calls := 0
	for _, m := range msgs {
		calls += len(m.ToolCalls)
	}
	b.mu.Unlock()

	usage := &llm.TokenUsage{PromptTokens: size, CompletionTokens: 20}
	if calls >= b.turns {
		cb(llm.StreamEvent{Type: llm.EventContentDelta, ContentDelta: "TASK_COMPLETE: read every file"})
		cb(llm.StreamEvent{Type: llm.EventMessageDone, Usage: usage, FinishReason: "stop"})
		return nil
	}
	id := fmt.Sprintf("toolu_%03d", calls)
	args, _ := json.Marshal(map[string]string{"path": fmt.Sprintf("f%03d.go", calls)})
	cb(llm.StreamEvent{Type: llm.EventToolUseStart, ToolUseID: id, ToolName: "read_file"})
	cb(llm.StreamEvent{Type: llm.EventToolUseDone, ToolUseID: id, ToolName: "read_file", CompleteInput: string(args)})
	cb(llm.StreamEvent{Type: llm.EventMessageDone, Usage: usage, FinishReason: "tool_calls"})
	return nil
}

func (b *windowBackend) SendMessageStream(context.Context, []tui.ChatMessage, []tui.SkillDefinition, llm.StreamCallback) error {
	return fmt.Errorf("not used")
}
func (b *windowBackend) SendMessageSync(context.Context, []tui.ChatMessage, []tui.SkillDefinition) (*llm.ChatCompletionResult, error) {
	return nil, fmt.Errorf("not used")
}
func (b *windowBackend) SetSystemPrompt(string)               {}
func (b *windowBackend) SetThinkingConfig(llm.ThinkingConfig) {}
func (b *windowBackend) Close() error                         { return nil }

// bigFileTool returns ~10k tokens for any path.
type bigFileTool struct{}

func (bigFileTool) Name() string        { return "read_file" }
func (bigFileTool) Description() string { return "read a file" }
func (bigFileTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)
}
func (bigFileTool) IsConcurrencySafe(map[string]any) bool      { return true }
func (bigFileTool) IsReadOnly() bool                           { return true }
func (bigFileTool) ValidateInput(map[string]any) error         { return nil }
func (bigFileTool) InterruptBehavior() tools.InterruptBehavior { return tools.InterruptCancel }
func (bigFileTool) Execute(_ context.Context, input map[string]any, _ chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	path, _ := input["path"].(string)
	return tools.ToolResult{Content: path + "\n" + strings.Repeat("x", 40_000)}, nil
}

func newCompactionRunner(t *testing.T, backend *windowBackend, budgetWindow int) (*Runner, *compact.Store) {
	t.Helper()
	registry := tools.NewRegistry()
	registry.Register(bigFileTool{})
	client := llm.NewClientWithBackend(&llm.Config{}, registry, backend)
	client.SetToolMode(tools.ModeAgent)

	checkpoints, err := NewCheckpointStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	opts := DefaultOptions()
	opts.Workspace = t.TempDir()
	opts.MaxTurns = 60
	opts.EnablePlanning = false
	opts.PlanningExplicit = true
	opts.EmitArtifacts = false
	opts.DisableCheckpoints = true
	opts.Verbose = false
	store := &compact.Store{Dir: t.TempDir()}
	return &Runner{
		client:   client,
		registry: registry,
		store:    checkpoints,
		options:  opts,
		out:      io.Discard,
		errOut:   io.Discard,
		budget:   ctxmgr.NewTokenBudget(budgetWindow, 500, 0),
		pruned:   store,
	}, store
}

// Acceptance for #174: a run that reads ~250k tokens of files through a 64k
// window completes, every request stays inside the window, and the pruned
// results can be recalled.
func TestAgentRunCompletesPastTheWindow(t *testing.T) {
	backend := &windowBackend{window: 64_000, turns: 25}
	runner, store := newCompactionRunner(t, backend, 64_000)

	state, err := runner.RunGoal(context.Background(), "read every file")
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if state.Status != StatusCompleted {
		t.Fatalf("status %q, want completed (error: %s)", state.Status, state.Error)
	}
	if backend.overflows != 0 {
		t.Errorf("%d requests overflowed; proactive compaction should have kept them inside", backend.overflows)
	}
	if backend.maxSeen > backend.window {
		t.Errorf("largest request %d exceeded the %d window", backend.maxSeen, backend.window)
	}
	if runner.budget.CompactCount == 0 {
		t.Error("the run never compacted")
	}
	if _, err := store.Load("toolu_000"); err != nil {
		t.Errorf("the first file's result should be recallable: %v", err)
	}
}

// When the real window is smaller than the configured one, the overflow error
// triggers a forced prune and the turn is retried instead of the run dying.
func TestAgentRunRecoversFromOverflow(t *testing.T) {
	backend := &windowBackend{window: 40_000, turns: 12}
	runner, _ := newCompactionRunner(t, backend, 64_000) // configured window too big

	state, err := runner.RunGoal(context.Background(), "read every file")
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if state.Status != StatusCompleted {
		t.Fatalf("status %q, want completed (error: %s)", state.Status, state.Error)
	}
	if backend.overflows == 0 {
		t.Fatal("test setup: expected at least one overflow to recover from")
	}
}
