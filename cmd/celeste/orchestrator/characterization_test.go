package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
)

// Baseline of a known bug: the default runner factory sets neither a prompt
// func nor auto-approve, so mutating tools resolve to Ask and are silently
// denied. 2.0 F2 fixes it (spec §3.2); flip this test then.
func TestOrchestratorSilentlyDeniesMutatingTools(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	ws := t.TempDir()
	wd, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(wd) })
	os.Chdir(ws)

	var turns []fakeprovider.Turn
	for i := 0; i < 8; i++ {
		turns = append(turns,
			fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "w", Name: "write_file", Args: `{"path":"out.txt","content":"hi"}`}}},
			fakeprovider.Turn{Text: "TASK_COMPLETE: wrote it"})
	}
	srv := fakeprovider.NewOpenAI(t, turns...)
	cfg := &config.Config{APIKey: "k", BaseURL: srv.BaseURL(), Model: "fake-model", Timeout: 10}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	_, _ = New(cfg).Run(ctx, "write hi to out.txt")

	if _, err := os.Stat(filepath.Join(ws, "out.txt")); err == nil {
		t.Fatal("out.txt was written: the silent-denial baseline no longer holds - if F2 landed, flip this test")
	}
}
