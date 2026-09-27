package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
)

// Baseline of a known bug: the default runner factory sets neither a prompt
// func nor auto-approve, so mutating tools resolve to Ask and are silently
// denied. 2.0 F2 fixes it (spec §3.2); flip this test then.
//
// agent.DefaultOptions().EnablePlanning is true, so the run makes a planning
// request before any tool call: the script below has a text-only turn first
// (consumed by the planning phase), then the write_file tool call, then a
// completion turn.
func TestOrchestratorSilentlyDeniesMutatingTools(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	ws := t.TempDir()
	wd, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(wd) })
	os.Chdir(ws)

	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{Text: "1. Write hi to out.txt"},
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "w", Name: "write_file", Args: `{"path":"out.txt","content":"hi"}`}}},
		fakeprovider.Turn{Text: "TASK_COMPLETE: wrote it"},
	)
	cfg := &config.Config{APIKey: "k", BaseURL: srv.BaseURL(), Model: "fake-model", Timeout: 10}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	_, _ = New(cfg).Run(ctx, "write hi to out.txt")

	if _, err := os.Stat(filepath.Join(ws, "out.txt")); err == nil {
		t.Fatal("out.txt was written: the silent-denial baseline no longer holds - if F2 landed, flip this test")
	}

	// Assert the denial directly, not just via the file's absence: the request
	// that follows the write_file call must carry a tool-result message that
	// shows the call was denied (registry.go's Ask-with-no-prompt branch).
	reqs := srv.Requests()
	if len(reqs) < 3 {
		t.Fatalf("got %d requests, want at least 3 (plan, tool call, post-denial)", len(reqs))
	}
	msgs, _ := reqs[2].Body["messages"].([]any)
	var toolMsg map[string]any
	for _, m := range msgs {
		mm, _ := m.(map[string]any)
		if mm["role"] == "tool" {
			toolMsg = mm
		}
	}
	if toolMsg == nil {
		t.Fatal("no tool-result message found in the request following the write_file call")
	}
	content, _ := toolMsg["content"].(string)
	if !strings.Contains(content, "Permission denied") || !strings.Contains(content, "no prompt is configured") {
		t.Fatalf("tool result = %q, want a permission-denied message (registry.go's Ask-with-no-prompt branch)", content)
	}
}
