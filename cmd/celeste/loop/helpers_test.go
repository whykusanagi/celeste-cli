package loop

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

// fakeTool is a configurable tools.Tool. run defaults to returning "ok:<name>".
type fakeTool struct {
	name     string
	safe     bool
	readOnly bool
	timeout  time.Duration
	run      func(ctx context.Context, input map[string]any) (tools.ToolResult, error)
}

func (f *fakeTool) Name() string        { return f.name }
func (f *fakeTool) Description() string { return "test tool " + f.name }
func (f *fakeTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"k":{"type":"string"}}}`)
}
func (f *fakeTool) IsConcurrencySafe(map[string]any) bool      { return f.safe }
func (f *fakeTool) IsReadOnly() bool                           { return f.readOnly }
func (f *fakeTool) ValidateInput(map[string]any) error         { return nil }
func (f *fakeTool) InterruptBehavior() tools.InterruptBehavior { return tools.InterruptCancel }
func (f *fakeTool) Timeout() time.Duration                     { return f.timeout }
func (f *fakeTool) Execute(ctx context.Context, input map[string]any, _ chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	if f.run != nil {
		return f.run(ctx, input)
	}
	return tools.ToolResult{Content: "ok:" + f.name}, nil
}

func newRegistry(ts ...tools.Tool) *tools.Registry {
	r := tools.NewRegistry()
	for _, t := range ts {
		r.Register(t)
	}
	return r
}

func hermetic(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}

// newClient is the real llm.Client against a scripted fake provider.
func newClient(srv *fakeprovider.Server, reg *tools.Registry) *llm.Client {
	return llm.NewClient(&llm.Config{APIKey: "k", BaseURL: srv.BaseURL(), Model: "fake-model", Timeout: 10 * time.Second}, reg)
}

// stubLLM is an LLM whose replies a test controls call by call.
type stubLLM struct {
	mu    sync.Mutex
	calls int
	seen  [][]Message
	reply func(n int, ctx context.Context, cb llm.StreamEventCallback) error
}

func (s *stubLLM) SendMessageStreamEvents(ctx context.Context, m []tui.ChatMessage, _ []tui.SkillDefinition, cb llm.StreamEventCallback) error {
	s.mu.Lock()
	n := s.calls
	s.calls++
	s.seen = append(s.seen, append([]Message(nil), m...))
	s.mu.Unlock()
	return s.reply(n, ctx, cb)
}
func (s *stubLLM) GetSkills() []tui.SkillDefinition { return nil }

func (s *stubLLM) request(n int) []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.seen[n]
}

func sayText(cb llm.StreamEventCallback, text string, usage *llm.TokenUsage) {
	cb(llm.StreamEvent{Type: llm.EventContentDelta, ContentDelta: text})
	cb(llm.StreamEvent{Type: llm.EventMessageDone, FinishReason: "stop", Usage: usage})
}

func callTool(cb llm.StreamEventCallback, id, name, args string) {
	cb(llm.StreamEvent{Type: llm.EventToolUseStart, ToolUseID: id, ToolName: name})
	cb(llm.StreamEvent{Type: llm.EventToolUseDone, ToolUseID: id, ToolName: name, CompleteInput: args})
	cb(llm.StreamEvent{Type: llm.EventMessageDone, FinishReason: "tool_calls"})
}

// collect drains one Run's events. Call it before Run; the returned func
// blocks until EventDone has been received.
func collect(l *Loop) func() []Event {
	ch := l.Events()
	var evs []Event
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range ch {
			evs = append(evs, ev)
			if ev.Kind == EventDone {
				return
			}
		}
	}()
	return func() []Event { <-done; return evs }
}

func kinds(evs []Event) []EventKind {
	var out []EventKind
	for _, e := range evs {
		if e.Kind != EventTextDelta {
			out = append(out, e.Kind)
		}
	}
	return out
}

func toJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
