package permissions

import "testing"

func bashTool() ToolInfo {
	return &primaryTool{toolStub: toolStub{name: "bash"}, primary: "command"}
}

// A directory-scoped rule is matched against the cleaned path: ".." can
// neither walk an allowed path out of its directory nor walk a denied one
// past its rule.
func TestPathRulesMatchTheCleanedPath(t *testing.T) {
	c := NewChecker(PermissionConfig{
		Mode:        ModeDefault,
		AlwaysAllow: []Rule{{ToolPattern: "write_file(public/*)", Decision: Allow}},
		AlwaysDeny:  []Rule{{ToolPattern: "write_file(secrets/*)", Decision: Deny}},
	})
	for _, p := range []string{"public/../private-config", "public/../../outside", "public/a/../../x"} {
		if got := c.Check(fileWriter(), map[string]any{"path": p, "content": "x"}).Decision; got == Allow {
			t.Errorf("%s: allowed by write_file(public/*)", p)
		}
	}
	if got := c.Check(fileWriter(), map[string]any{"path": "public/../secrets/key", "content": "x"}).Decision; got != Deny {
		t.Errorf("public/../secrets/key: %v, want Deny", got)
	}
	if got := c.Check(fileWriter(), map[string]any{"path": "public/./css/site.css", "content": "x"}).Decision; got != Allow {
		t.Errorf("public/./css/site.css: %v, want Allow", got)
	}
}

// A command rule covers one command: an allow rule never matches a
// command line that chains or substitutes another, and a deny rule
// matches a denied command anywhere in the line.
func TestCommandRulesDoNotSpanShellSeparators(t *testing.T) {
	c := NewChecker(PermissionConfig{
		Mode:        ModeDefault,
		AlwaysAllow: []Rule{{ToolPattern: "bash(git *)", Decision: Allow}},
		AlwaysDeny:  []Rule{{ToolPattern: "bash(sudo *)", Decision: Deny}},
	})
	for _, cmd := range []string{
		"git status; rm -rf x", "git status && rm x", "git log || rm x", "git log | sh",
		"git log `rm x`", "git log $(rm x)", "git status\nrm x", "git log > out", "git log & rm x",
	} {
		if got := c.Check(bashTool(), map[string]any{"command": cmd}).Decision; got == Allow {
			t.Errorf("%q: allowed by bash(git *)", cmd)
		}
	}
	if got := c.Check(bashTool(), map[string]any{"command": "git status --short"}).Decision; got != Allow {
		t.Errorf("git status --short: %v, want Allow", got)
	}
	for _, cmd := range []string{"echo hi; sudo rm x", "true && sudo x", "echo $(sudo x)"} {
		if got := c.Check(bashTool(), map[string]any{"command": cmd}).Decision; got != Deny {
			t.Errorf("%q: %v, want Deny by bash(sudo *)", cmd, got)
		}
	}
}
