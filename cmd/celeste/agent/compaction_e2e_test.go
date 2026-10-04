package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/compact"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	ctxmgr "github.com/whykusanagi/celeste-cli/v2/cmd/celeste/context"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/loop"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools/builtin"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
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
	issued    int // tool calls made so far; a summary can't reset it
	onRequest func([]tui.ChatMessage)
}

func (b *windowBackend) SendMessageStreamEvents(_ context.Context, msgs []tui.ChatMessage, _ []tui.SkillDefinition, cb llm.StreamEventCallback) error {
	b.mu.Lock()
	if b.onRequest != nil {
		b.onRequest(msgs)
	}
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
	calls := b.issued
	if calls < b.turns {
		b.issued++
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

// With pruning unavailable, the summary rung keeps the run inside the window:
// everything but the newest ~20k tokens is replaced by a structured summary
// from the small-model role.
func TestAgentRunSummarizesWhenPruningIsNotEnough(t *testing.T) {
	backend := &windowBackend{window: 64_000, turns: 20}
	runner, _ := newCompactionRunner(t, backend, 64_000)
	runner.pruned = nil // force the summary rung

	var summaries int
	var sawGoal bool
	runner.summarize = func(_ context.Context, system, user string) (string, error) {
		summaries++
		if strings.Contains(user, "read every file") {
			sawGoal = true
		}
		return "## Goal\nread every file\n## Progress\n### Done\nread some files", nil
	}

	state, err := runner.RunGoal(context.Background(), "read every file")
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if state.Status != StatusCompleted {
		t.Fatalf("status %q, want completed (error: %s)", state.Status, state.Error)
	}
	if summaries == 0 {
		t.Fatal("the summary rung never ran")
	}
	if !sawGoal {
		t.Error("the original goal should reach the summarizer")
	}
	if backend.overflows != 0 || backend.maxSeen > backend.window {
		t.Errorf("summaries should keep requests inside the window: overflows=%d max=%d", backend.overflows, backend.maxSeen)
	}
	if !compact.IsSummary(state.Messages[0]) {
		t.Error("the history should now start with the summary")
	}
}

// batchBackend asks for n files in one parallel batch, then checks the next
// request: every result must reach the model un-elided once (#234 thrash).
type batchBackend struct {
	mu       sync.Mutex
	n        int
	requests int
	elided   []string // results already elided when the model first saw them
}

func (b *batchBackend) SendMessageStreamEvents(_ context.Context, msgs []tui.ChatMessage, _ []tui.SkillDefinition, cb llm.StreamEventCallback) error {
	b.mu.Lock()
	b.requests++
	req := b.requests
	b.mu.Unlock()
	usage := &llm.TokenUsage{PromptTokens: compact.Estimate(msgs) + 500, CompletionTokens: 20}
	if req == 1 {
		for i := 0; i < b.n; i++ {
			id := fmt.Sprintf("toolu_%02d", i)
			args, _ := json.Marshal(map[string]string{"path": fmt.Sprintf("f%02d.go", i)})
			cb(llm.StreamEvent{Type: llm.EventToolUseStart, ToolUseID: id, ToolName: "read_file"})
			cb(llm.StreamEvent{Type: llm.EventToolUseDone, ToolUseID: id, ToolName: "read_file", CompleteInput: string(args)})
		}
		cb(llm.StreamEvent{Type: llm.EventMessageDone, Usage: usage, FinishReason: "tool_calls"})
		return nil
	}
	if req == 2 {
		b.mu.Lock()
		for _, m := range msgs {
			if m.Role == "tool" && strings.Contains(m.Content, "recall_tool_result with id") {
				b.elided = append(b.elided, m.ToolCallID)
			}
		}
		b.mu.Unlock()
	}
	cb(llm.StreamEvent{Type: llm.EventContentDelta, ContentDelta: "TASK_COMPLETE: summarized"})
	cb(llm.StreamEvent{Type: llm.EventMessageDone, Usage: usage, FinishReason: "stop"})
	return nil
}

func (b *batchBackend) SendMessageStream(context.Context, []tui.ChatMessage, []tui.SkillDefinition, llm.StreamCallback) error {
	return fmt.Errorf("not used")
}
func (b *batchBackend) SendMessageSync(context.Context, []tui.ChatMessage, []tui.SkillDefinition) (*llm.ChatCompletionResult, error) {
	return nil, fmt.Errorf("not used")
}
func (b *batchBackend) SetSystemPrompt(string)               {}
func (b *batchBackend) SetThinkingConfig(llm.ThinkingConfig) {}
func (b *batchBackend) Close() error                         { return nil }

// sizedFileTool returns size bytes for any path.
type sizedFileTool struct {
	bigFileTool
	size int
}

func (s sizedFileTool) Execute(_ context.Context, input map[string]any, _ chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	path, _ := input["path"].(string)
	return tools.ToolResult{Content: path + "\n" + strings.Repeat("x", s.size)}, nil
}

// #234 thrash, end to end: 22 results of ~1.5k tokens on a 40k window.
func TestAgentNeverElidesAResultBeforeTheModelSawIt(t *testing.T) {
	backend := &batchBackend{n: 22}
	registry := tools.NewRegistry()
	registry.Register(sizedFileTool{size: 6_000})
	client := llm.NewClientWithBackend(&llm.Config{}, registry, backend)
	client.SetToolMode(tools.ModeAgent)
	runner, _ := newCompactionRunner(t, &windowBackend{}, 40_000)
	runner.client, runner.registry = client, registry
	runner.options.MaxToolCallsPerTurn = 30

	state, err := runner.RunGoal(context.Background(), "summarize every file")
	if err != nil || state.Status != StatusCompleted {
		t.Fatalf("run: %v, status %q (%s)", err, state.Status, state.Error)
	}
	if backend.requests < 2 {
		t.Fatalf("requests = %d, want the batch and its follow-up", backend.requests)
	}
	if len(backend.elided) > 0 {
		t.Fatalf("results elided before the model saw them: %v", backend.elided)
	}
}

// #200 end to end: the agent's summary rung carries the todo list and the
// files the run changed, whatever the summarizer wrote.
func TestAgentSummaryCarriesAuthoritativeState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	backend := &windowBackend{window: 64_000, turns: 20}
	runner, _ := newCompactionRunner(t, backend, 64_000)
	runner.pruned = nil // force the summary rung
	ws := runner.options.Workspace
	builtin.NewTodoStore(ws).Create("read every file", "")
	env, err := loop.Setup(loop.ModeAgent, &config.Config{APIKey: "k", BaseURL: "http://127.0.0.1:1", Model: "m"}, ws, loop.SetupOptions{SessionID: "agent-state", Warn: func(string) {}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(env.Close)
	changed := filepath.Join(ws, "notes.md")
	if err := os.WriteFile(changed, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := env.Snapshots.Checkpoint(changed, "call_0"); err != nil {
		t.Fatal(err)
	}
	runner.env = env
	var summaries []string
	runner.summarize = func(_ context.Context, _, _ string) (string, error) {
		return "## Goal\nread every file", nil // omits todos and files
	}
	backend.onRequest = func(msgs []tui.ChatMessage) {
		if len(msgs) > 0 && compact.IsSummary(msgs[0]) {
			summaries = append(summaries, msgs[0].Content)
		}
	}
	if _, err := runner.RunGoal(context.Background(), "read every file"); err != nil {
		t.Fatal(err)
	}
	if len(summaries) == 0 {
		t.Fatal("no request carried a summary")
	}
	for _, want := range []string{"- [ ] 1. read every file (pending)", "- notes.md"} {
		if !strings.Contains(summaries[0], want) {
			t.Errorf("summary lacks %q:\n%s", want, summaries[0])
		}
	}
}

// The agent's meter counts the tool schemas the run offers, not only the
// system prompt: before the first provider count it is all there is (#234
// item 1).
func TestAgentBudgetCountsToolSchemas(t *testing.T) {
	isolateHome(t)
	srv := fakeprovider.NewOpenAI(t)
	opts := DefaultOptions()
	opts.Workspace = t.TempDir()
	r, err := NewRunner(fakeCfg(srv), opts, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if r.budget.ToolDefinitionTokens <= 0 {
		t.Fatalf("ToolDefinitionTokens = %d, want the offered tools' schemas", r.budget.ToolDefinitionTokens)
	}
}

// When the provider counts more than the estimate, the surplus still counts
// after a prune: a prune that leaves the next request over the threshold
// escalates to the summary rung, as the chat's compactor does.
func TestAgentSummaryRungSeesTheProviderSurplus(t *testing.T) {
	runner, _ := newCompactionRunner(t, &windowBackend{}, 64_000)
	var summaries int
	runner.summarize = func(context.Context, string, string) (string, error) {
		summaries++
		return "## Goal\nread every file", nil
	}
	// A long request (~10k tokens) that no prune can shrink, then reads.
	msgs := []tui.ChatMessage{{Role: "user", Content: "read every file\n" + strings.Repeat("y", 40_000)}}
	for i := 0; i < 16; i++ {
		id := fmt.Sprintf("toolu_%03d", i)
		msgs = append(msgs,
			tui.ChatMessage{Role: "assistant", ToolCalls: []tui.ToolCallInfo{{ID: id, Name: "read_file", Arguments: fmt.Sprintf(`{"path":"f%03d.go"}`, i)}}},
			tui.ChatMessage{Role: "tool", ToolCallID: id, Name: "read_file", Content: strings.Repeat("x", 24_000)})
	}
	msgs = append(msgs, tui.ChatMessage{Role: "assistant", Content: "reading on"})
	// ~106k estimated; the provider counted 40k more (images, a different
	// tokenizer). The prune leaves ~23k by the estimate, ~63k by the
	// provider's count: over the 48k threshold.
	c := &runCompactor{r: runner, meter: compact.NewMeter(0)}
	c.meter.Sending(msgs[:len(msgs)-1])
	if _, _, changed := c.Compact(context.Background(), msgs, &llm.TokenUsage{PromptTokens: compact.Estimate(msgs) + 40_000}, false); !changed {
		t.Fatal("nothing compacted")
	}
	if summaries == 0 {
		t.Fatal("the summary rung ignored the provider's surplus")
	}
}

// A large batch the model has not seen yet keeps the history over the
// threshold, but the prune must leave it alone and a summary cannot split
// it: the summary rung waits for the next call, after the model has seen
// the batch, unless the request would leave the reply no room: past the
// midpoint between the threshold and the window (#234).
func TestAgentSkipsTheSummaryWhileAnUnseenBatchFits(t *testing.T) {
	runner, _ := newCompactionRunner(t, &windowBackend{}, 40_000)
	var summaries int
	runner.summarize = func(context.Context, string, string) (string, error) {
		summaries++
		return "## Goal\nread every file", nil
	}
	read := func(msgs []tui.ChatMessage, id string, chars int) []tui.ChatMessage {
		return append(msgs,
			tui.ChatMessage{Role: "assistant", ToolCalls: []tui.ToolCallInfo{{ID: id, Name: "read_file", Arguments: fmt.Sprintf(`{"path":"%s.go"}`, id)}}},
			tui.ChatMessage{Role: "tool", ToolCallID: id, Name: "read_file", Content: strings.Repeat("x", chars)})
	}
	seen := []tui.ChatMessage{{Role: "user", Content: "read every file"}}
	for i := 0; i < 3; i++ {
		seen = read(seen, fmt.Sprintf("s%d", i), 800)
	}
	batch := func(chars int) []tui.ChatMessage {
		msgs := append([]tui.ChatMessage{}, seen...)
		calls := tui.ChatMessage{Role: "assistant"}
		var results []tui.ChatMessage
		for i := 0; i < 22; i++ {
			id := fmt.Sprintf("b%02d", i)
			calls.ToolCalls = append(calls.ToolCalls, tui.ToolCallInfo{ID: id, Name: "read_file", Arguments: fmt.Sprintf(`{"path":"%s.go"}`, id)})
			results = append(results, tui.ChatMessage{Role: "tool", ToolCallID: id, Name: "read_file", Content: strings.Repeat("y", chars)})
		}
		return append(append(msgs, calls), results...)
	}

	// ~29k: over the 24k threshold, well inside the 40k window.
	msgs := batch(5_000)
	c := &runCompactor{r: runner, meter: compact.NewMeter(0)}
	c.meter.Sending(seen)
	c.Compact(context.Background(), msgs, &llm.TokenUsage{PromptTokens: compact.Estimate(seen)}, false)
	if summaries != 0 {
		t.Fatalf("summarized %d times while the unseen batch still fits the window", summaries)
	}

	// ~36k: inside the window, but past the midpoint between the threshold
	// and the window, so the reply would have no room. Summarize.
	msgs = batch(6_400)
	c = &runCompactor{r: runner, meter: compact.NewMeter(0)}
	c.meter.Sending(seen)
	c.Compact(context.Background(), msgs, &llm.TokenUsage{PromptTokens: compact.Estimate(seen)}, false)
	if summaries != 1 {
		t.Fatalf("summaries = %d, want 1 once the request leaves the reply no room", summaries)
	}

	// Past the window itself: the request cannot be sent, so summarize.
	msgs = batch(8_000)
	c = &runCompactor{r: runner, meter: compact.NewMeter(0)}
	c.meter.Sending(seen)
	c.Compact(context.Background(), msgs, &llm.TokenUsage{PromptTokens: compact.Estimate(seen)}, false)
	if summaries != 2 {
		t.Fatalf("summaries = %d, want 2: the request exceeds the window", summaries)
	}
}

// A provider that reports usage once must not leave the meter at that
// pre-prune count: once the history is pruned, later replies without usage
// must see the smaller history, not prune or summarize again every call.
func TestAgentDoesNotRecompactAfterAPruneWithoutNewUsage(t *testing.T) {
	runner, _ := newCompactionRunner(t, &windowBackend{}, 64_000)
	var summaries int
	runner.summarize = func(context.Context, string, string) (string, error) {
		summaries++
		return "## Goal\nread every file", nil
	}
	msgs := []tui.ChatMessage{{Role: "user", Content: "read every file"}}
	for i := 0; i < 14; i++ {
		id := fmt.Sprintf("toolu_%03d", i)
		msgs = append(msgs,
			tui.ChatMessage{Role: "assistant", ToolCalls: []tui.ToolCallInfo{{ID: id, Name: "read_file", Arguments: fmt.Sprintf(`{"path":"f%03d.go"}`, i)}}},
			tui.ChatMessage{Role: "tool", ToolCallID: id, Name: "read_file", Content: strings.Repeat("x", 16_000)})
	}
	msgs = append(msgs, tui.ChatMessage{Role: "assistant", Content: "reading on"})
	c := &runCompactor{r: runner, meter: compact.NewMeter(0)}
	c.meter.Sending(msgs[:len(msgs)-1])
	out, _, changed := c.Compact(context.Background(), msgs, &llm.TokenUsage{PromptTokens: compact.Estimate(msgs[:len(msgs)-1])}, false)
	if !changed {
		t.Fatal("the first call did not compact a history over the threshold")
	}
	first := summaries
	for call := 2; call <= 4; call++ {
		out = append(out, tui.ChatMessage{Role: "user", Content: "go on"}, tui.ChatMessage{Role: "assistant", Content: "ok"})
		var again bool
		out, _, again = c.Compact(context.Background(), out, &llm.TokenUsage{}, false)
		if again || summaries != first {
			t.Fatalf("call %d compacted again (changed=%v, summaries %d -> %d): used = %d for an estimate of %d",
				call, again, first, summaries, c.meter.Used(out), compact.Estimate(out))
		}
	}
}

// L2: a 32k run whose system prompt and tool schemas are ~16k must not
// summarize (or try to) on every call while the history is short.
func TestAgentNoSummaryLoopAt32k(t *testing.T) {
	runner, _ := newCompactionRunner(t, &windowBackend{}, 32_768)
	runner.budget.SystemPromptTokens, runner.budget.ToolDefinitionTokens = 7_500, 9_000
	var summaries int
	runner.summarize = func(context.Context, string, string) (string, error) {
		summaries++
		return "## Goal\nread every file", nil
	}
	msgs := []tui.ChatMessage{{Role: "user", Content: "fix the bug"}}
	c := &runCompactor{r: runner, meter: compact.NewMeter(0)}
	for i := 0; i < 6; i++ {
		id := fmt.Sprintf("t%d", i)
		msgs = append(msgs,
			tui.ChatMessage{Role: "assistant", ToolCalls: []tui.ToolCallInfo{{ID: id, Name: "read_file", Arguments: fmt.Sprintf(`{"path":"%s.go"}`, id)}}},
			tui.ChatMessage{Role: "tool", ToolCallID: id, Name: "read_file", Content: strings.Repeat("x", 5_600)})
		var usage *llm.TokenUsage
		if i > 0 {
			usage = &llm.TokenUsage{PromptTokens: compact.Estimate(msgs[:len(msgs)-2]) + 16_500}
		}
		msgs, _, _ = c.Compact(context.Background(), msgs, usage, false)
	}
	if summaries != 0 {
		t.Fatalf("summarized %d times with ~8.5k of history under a 16.5k prefix", summaries)
	}
}

// Past the ceiling with only a previous summary before the kept tail, the
// urgent rung does not call the summarizer: it would only re-summarize the
// summary.
func TestAgentUrgentSummaryNeedsHistory(t *testing.T) {
	runner, _ := newCompactionRunner(t, &windowBackend{}, 32_768)
	runner.budget.SystemPromptTokens, runner.budget.ToolDefinitionTokens = 7_500, 9_000
	var summaries int
	runner.summarize = func(context.Context, string, string) (string, error) {
		summaries++
		return "## Goal\nread", nil
	}
	// A previous summary, then one request bigger than the history room.
	msgs := compact.SummaryMessages("## Goal\nread\n"+strings.Repeat("s", 4_000), "", true)
	msgs = append(msgs, tui.ChatMessage{Role: "user", Content: "read this\n" + strings.Repeat("y", 4*12_000)})
	c := &runCompactor{r: runner, meter: compact.NewMeter(0)}
	c.Compact(context.Background(), msgs, nil, false)
	if summaries != 0 {
		t.Fatalf("re-summarized the previous summary %d times", summaries)
	}
}
