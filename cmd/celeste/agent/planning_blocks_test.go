package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

const planKey = "test-format||fake"

// planBackend answers the planning request with plan (and blocks), then
// completes.
type planBackend struct {
	plan     string
	pb       *tui.ProviderBlocks
	toolUse  bool  // the planning reply also calls a tool
	rejected bool  // the planning reply reports BlocksRejected
	failErr  error // the planning request fails with this
	calls    int
}

func (b *planBackend) SendMessageStream(context.Context, []tui.ChatMessage, []tui.SkillDefinition, llm.StreamCallback) error {
	return nil
}
func (b *planBackend) SendMessageStreamEvents(_ context.Context, _ []tui.ChatMessage, _ []tui.SkillDefinition, cb llm.StreamEventCallback) error {
	b.calls++
	if b.calls == 1 && b.failErr != nil {
		return b.failErr
	}
	if b.calls == 1 {
		cb(llm.StreamEvent{Type: llm.EventContentDelta, ContentDelta: b.plan})
		finish := "stop"
		if b.toolUse {
			cb(llm.StreamEvent{Type: llm.EventToolUseStart, ToolUseID: "p1", ToolName: "read_file"})
			cb(llm.StreamEvent{Type: llm.EventToolUseDone, ToolUseID: "p1", ToolName: "read_file", CompleteInput: `{"path":"a"}`})
			finish = "tool_use"
		}
		cb(llm.StreamEvent{Type: llm.EventMessageDone, FinishReason: finish, ProviderBlocks: b.pb, BlocksRejected: b.rejected})
		return nil
	}
	cb(llm.StreamEvent{Type: llm.EventContentDelta, ContentDelta: "TASK_COMPLETE: done"})
	cb(llm.StreamEvent{Type: llm.EventMessageDone, FinishReason: "stop"})
	return nil
}
func (b *planBackend) SendMessageSync(context.Context, []tui.ChatMessage, []tui.SkillDefinition) (*llm.ChatCompletionResult, error) {
	return &llm.ChatCompletionResult{Content: "TASK_COMPLETE: done"}, nil
}
func (b *planBackend) SetSystemPrompt(string)               {}
func (b *planBackend) SetThinkingConfig(llm.ThinkingConfig) {}
func (b *planBackend) Close() error                         { return nil }

func runPlanned(t *testing.T, be *planBackend) *RunState {
	t.Helper()
	isolateHome(t)
	opts := DefaultOptions()
	opts.Workspace = t.TempDir()
	opts.Client = llm.NewClientWithBackend(&llm.Config{Model: "fake"}, nil, be)
	opts.EnablePlanning = true
	opts.PlanningExplicit = true
	opts.RequireVerification = false
	r, err := NewRunner(&config.Config{Model: "fake", BaseURL: "http://127.0.0.1:1"}, opts, nil, nil)
	require.NoError(t, err)
	defer r.Close()
	state, err := r.RunGoal(context.Background(), "read the file")
	require.NoError(t, err)
	return state
}

func planMessage(t *testing.T, state *RunState, content string) tui.ChatMessage {
	t.Helper()
	for _, m := range state.Messages {
		if m.Role == "assistant" && m.Content == content {
			return m
		}
	}
	t.Fatalf("no plan message %q in %d messages", content, len(state.Messages))
	return tui.ChatMessage{}
}

// The planning reply is part of the history later turns send, so it keeps
// its blocks, unless TrimSpace changed its text (ruling 6).
func TestPlanningReplyKeepsProviderBlocks(t *testing.T) {
	pb, err := tui.NewProviderBlocks(planKey, []json.RawMessage{json.RawMessage(`{"type":"thinking","thinking":"plan","signature":"P"}`)})
	require.NoError(t, err)

	t.Run("untrimmed", func(t *testing.T) {
		state := runPlanned(t, &planBackend{plan: "1. Read the file", pb: pb})
		got, ok := tui.ReplayBlocks(planMessage(t, state, "1. Read the file"), planKey)
		require.True(t, ok, "the plan message lost its blocks")
		assert.Equal(t, pb.Blocks, got)
	})
	t.Run("trimmed", func(t *testing.T) {
		state := runPlanned(t, &planBackend{plan: "  1. Read the file\n", pb: pb})
		assert.Nil(t, planMessage(t, state, "1. Read the file").ProviderBlocks, "trimmed text no longer matches the blocks")
	})
	// The planning request offers tools but records no calls: blocks with a
	// tool_use would replay a call that never gets a result (ruling 6).
	t.Run("tool use", func(t *testing.T) {
		state := runPlanned(t, &planBackend{plan: "1. Read the file", pb: pb, toolUse: true})
		assert.Nil(t, planMessage(t, state, "1. Read the file").ProviderBlocks, "a planning reply that called a tool keeps no blocks")
	})
}

// A planning reply that reports BlocksRejected strips the blocks the request
// replayed, as the loop does; the plan reply keeps its own fresh blocks.
func TestPlanningStripsRejectedBlocks(t *testing.T) {
	isolateHome(t)
	old, err := tui.NewProviderBlocks(planKey, []json.RawMessage{json.RawMessage(`{"type":"text","text":"earlier"}`)})
	require.NoError(t, err)
	fresh, err := tui.NewProviderBlocks(planKey, []json.RawMessage{json.RawMessage(`{"type":"text","text":"plan"}`)})
	require.NoError(t, err)
	be := &planBackend{plan: "1. Read the file", pb: fresh, rejected: true}
	opts := DefaultOptions()
	opts.Workspace = t.TempDir()
	opts.Client = llm.NewClientWithBackend(&llm.Config{Model: "fake"}, nil, be)
	opts.EnablePlanning = true
	opts.PlanningExplicit = true
	r, err := NewRunner(&config.Config{Model: "fake", BaseURL: "http://127.0.0.1:1"}, opts, nil, nil)
	require.NoError(t, err)
	defer r.Close()
	earlier := tui.AttachProviderBlocks(tui.ChatMessage{Role: "assistant", Content: "earlier"}, old)
	held := []tui.ChatMessage{{Role: "user", Content: "go"}, earlier}
	state := &RunState{Goal: "read the file", Options: opts, Messages: held}
	require.NoError(t, r.runPlanningPhase(context.Background(), state))
	assert.Nil(t, state.Messages[1].ProviderBlocks, "the rejected blocks are stripped")
	assert.NotNil(t, held[1].ProviderBlocks, "copy-on-write")
	got, ok := tui.ReplayBlocks(planMessage(t, state, "1. Read the file"), planKey)
	require.True(t, ok)
	assert.Equal(t, fresh.Blocks, got)
}

// W8-1 review M4: the planning request's replayed blocks were refused and
// the resend failed too. The error ends the phase, and the history no
// longer carries the refused blocks.
func TestPlanningStripsRejectedBlocksOnError(t *testing.T) {
	isolateHome(t)
	old, err := tui.NewProviderBlocks(planKey, []json.RawMessage{json.RawMessage(`{"type":"text","text":"earlier"}`)})
	require.NoError(t, err)
	be := &planBackend{failErr: &llm.BlocksRejectedError{Err: errors.New("resend failed")}}
	opts := DefaultOptions()
	opts.Workspace = t.TempDir()
	opts.Client = llm.NewClientWithBackend(&llm.Config{Model: "fake"}, nil, be)
	opts.EnablePlanning = true
	opts.PlanningExplicit = true
	r, err := NewRunner(&config.Config{Model: "fake", BaseURL: "http://127.0.0.1:1"}, opts, nil, nil)
	require.NoError(t, err)
	defer r.Close()
	earlier := tui.AttachProviderBlocks(tui.ChatMessage{Role: "assistant", Content: "earlier"}, old)
	held := []tui.ChatMessage{{Role: "user", Content: "go"}, earlier}
	state := &RunState{Goal: "read the file", Options: opts, Messages: held}
	require.Error(t, r.runPlanningPhase(context.Background(), state))
	assert.Nil(t, state.Messages[1].ProviderBlocks, "the rejected blocks are stripped")
	assert.NotNil(t, held[1].ProviderBlocks, "copy-on-write")
}
