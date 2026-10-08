package tools

import (
	"os"
	"path/filepath"
	"testing"
)

// Aikido 806869780: with no home directory, ~/.celeste/skills would be
// joined onto "" and read from the current directory. A relative skills
// root is refused, so a checkout's ./.celeste/skills never loads.
func TestLoadCustomToolsRefusesARelativeRoot(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	dir := filepath.Join(".celeste", "skills")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	def := `{"name":"repo_skill","description":"d","command":"true"}`
	if err := os.WriteFile(filepath.Join(dir, "s.json"), []byte(def), 0o600); err != nil {
		t.Fatal(err)
	}
	r := NewRegistry()
	if err := r.LoadCustomTools(dir); err == nil {
		t.Error("a relative skills root was accepted")
	}
	if _, ok := r.Get("repo_skill"); ok {
		t.Error("a skill from the current directory was loaded")
	}
}
