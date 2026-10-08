package permissions

import (
	"os"
	"path/filepath"
	"testing"
)

func fileReader() ToolInfo {
	return &primaryTool{toolStub: toolStub{name: "read_file", readOnly: true}, primary: "path"}
}

// docsExampleConfig is the example permissions.json in docs/PERMISSIONS.md.
const docsExampleConfig = `{
  "mode": "default",
  "always_allow": [
    {"tool_pattern": "read_file", "decision": "allow"},
    {"tool_pattern": "bash(git status*)", "decision": "allow"}
  ],
  "always_deny": [
    {"tool_pattern": "bash(sudo *)", "decision": "deny"},
    {"tool_pattern": "write_file(secrets/*)", "decision": "deny"}
  ],
  "pattern_rules": [
    {"tool_pattern": "read_file(*.env)", "decision": "deny"},
    {"tool_pattern": "write_file(docs/*)", "decision": "ask"}
  ]
}`

func loadDocsExample(t *testing.T) *Checker {
	t.Helper()
	p := filepath.Join(t.TempDir(), "permissions.json")
	if err := os.WriteFile(p, []byte(docsExampleConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	return NewChecker(*cfg)
}

// An argument-scoped pattern deny or ask is more specific than a bare-name
// always_allow, so it wins: the docs' example config denies read_file on
// a.env although always_allow names read_file (as every saved file does:
// the defaults are written with the first saved rule).
func TestScopedPatternRuleOverridesBareAlwaysAllow(t *testing.T) {
	c := loadDocsExample(t)
	if got := c.Check(fileReader(), map[string]any{"path": "a.env"}).Decision; got != Deny {
		t.Errorf("read_file a.env: %v, want Deny", got)
	}
	if got := c.Check(fileReader(), map[string]any{"path": "main.go"}).Decision; got != Allow {
		t.Errorf("read_file main.go: %v, want Allow", got)
	}
	if got := c.Check(fileWriter(), map[string]any{"path": "docs/x.md", "content": "x"}).Decision; got != Ask {
		t.Errorf("write_file docs/x.md: %v, want Ask", got)
	}
	if got := c.Check(fileWriter(), map[string]any{"path": "secrets/k", "content": "x"}).Decision; got != Deny {
		t.Errorf("write_file secrets/k: %v, want Deny", got)
	}

	// The same with the defaults saved into the file by a first "Always
	// allow" (persistRule loads DefaultConfig when there is no file).
	cfg := DefaultConfig()
	cfg.PatternRules = []Rule{{ToolPattern: "read_file(*.env)", Decision: Deny}}
	if got := NewChecker(cfg).Check(fileReader(), map[string]any{"path": "a.env"}).Decision; got != Deny {
		t.Errorf("defaults + read_file(*.env) deny: %v, want Deny", got)
	}
}

// A directory-scoped rule matches the path however it is spelled inside
// the workspace: absolute, or through a symlinked directory.
func TestPathRulesMatchAbsoluteAndSymlinkedPaths(t *testing.T) {
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, "secrets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(ws, "public"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(ws, "secrets"), filepath.Join(ws, "alias")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(ws, "secrets"), filepath.Join(ws, "public", "link")); err != nil {
		t.Fatal(err)
	}
	c := NewChecker(PermissionConfig{
		Mode:        ModeDefault,
		AlwaysAllow: []Rule{{ToolPattern: "write_file(public/*)", Decision: Allow}},
		AlwaysDeny:  []Rule{{ToolPattern: "write_file(secrets/*)", Decision: Deny}},
	})
	c.SetWorkspace(ws)
	realWS, err := filepath.EvalSymlinks(ws)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{
		filepath.Join(ws, "secrets", "k"),
		filepath.Join(realWS, "secrets", "k"),
		filepath.Join(ws, "public", "..", "secrets", "k"),
		"alias/k",
		filepath.Join(ws, "alias", "k"),
		"public/link/k",
	} {
		if got := c.Check(fileWriter(), map[string]any{"path": p, "content": "x"}).Decision; got != Deny {
			t.Errorf("%s: %v, want Deny by write_file(secrets/*)", p, got)
		}
	}
	for _, p := range []string{"public/a.css", filepath.Join(ws, "public", "a.css")} {
		if got := c.Check(fileWriter(), map[string]any{"path": p, "content": "x"}).Decision; got != Allow {
			t.Errorf("%s: %v, want Allow by write_file(public/*)", p, got)
		}
	}
	// Without the deny rule, an allow still never covers a path whose real
	// location is outside the rule's directory.
	c2 := NewChecker(PermissionConfig{
		Mode:        ModeDefault,
		AlwaysAllow: []Rule{{ToolPattern: "write_file(public/*)", Decision: Allow}},
	})
	c2.SetWorkspace(ws)
	if got := c2.Check(fileWriter(), map[string]any{"path": "public/link/k", "content": "x"}).Decision; got == Allow {
		t.Errorf("public/link/k (really secrets/k) allowed by write_file(public/*)")
	}
}

// A deny rule on a command also matches it behind shell keywords, wrappers,
// a leading backslash or variable assignments.
func TestCommandDenyMatchesBehindWrappers(t *testing.T) {
	c := NewChecker(PermissionConfig{
		Mode:       ModeDefault,
		AlwaysDeny: []Rule{{ToolPattern: "bash(rm *)", Decision: Deny}, {ToolPattern: "bash(sudo *)", Decision: Deny}},
	})
	for _, cmd := range []string{
		"{ rm x; }", "if true; then rm x; fi", "while true; do rm x; done", "false || ! rm x",
		"command rm x", `\rm x`, "x=1 rm y", "A=1 B=2 rm y", "env rm x", "env -i X=1 rm x",
		"exec rm x", "builtin rm x", "true; else rm x", "nohup rm x", "time rm x", `'rm' x`, `"rm" x`,
		"x=1 sudo y", "command sudo y", `\sudo y`,
	} {
		if got := c.Check(bashTool(), map[string]any{"command": cmd}).Decision; got != Deny {
			t.Errorf("%q: %v, want Deny", cmd, got)
		}
	}
	if got := c.Check(bashTool(), map[string]any{"command": "echo rm x"}).Decision; got == Deny {
		t.Errorf("echo rm x: denied")
	}
}

// A rule for an absolute directory outside the workspace keeps working
// with the workspace known.
func TestAbsoluteRulesOutsideTheWorkspace(t *testing.T) {
	ws, out := t.TempDir(), t.TempDir()
	c := NewChecker(PermissionConfig{
		Mode:        ModeDefault,
		AlwaysAllow: []Rule{{ToolPattern: "write_file(" + filepath.ToSlash(out) + "/*)", Decision: Allow}},
	})
	c.SetWorkspace(ws)
	if got := c.Check(fileWriter(), map[string]any{"path": filepath.Join(out, "a"), "content": "x"}).Decision; got != Allow {
		t.Errorf("absolute allow outside the workspace: %v, want Allow", got)
	}
}
