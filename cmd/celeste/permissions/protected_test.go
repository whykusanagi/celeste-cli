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

func TestCheckerDeniesBashCommandNamingHookFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	c := NewChecker(PermissionConfig{Mode: ModeTrust})
	bash := protTool{name: "bash"}
	denied := []string{
		"echo '{}' > $HOME/.celeste/trusted.json",
		"cp x ${HOME}/.celeste/hooks.json",
		"cat ~/.celeste/grimoire.md",
		"cp x " + filepath.Join(home, ".celeste", "hooks.json"),
		"echo x > some/where/.celeste/trusted.json",
	}
	for _, cmd := range denied {
		if got := c.Check(bash, map[string]any{"command": cmd}); got.Decision != Deny {
			t.Errorf("%q: decision %v, want Deny", cmd, got.Decision)
		}
	}
	allowed := []string{
		"ls ~/.celeste",
		"echo hello world",
		"cat ~/.celeste/hooks.json.bak",
	}
	for _, cmd := range allowed {
		if got := c.Check(bash, map[string]any{"command": cmd}); got.Decision == Deny {
			t.Errorf("%q: denied (%s), want not denied", cmd, got.Reason)
		}
	}
}

func TestCheckerDeniesBashCommandWithNonFilenameSuffix(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	c := NewChecker(PermissionConfig{Mode: ModeTrust})
	bash := protTool{name: "bash"}
	denied := []string{
		"cat ~/.celeste/hooks.json*",
		"cat ~/.celeste/hooks.json$u",
		"echo ~/.celeste/hooks.json{,}",
		"echo ~/.celeste/hooks.json,",
	}
	for _, cmd := range denied {
		if got := c.Check(bash, map[string]any{"command": cmd}); got.Decision != Deny {
			t.Errorf("%q: decision %v, want Deny", cmd, got.Decision)
		}
	}
	allowed := []string{
		"cat ~/.celeste/hooks.json.bak",
		"cat ~/.celeste/hooks.json-old",
	}
	for _, cmd := range allowed {
		if got := c.Check(bash, map[string]any{"command": cmd}); got.Decision == Deny {
			t.Errorf("%q: denied (%s), want not denied", cmd, got.Reason)
		}
	}
}

// A non-bash tool's arguments are no longer substring-matched at all: this
// closed a false positive where a doc's *content* merely mentioning one of
// these paths was denied. Real protection for file tools' `path` arguments
// now lives in tools/builtin's resolvePath (see its own tests).
func TestCheckerDoesNotSubstringMatchNonBashTools(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	c := NewChecker(PermissionConfig{Mode: ModeTrust})
	write := protTool{name: "write_file"}

	cases := []map[string]any{
		{"path": filepath.Join(home, ".celeste", "hooks.json"), "content": "{}"},
		{"path": "~/.celeste/grimoire.md"},
		{"path": "docs/HOOKS.md", "content": "See ~/.celeste/hooks.json for details."},
		{"path": ".celeste/hooks.json"},
	}
	for _, input := range cases {
		if got := c.Check(write, input); got.Decision == Deny {
			t.Errorf("%v: denied (%s), want not denied by the checker (resolvePath is the real gate now)", input, got.Reason)
		}
	}
}
