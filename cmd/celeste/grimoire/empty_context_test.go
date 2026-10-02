package grimoire

import (
	"os"
	"path/filepath"
	"testing"
)

// An empty or blank project grimoire renders no context, so it does not
// count as project context: the chat still shows the /init hint.
func TestBlankGrimoireIsNoProjectContext(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	ws := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(ws, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	mk(t, filepath.Join(ws, ".grimoire"), "")
	if HasProjectContext(ws) {
		t.Error("an empty .grimoire counted as project context")
	}
	mk(t, filepath.Join(ws, ".grimoire"), " \n\t\n")
	if HasProjectContext(ws) {
		t.Error("a blank .grimoire counted as project context")
	}
	mk(t, filepath.Join(ws, ".grimoire"), "# Project\n")
	if !HasProjectContext(ws) {
		t.Error("a .grimoire with content is project context")
	}
}
