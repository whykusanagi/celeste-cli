package permissions

import "testing"

// In default mode a read-only tool is auto-allowed only after the pattern
// rules: a matching pattern deny (or ask) still applies to it.
func TestReadOnlyAutoAllowYieldsToPatternRules(t *testing.T) {
	c := NewChecker(PermissionConfig{
		Mode: ModeDefault,
		PatternRules: []Rule{
			{ToolPattern: "read_file(*.env)", Decision: Deny},
			{ToolPattern: "read_file(*.key)", Decision: Ask},
		},
	})
	if got := c.Check(readOnlyTool("read_file"), map[string]any{"path": "a.env"}).Decision; got != Deny {
		t.Errorf("a.env: %v, want Deny", got)
	}
	if got := c.Check(readOnlyTool("read_file"), map[string]any{"path": "id.key"}).Decision; got != Ask {
		t.Errorf("id.key: %v, want Ask", got)
	}
	if got := c.Check(readOnlyTool("read_file"), map[string]any{"path": "main.go"}).Decision; got != Allow {
		t.Errorf("main.go: %v, want Allow", got)
	}
}
