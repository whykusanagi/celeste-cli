package steer

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/loop"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/rules"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// script is a loop.LLM that plays one reply per request: text, or a tool
// call written as "call:<tool>:<json args>".
type script struct {
	mu      sync.Mutex
	replies []string
	seen    [][]tui.ChatMessage
}

func (s *script) SendMessageStreamEvents(_ context.Context, m []tui.ChatMessage, _ []tui.SkillDefinition, cb llm.StreamEventCallback) error {
	s.mu.Lock()
	n := len(s.seen)
	s.seen = append(s.seen, append([]tui.ChatMessage(nil), m...))
	s.mu.Unlock()
	reply := "(script exhausted)"
	if n < len(s.replies) {
		reply = s.replies[n]
	}
	if rest, ok := strings.CutPrefix(reply, "call:"); ok {
		name, args, _ := strings.Cut(rest, ":")
		id := "c" + string(rune('0'+n))
		cb(llm.StreamEvent{Type: llm.EventToolUseStart, ToolUseID: id, ToolName: name})
		cb(llm.StreamEvent{Type: llm.EventToolUseDone, ToolUseID: id, ToolName: name, CompleteInput: args})
		cb(llm.StreamEvent{Type: llm.EventMessageDone, FinishReason: "tool_calls"})
		return nil
	}
	cb(llm.StreamEvent{Type: llm.EventContentDelta, ContentDelta: reply})
	cb(llm.StreamEvent{Type: llm.EventMessageDone, FinishReason: "stop"})
	return nil
}

func (s *script) GetSkills() []tui.SkillDefinition { return nil }

type okTool struct{ name string }

func (t okTool) Name() string                               { return t.name }
func (t okTool) Description() string                        { return t.name }
func (t okTool) Parameters() json.RawMessage                { return json.RawMessage(`{"type":"object"}`) }
func (t okTool) IsConcurrencySafe(map[string]any) bool      { return false }
func (t okTool) IsReadOnly() bool                           { return false }
func (t okTool) ValidateInput(map[string]any) error         { return nil }
func (t okTool) InterruptBehavior() tools.InterruptBehavior { return tools.InterruptCancel }
func (t okTool) Execute(context.Context, map[string]any, chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	return tools.ToolResult{Content: "ok"}, nil
}

func run(t *testing.T, s *Session, replies ...string) ([]loop.Message, loop.Result, *script) {
	t.Helper()
	sc := &script{replies: replies}
	reg := tools.NewRegistry()
	for _, n := range []string{"write_file", "bash", "generate_speech"} {
		reg.Register(okTool{name: n})
	}
	l := &loop.Loop{Client: sc, Tools: reg, Limits: loop.DefaultLimits(), Steering: s.Steering()}
	msgs, res, err := l.Run(context.Background(), []loop.Message{{Role: "user", Content: "go"}})
	if err != nil {
		t.Fatal(err)
	}
	return msgs, res, sc
}

func builtins() *rules.Set { return &rules.Set{Rules: rules.Builtins()} }

func reminders(msgs []loop.Message) []string {
	var out []string
	for _, m := range msgs {
		if src, ok := m.Metadata[loop.MetaReminder].(string); ok {
			out = append(out, src)
		}
	}
	return out
}

func TestNewIsNilWhenOff(t *testing.T) {
	if New(Options{Rules: builtins(), RulesMode: "off"}) != nil || New(Options{RulesMode: "on"}) != nil {
		t.Error("off, or no rules, must give no Session")
	}
	var s *Session
	if s.Steering() != nil {
		t.Error("a nil Session must be a nil loop.Steering")
	}
}

func TestShadowLogsAndCountsButNeverActs(t *testing.T) {
	rules.ResetStats()
	var logged []string
	s := New(Options{Rules: builtins(), RulesMode: "shadow", Logf: func(l string) { logged = append(logged, l) }})
	msgs, res, sc := run(t, s, "Audio saved: /tmp/x.mp3")
	if res.FinalText != "Audio saved: /tmp/x.mp3" || len(sc.seen) != 1 || len(reminders(msgs)) != 0 {
		t.Fatalf("shadow acted: res=%+v requests=%d", res, len(sc.seen))
	}
	if len(logged) != 1 || !strings.Contains(logged[0], "unbacked-audio-claim would interrupt") {
		t.Errorf("logged = %v", logged)
	}
	if snap := rules.Snapshot(); snap["shadowed"] != 1 || snap["acted"] != 0 {
		t.Errorf("stats = %v", snap)
	}
}

func TestOnInterruptsAnUnbackedAudioClaim(t *testing.T) {
	s := New(Options{Rules: builtins(), RulesMode: "on"})
	msgs, res, _ := run(t, s, "Audio saved: /tmp/x.mp3", "No audio was made; generate_speech did not run.")
	if !strings.HasPrefix(res.FinalText, "No audio was made") {
		t.Fatalf("final = %q", res.FinalText)
	}
	if got := reminders(msgs); len(got) != 1 || got[0] != "rule:unbacked-audio-claim" {
		t.Errorf("reminders = %v", got)
	}
}

func TestOnAppendsVoiceReminderAfterTheWrite(t *testing.T) {
	s := New(Options{Rules: builtins(), RulesMode: "on"})
	msgs, _, sc := run(t, s, `call:write_file:{"path":"main.go","content":"// done, darling~"}`, "Rewritten plainly.")
	if got := reminders(msgs); len(got) != 1 || got[0] != "rule:persona-voice-in-files" {
		t.Fatalf("reminders = %v", got)
	}
	second := sc.seen[1]
	if second[len(second)-2].Role != "tool" || second[len(second)-1].Metadata[loop.MetaReminder] != "rule:persona-voice-in-files" {
		t.Errorf("the reminder must follow the write's result: %+v", second)
	}
}

func TestRuntimeVerifiesSilencesTaskCompleteRule(t *testing.T) {
	for _, verifies := range []bool{false, true} {
		s := New(Options{Rules: builtins(), RulesMode: "on", RuntimeVerifies: verifies})
		_, _, sc := run(t, s, `call:write_file:{"path":"a.go","content":"package a"}`, "TASK_COMPLETE: wrote a.go", "TASK_COMPLETE: ran nothing yet")
		want := 3
		if verifies {
			want = 2
		}
		if len(sc.seen) != want {
			t.Errorf("RuntimeVerifies=%v: requests = %d, want %d", verifies, len(sc.seen), want)
		}
	}
}

// An interrupt the loop did not honour (out of re-runs) is not lost: its
// reminder joins at the next boundary.
func TestUnhonouredInterruptReminderJoinsLater(t *testing.T) {
	s := New(Options{Rules: builtins(), RulesMode: "on"})
	s.Request(1, nil)
	if hit := s.Observe(loop.Event{Kind: loop.EventTextDelta, Text: "Audio saved: x"}); !hit && !s.EndStream() {
		t.Fatal("the rule must ask to interrupt")
	}
	if got := s.Reminders(loop.BoundaryTools); len(got) != 1 || got[0].Source != "rule:unbacked-audio-claim" {
		t.Errorf("reminders = %+v", got)
	}
}
