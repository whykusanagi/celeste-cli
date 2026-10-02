package gittest

import (
	"os"
	"path/filepath"
	"testing"
)

// The developer's global config (~/.gitconfig: core.hooksPath, signing,
// aliases) never reaches test git: hooks from outside the test's
// repository must not run on its commits.
func TestCommandIgnoresTheGlobalConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte("[core]\n\thooksPath = /nonexistent/hooks\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	Run(t, dir, "init", "-q")
	out, err := Command(dir, "config", "--get", "core.hooksPath").CombinedOutput()
	if err == nil || len(out) != 0 {
		t.Fatalf("global core.hooksPath reached test git: %q (err %v)", out, err)
	}
}
