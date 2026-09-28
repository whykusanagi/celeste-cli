package permissions

import (
	"path/filepath"
	"testing"
)

type protTool struct {
	name string
	ro   bool
}

func (p protTool) ToolName() string { return p.name }
func (p protTool) IsReadOnly() bool { return p.ro }

func TestCheckerProtectsHookFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	c := NewChecker(PermissionConfig{Mode: ModeTrust})
	write := protTool{name: "write_file"}
	bash := protTool{name: "bash"}
	denied := []struct {
		tool  protTool
		input map[string]any
	}{
		{write, map[string]any{"path": filepath.Join(home, ".celeste", "hooks.json"), "content": "{}"}},
		{write, map[string]any{"path": "~/.celeste/grimoire.md"}},
		{bash, map[string]any{"command": "echo '{}' > $HOME/.celeste/trusted.json"}},
		{bash, map[string]any{"command": "cp x ${HOME}/.celeste/hooks.json"}},
		{write, map[string]any{"path": "some/where/.celeste/trusted.json"}},
		{protTool{name: "patch_file"}, map[string]any{"edits": []any{map[string]any{"path": "~/.celeste/hooks.json"}}}},
	}
	for _, d := range denied {
		if got := c.Check(d.tool, d.input); got.Decision != Deny {
			t.Errorf("%s %v: decision %v, want Deny", d.tool.name, d.input, got.Decision)
		}
	}
	allowed := []struct {
		tool  protTool
		input map[string]any
	}{
		{protTool{name: "read_file", ro: true}, map[string]any{"path": filepath.Join(home, ".celeste", "hooks.json")}},
		{write, map[string]any{"path": ".celeste/hooks.json"}}, // repo hooks: untrusted until approved anyway
		{write, map[string]any{"path": "notes.md"}},
	}
	for _, a := range allowed {
		if got := c.Check(a.tool, a.input); got.Decision == Deny {
			t.Errorf("%s %v: denied (%s), want not denied", a.tool.name, a.input, got.Reason)
		}
	}
}

func TestCheckerProtectsRelativePathFromWorkspace(t *testing.T) {
	t.Run("home workspace relative protected path denied", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		t.Chdir(home)
		c := NewChecker(PermissionConfig{Mode: ModeTrust})

		got := c.Check(protTool{name: "write_file"}, map[string]any{"path": ".celeste/hooks.json"})
		if got.Decision != Deny {
			t.Fatalf("decision %v, want Deny", got.Decision)
		}
	})

	t.Run("unrelated workspace relative hook path allowed", func(t *testing.T) {
		home := t.TempDir()
		workspace := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		t.Chdir(workspace)
		c := NewChecker(PermissionConfig{Mode: ModeTrust})

		got := c.Check(protTool{name: "write_file"}, map[string]any{"path": ".celeste/hooks.json"})
		if got.Decision == Deny {
			t.Fatalf("denied (%s), want not denied", got.Reason)
		}
	})

	t.Run("unrelated relative path in home workspace allowed", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		t.Chdir(home)
		c := NewChecker(PermissionConfig{Mode: ModeTrust})

		got := c.Check(protTool{name: "write_file"}, map[string]any{"path": "notes.md"})
		if got.Decision == Deny {
			t.Fatalf("denied (%s), want not denied", got.Reason)
		}
	})
}
