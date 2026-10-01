package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// blocksBackend answers each request with the next scripted text and that
// reply's ProviderBlocks (thinking with a signature, then the text), and
// records what it was sent. It stands in for W2's Anthropic backend.
type blocksBackend struct {
	mu      sync.Mutex
	key     string
	replies []string
	seen    [][]tui.ChatMessage
}

func (b *blocksBackend) blocksFor(t *testing.T, text string) *tui.ProviderBlocks {
	t.Helper()
	pb, err := tui.NewProviderBlocks(b.key, []json.RawMessage{
		json.RawMessage(`{"type":"thinking","thinking":"about <` + text + `>","signature":"sig-` + text + `"}`),
		json.RawMessage(`{"type":"text","text":"` + text + `"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	return pb
}

func (b *blocksBackend) request(n int) []tui.ChatMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.seen[n]
}

func (b *blocksBackend) SendMessageStreamEvents(_ context.Context, m []tui.ChatMessage, _ []tui.SkillDefinition, cb llm.StreamEventCallback) error {
	b.mu.Lock()
	n := len(b.seen)
	b.seen = append(b.seen, append([]tui.ChatMessage(nil), m...))
	b.mu.Unlock()
	if n >= len(b.replies) {
		return errors.New("blocksBackend: no reply scripted")
	}
	text := b.replies[n]
	pb, err := tui.NewProviderBlocks(b.key, []json.RawMessage{
		json.RawMessage(`{"type":"thinking","thinking":"about <` + text + `>","signature":"sig-` + text + `"}`),
		json.RawMessage(`{"type":"text","text":"` + text + `"}`),
	})
	if err != nil {
		return err
	}
	cb(llm.StreamEvent{Type: llm.EventContentDelta, ContentDelta: text})
	cb(llm.StreamEvent{Type: llm.EventMessageDone, FinishReason: "stop", ProviderBlocks: pb})
	return nil
}
func (b *blocksBackend) SendMessageStream(context.Context, []tui.ChatMessage, []tui.SkillDefinition, llm.StreamCallback) error {
	return errors.New("blocksBackend: not used")
}
func (b *blocksBackend) SendMessageSync(context.Context, []tui.ChatMessage, []tui.SkillDefinition) (*llm.ChatCompletionResult, error) {
	return nil, errors.New("blocksBackend: not used")
}
func (b *blocksBackend) SetSystemPrompt(string)               {}
func (b *blocksBackend) SetThinkingConfig(llm.ThinkingConfig) {}
func (b *blocksBackend) Close() error                         { return nil }

// useBlocksBackend points the chat's adapter at be. Called before the first
// message, so no run goroutine reads the client concurrently.
func useBlocksBackend(deps *chatDeps, baseURL string, be *blocksBackend) {
	deps.adapter.client = llm.NewClientWithBackend(&llm.Config{BaseURL: baseURL, Model: "fake-model", Timeout: 10 * time.Second}, deps.registry, be)
}

func assertReplays(t *testing.T, msgs []tui.ChatMessage, content, key string, want *tui.ProviderBlocks) {
	t.Helper()
	for _, m := range msgs {
		if m.Role != "assistant" || m.Content != content {
			continue
		}
		got, ok := tui.ReplayBlocks(m, key)
		if !ok {
			t.Fatalf("reply %q was sent without its blocks (have %+v)", content, m.ProviderBlocks)
		}
		if len(got) != len(want.Blocks) {
			t.Fatalf("reply %q: %d blocks, want %d", content, len(got), len(want.Blocks))
		}
		for i := range got {
			if !bytes.Equal(got[i], want.Blocks[i]) {
				t.Fatalf("reply %q block %d = %s, want %s", content, i, got[i], want.Blocks[i])
			}
		}
		return
	}
	t.Fatalf("no assistant message %q in the request", content)
}

func savedBlocks(s config.Session) int {
	n := 0
	for _, m := range s.Messages {
		if m.ProviderBlocks != nil {
			n++
		}
	}
	return n
}

// The chat path W2 depends on (spec W2 test, F3 Review Focus 3): a reply's
// blocks reach the next request through SyncLLM and the typing commit, are
// saved with the session, and a resumed chat sends them again, byte for byte.
func TestTUIProviderBlocksSurviveTurnsAndResume(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t) // the chat's endpoint; blocksBackend answers
	m, deps, ws := chatApp(t, srv)
	key := llm.ProviderKey("test-blocks", srv.BaseURL(), "fake-model")
	be := &blocksBackend{key: key, replies: []string{"first", "second"}}
	useBlocksBackend(deps, srv.BaseURL(), be)

	m = drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "one"}},
		func(m tea.Model) bool { return lastAssistant(m) == "first" && turnIdle(m) }, 30*time.Second)
	drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "two"}},
		func(m tea.Model) bool { return lastAssistant(m) == "second" && turnIdle(m) }, 30*time.Second)
	assertReplays(t, be.request(1), "first", key, be.blocksFor(t, "first"))

	// persistSession is not signalled to the driver (see
	// TestTUIResumeCarriesToolHistory): poll for the save.
	var sessions []config.Session
	deadline := time.Now().Add(2 * time.Second)
	for {
		var err error
		sessions, err = config.NewSessionManager().List()
		if err == nil && len(sessions) > 0 && savedBlocks(sessions[0]) == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the saved session never held both replies' blocks: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	resumeSessionID = sessions[0].ID
	t.Cleanup(func() { resumeSessionID = "" })
	tui.CloseLogging()
	cfg := &config.Config{APIKey: "k", BaseURL: srv.BaseURL(), Model: "fake-model", Timeout: 10}
	app, deps2, err := newChatApp(cfg, ws, os.Getenv("HOME"))
	if err != nil {
		t.Fatal(err)
	}
	cleanupChatDeps(t, deps2)
	be2 := &blocksBackend{key: key, replies: []string{"third"}}
	useBlocksBackend(deps2, srv.BaseURL(), be2)
	var m2 tea.Model = app
	m2, _ = m2.Update(tea.WindowSizeMsg{Width: 160, Height: 50})
	drive(t, m2, []tea.Msg{tui.SendMessageMsg{Content: "three"}},
		func(m tea.Model) bool { return lastAssistant(m) == "third" && turnIdle(m) }, 30*time.Second)
	resumed := be2.request(0)
	assertReplays(t, resumed, "first", key, be.blocksFor(t, "first"))
	assertReplays(t, resumed, "second", key, be.blocksFor(t, "second"))
}

// withoutEmptyReplies drops empty bubbles but keeps a blocks-only reply.
func TestWithoutEmptyRepliesKeepsBlocksOnlyReplies(t *testing.T) {
	pb, err := tui.NewProviderBlocks("k", []json.RawMessage{json.RawMessage(`{"type":"compaction","content":"s"}`)})
	if err != nil {
		t.Fatal(err)
	}
	out := withoutEmptyReplies([]tui.ChatMessage{
		{Role: "user", Content: "q"},
		{Role: "assistant"},
		tui.AttachProviderBlocks(tui.ChatMessage{Role: "assistant"}, pb),
	})
	if len(out) != 2 || out[1].ProviderBlocks == nil {
		t.Fatalf("out = %+v, want the user message and the blocks-only reply", out)
	}
}

// rejectingBackend repeats one tool call with blocks; the third reply reports
// BlocksRejected, and the identical-call guard ends the run at once.
type rejectingBackend struct {
	mu sync.Mutex
	n  int
}

func (b *rejectingBackend) SendMessageStreamEvents(_ context.Context, _ []tui.ChatMessage, _ []tui.SkillDefinition, cb llm.StreamEventCallback) error {
	b.mu.Lock()
	n := b.n
	b.n++
	b.mu.Unlock()
	pb, err := tui.NewProviderBlocks("k", []json.RawMessage{json.RawMessage(`{"type":"tool_use","id":"c1","name":"read_file","input":{"path":"a.txt"}}`)})
	if err != nil {
		return err
	}
	cb(llm.StreamEvent{Type: llm.EventToolUseStart, ToolUseID: "c1", ToolName: "read_file"})
	cb(llm.StreamEvent{Type: llm.EventToolUseDone, ToolUseID: "c1", ToolName: "read_file", CompleteInput: `{"path":"a.txt"}`})
	cb(llm.StreamEvent{Type: llm.EventMessageDone, FinishReason: "tool_calls", ProviderBlocks: pb, BlocksRejected: n == 2})
	return nil
}
func (b *rejectingBackend) SendMessageStream(context.Context, []tui.ChatMessage, []tui.SkillDefinition, llm.StreamCallback) error {
	return errors.New("rejectingBackend: not used")
}
func (b *rejectingBackend) SendMessageSync(context.Context, []tui.ChatMessage, []tui.SkillDefinition) (*llm.ChatCompletionResult, error) {
	return nil, errors.New("rejectingBackend: not used")
}
func (b *rejectingBackend) SetSystemPrompt(string)               {}
func (b *rejectingBackend) SetThinkingConfig(llm.ThinkingConfig) {}
func (b *rejectingBackend) Close() error                         { return nil }

// A strip the run never snapshotted (BlocksRejected, then a guard stop)
// still reaches the chat: the last history it receives has no blocks.
func TestRunTurnSendsTheStrippedHistoryAfterAGuardStop(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t)
	_, deps, ws := chatApp(t, srv)
	os.WriteFile(filepath.Join(ws, "a.txt"), []byte("alpha"), 0o644)
	a := deps.adapter
	a.client = llm.NewClientWithBackend(&llm.Config{BaseURL: srv.BaseURL(), Model: "fake-model", Timeout: 10 * time.Second}, deps.registry, &rejectingBackend{})
	msgs := runTurnMsgs(t, a, tui.TurnRequest{History: userTurn("read a.txt"), Tools: true, Run: 1})
	var last []tui.ChatMessage
	for _, m := range msgs {
		if h, ok := m.(tui.HistoryMsg); ok {
			last = h.History
		}
	}
	if len(last) == 0 {
		t.Fatal("no history reached the chat")
	}
	for i, m := range last {
		if m.ProviderBlocks != nil {
			t.Fatalf("the chat's last history still carries rejected blocks at %d", i)
		}
	}
}
