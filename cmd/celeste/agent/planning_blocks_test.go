package agent

import (
	"context"
	"encoding/json"
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
	plan  string
	pb    *tui.ProviderBlocks
	calls int
}

func (b *planBackend) SendMessageStream(context.Context, []tui.ChatMessage, []tui.SkillDefinition, llm.StreamCallback) error {
	return nil
}
func (b *planBackend) SendMessageStreamEvents(_ context.Context, _ []tui.ChatMessage, _ []tui.SkillDefinition, cb llm.StreamEventCallback) error {
	b.calls++
	if b.calls == 1 {
		cb(llm.StreamEvent{Type: llm.EventContentDelta, ContentDelta: b.plan})
		cb(llm.StreamEvent{Type: llm.EventMessageDone, FinishReason: "stop", ProviderBlocks: b.pb})
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
}
