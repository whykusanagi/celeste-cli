package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Ruling 7: /init writes .grimoire, /init agents also AGENTS.md, and an
// existing file is never overwritten.
func TestInitCommandWritesTheGrimoire(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, _ := newCompactTestApp(t)
	m = m.SetWorkDir(ws)
	m, _ = step(t, m, SendMessageMsg{Content: "/init"})
	if _, err := os.Stat(filepath.Join(ws, ".grimoire")); err != nil {
		t.Fatalf("/init did not write .grimoire: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ws, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatal("plain /init must not write AGENTS.md")
	}
	if err := os.WriteFile(filepath.Join(ws, ".grimoire"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, _ = step(t, m, SendMessageMsg{Content: "/init agents"})
	if _, err := os.Stat(filepath.Join(ws, "AGENTS.md")); err != nil {
		t.Fatalf("/init agents did not write AGENTS.md: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(ws, ".grimoire")); string(b) != "mine" {
		t.Fatal("/init overwrote an existing .grimoire")
	}
	msgs := m.DebugMessages()
	if last := msgs[len(msgs)-1].Content; !strings.Contains(last, "already exists") || !strings.Contains(last, "AGENTS.md") {
		t.Fatalf("/init agents should report the existing .grimoire and the new AGENTS.md:\n%s", last)
	}
}

// /grimoire shows the context-file warnings (a cut or skipped file), as
// celeste grimoire does.
func TestGrimoireCommandShowsContextFileWarnings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "AGENTS.md"), []byte(strings.Repeat("a", 40<<10)), 0o644); err != nil {
		t.Fatal(err)
	}
	m, _ := newCompactTestApp(t)
	m = m.SetWorkDir(ws)
	m, _ = step(t, m, SendMessageMsg{Content: "/grimoire"})
	msgs := m.DebugMessages()
	if last := msgs[len(msgs)-1].Content; !strings.Contains(last, "⚠ ") || !strings.Contains(last, "cut to") {
		t.Fatalf("/grimoire should show the cap warning:\n%.300s", last[len(last)-min(300, len(last)):])
	}
}
