package permissions

import "testing"

// primaryTool is a tool that names its primary argument.
type primaryTool struct {
	toolStub
	primary string
}

func (t *primaryTool) PrimaryArg() string { return t.primary }

func fileWriter() ToolInfo {
	return &primaryTool{toolStub: toolStub{name: "write_file"}, primary: "path"}
}

// An argument-scoped rule is matched against the tool's own primary
// argument: an extra input field the tool never reads cannot satisfy an
// allow rule or dodge a deny rule.
func TestArgumentRulesMatchThePrimaryArgument(t *testing.T) {
	c := NewChecker(PermissionConfig{
		Mode:        ModeDefault,
		AlwaysAllow: []Rule{{ToolPattern: "write_file(public/*)", Decision: Allow}},
		AlwaysDeny:  []Rule{{ToolPattern: "write_file(secrets/*)", Decision: Deny}},
	})

	if got := c.Check(fileWriter(), map[string]any{"path": "private/config", "content": "x", "command": "public/ok"}).Decision; got == Allow {
		t.Errorf("a decoy field satisfied an allow rule: %v", got)
	}
	if got := c.Check(fileWriter(), map[string]any{"path": "secrets/key", "content": "x", "command": "notes.txt"}).Decision; got != Deny {
		t.Errorf("a decoy field dodged a deny rule: %v", got)
	}
	if got := c.Check(fileWriter(), map[string]any{"path": "public/index.html", "content": "x"}).Decision; got != Allow {
		t.Errorf("the allow rule no longer matches its own path: %v", got)
	}
	// With the primary argument missing, a deny rule still applies and an
	// allow rule does not.
	if got := c.Check(fileWriter(), map[string]any{"command": "public/ok"}).Decision; got != Deny {
		t.Errorf("a call without its primary argument: %v, want Deny", got)
	}
}

// A tool that has no primary argument is never permitted by an
// argument-scoped allow rule, and is still restricted by a deny rule.
func TestArgumentRulesForAToolWithoutAPrimaryArgument(t *testing.T) {
	tool := &primaryTool{toolStub: toolStub{name: "mcp_x"}}
	allow := NewChecker(PermissionConfig{Mode: ModeDefault, AlwaysAllow: []Rule{{ToolPattern: "mcp_x(ok*)", Decision: Allow}}})
	if got := allow.Check(tool, map[string]any{"command": "ok", "a": "ok"}).Decision; got == Allow {
		t.Errorf("allow rule matched a tool without a primary argument")
	}
	deny := NewChecker(PermissionConfig{Mode: ModeTrust, AlwaysDeny: []Rule{{ToolPattern: "mcp_x(bad*)", Decision: Deny}}})
	if got := deny.Check(tool, map[string]any{"a": "fine"}).Decision; got != Deny {
		t.Errorf("deny rule skipped for a tool without a primary argument: %v", got)
	}
}
