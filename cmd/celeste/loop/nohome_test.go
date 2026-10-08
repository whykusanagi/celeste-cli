package loop

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/permissions"
)

// With no home directory, Setup never reads the home-level permissions
// file or custom skills relative to the current directory (normally the
// repository): the permission mode stays the default and a repository
// skill is not loaded.
func TestSetupWithoutHomeIgnoresWorkspaceRelativeHomeFiles(t *testing.T) {
	repo := t.TempDir()
	write(t, filepath.Join(repo, ".celeste", "permissions.json"), `{"mode":"trust"}`)
	write(t, filepath.Join(repo, ".celeste", "skills", "repo_tool.json"),
		`{"name":"repo_tool","description":"d","parameters":{"type":"object","properties":{}},"command":"echo hi"}`)
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	t.Setenv("home", "") // plan9
	t.Chdir(repo)

	var w warnings
	env, err := Setup(ModeChat, testCfg(), repo, SetupOptions{Warn: w.add})
	if err != nil {
		t.Fatal(err)
	}
	defer env.Close()

	if got := env.Checker.Mode(); got == permissions.ModeTrust {
		t.Fatalf("permission mode = %v: a workspace-relative permissions file was loaded", got)
	}
	if _, ok := env.Registry.Get("repo_tool"); ok {
		t.Fatal("a workspace-relative custom skill was loaded")
	}
	// "Always allow" is not persisted into the workspace either.
	_ = env.Checker.AddPersistentAllow(permissions.Rule{ToolPattern: "bash", Decision: permissions.Allow})
	data, _ := os.ReadFile(filepath.Join(repo, ".celeste", "permissions.json"))
	if string(data) != `{"mode":"trust"}` {
		t.Fatalf("permissions written into the workspace: %s", data)
	}
}
