package agent

import (
	"context"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

type recordingBackend struct{ calls int }

func (b *recordingBackend) SendMessageStream(context.Context, []tui.ChatMessage, []tui.SkillDefinition, llm.StreamCallback) error {
	return nil
}
func (b *recordingBackend) SendMessageStreamEvents(_ context.Context, _ []tui.ChatMessage, _ []tui.SkillDefinition, cb llm.StreamEventCallback) error {
	b.calls++
	cb(llm.StreamEvent{Type: llm.EventContentDelta, ContentDelta: "TASK_COMPLETE: done"})
	cb(llm.StreamEvent{Type: llm.EventMessageDone, FinishReason: "stop"})
	return nil
}
func (b *recordingBackend) SendMessageSync(context.Context, []tui.ChatMessage, []tui.SkillDefinition) (*llm.ChatCompletionResult, error) {
	return &llm.ChatCompletionResult{Content: "TASK_COMPLETE: done"}, nil
}
func (b *recordingBackend) SetSystemPrompt(string)               {}
func (b *recordingBackend) SetThinkingConfig(llm.ThinkingConfig) {}
func (b *recordingBackend) Close() error                         { return nil }

func TestNewRunnerUsesInjectedClient(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	be := &recordingBackend{}
	client := llm.NewClientWithBackend(&llm.Config{Model: "fake"}, nil, be)
	opts := DefaultOptions()
	opts.Workspace = t.TempDir()
	opts.Client = client
	opts.EnablePlanning = false
	opts.RequireVerification = false
	r, err := NewRunner(&config.Config{Model: "fake", BaseURL: "http://127.0.0.1:1"}, opts, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err := r.RunGoal(context.Background(), "say done"); err != nil {
		t.Fatal(err)
	}
	if be.calls == 0 {
		t.Fatal("NewRunner ignored Options.Client and built its own client")
	}
}
