package loop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/prompts"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools/builtin"
)

func TestEnvRenderStateReadsTheRecords(t *testing.T) {
	setupHome(t)
	ws := t.TempDir()
	store := builtin.NewTodoStore(ws)
	store.Create("write the lexer", "")
	item := store.Create("port the parser", "")
	if _, err := store.Update(item.ID, "in_progress"); err != nil {
		t.Fatal(err)
	}
	env, err := Setup(ModeAgent, testCfg(), ws, SetupOptions{SessionID: "state-test", Warn: func(string) {}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(env.Close)
	target := filepath.Join(ws, "pkg", "lexer.go")
	write(t, target, "package pkg\n")
	if _, err := env.Snapshots.Checkpoint(target, "call_1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("package pkg // changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := env.RenderState()
	for _, want := range []string{
		"- [ ] 1. write the lexer (pending)",
		"- [~] 2. port the parser (in progress)",
		"- " + filepath.Join("pkg", "lexer.go"),
		strings.SplitN(prompts.VoiceBoundary, "\n", 2)[0],
	} {
		if !strings.Contains(got, want) {
			t.Errorf("RenderState lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, ws) {
		t.Error("files must be relative to the workspace")
	}
}

// With no todos and no changed files the state is the voice rule alone:
// the persona is always on (W5), so the rule always applies.
func TestEnvRenderStateWithoutRecordsIsTheVoiceRule(t *testing.T) {
	setupHome(t)
	env, _ := mustSetup(t, ModeAgent, t.TempDir())
	got := env.RenderState()
	if strings.Contains(got, "Todo list") || strings.Contains(got, "Files modified") || !strings.Contains(got, prompts.VoiceBoundary) {
		t.Fatalf("RenderState = %q, want only the voice rule", got)
	}
}

// A file recorded through a symlinked path is still shown relative to the
// workspace (macOS temp directories live under a /var -> /private/var link).
func TestEnvDisplayPathFollowsSymlinks(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "ws")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	write(t, filepath.Join(real, "pkg", "a.go"), "package pkg\n")
	e := &Env{Workspace: link}
	if got, want := e.displayPath(filepath.Join(real, "pkg", "a.go")), filepath.Join("pkg", "a.go"); got != want {
		t.Fatalf("displayPath = %q, want %q", got, want)
	}
	outside := filepath.Join(t.TempDir(), "b.go")
	if got := e.displayPath(outside); got != outside {
		t.Fatalf("a path outside the workspace = %q, want it unchanged", got)
	}
}
