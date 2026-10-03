package acp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/compact"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

// Ruling 12: past the window's threshold, old tool results are pruned
// before the next request and the editor hears about it.
func TestCompactorPrunesPastTheWindow(t *testing.T) {
	read := func(id, f string) fakeprovider.Turn {
		return fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: id, Name: "read_file", Args: `{"path":"` + f + `"}`}}}
	}
	srv := fakeprovider.NewOpenAI(t, read("r1", "a.txt"), read("r2", "b.txt"), read("r3", "c.txt"), fakeprovider.Turn{Text: "read them"})
	base := testConfig(srv, 0)
	c := newTestClient(t, func() (*config.Config, error) {
		cfg, err := base()
		cfg.ContextLimit = 20000
		return cfg, err
	})
	ws := t.TempDir()
	line := strings.Repeat("abcdefghij", 7) + "\n" // 71 bytes
	for _, f := range []string{"a.txt", "b.txt", "c.txt"} {
		body := strings.Repeat(f[:1]+line, 40<<10/(len(line)+1))
		if err := os.WriteFile(filepath.Join(ws, f), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sid := c.newSession(ws)
	if res, err := c.call("session/prompt", textPrompt(sid, "read a, b and c")); err != nil || stopReason(t, res) != "end_turn" {
		t.Fatalf("prompt = %s %v", res, err)
	}
	reqs := srv.Requests()
	if len(reqs) != 4 {
		t.Fatalf("requests = %d, want 4", len(reqs))
	}
	if third := string(reqs[2].Raw); !strings.Contains(third, "elided to save context") {
		t.Fatalf("the third request carries no pruned result (%d bytes)", len(third))
	}
	found := false
	for _, u := range c.updatesOf("agent_thought_chunk") {
		found = found || strings.Contains(mustJSON(u["content"]), "context compacted")
	}
	if !found {
		t.Fatal("no agent_thought_chunk said the context was compacted")
	}
}

// When pruning is not enough (no tool results to prune) the summary rung
// runs, with PreCompact able to block it.
func TestCompactorSummarizesWhenPruningIsNotEnough(t *testing.T) {
	big := strings.Repeat("word ", 4000) // ~5k tokens per message
	var history []tui.ChatMessage
	for i := 0; i < 8; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		history = append(history, tui.ChatMessage{Role: role, Content: big})
	}
	called := 0
	summarize := func(ctx context.Context, system, user string) (string, error) {
		called++
		return "the summary", nil
	}
	cfg := &config.Config{Model: "fake-model", ContextLimit: 20000}
	store := &compact.Store{Dir: t.TempDir()}
	c := newCompactor(cfg, "system", store, summarize, nil, t.Logf)
	out, notes, changed := c.Compact(context.Background(), history, nil, false)
	if !changed || called != 1 || len(out) >= len(history) {
		t.Fatalf("changed=%v called=%d len=%d", changed, called, len(out))
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "context compacted") {
		t.Fatalf("notes = %v", notes)
	}
	// Under the threshold nothing happens.
	out, notes, changed = c.Compact(context.Background(), history[:1], nil, false)
	if changed || len(notes) != 0 || len(out) != 1 || called != 1 {
		t.Fatalf("under the threshold: changed=%v notes=%v called=%d", changed, notes, called)
	}
}
