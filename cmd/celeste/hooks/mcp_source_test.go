package hooks

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestMCPSourceKeyAndPrompt(t *testing.T) {
	file := filepath.Join("repo", ".celeste", "mcp.json")
	src := MCPSource(file, "odd#mcp:name", `command: "sh"`, "h1")
	if src.Kind != KindRepoMCP || src.Root != "repo" || src.Global() {
		t.Fatalf("src = %+v", src)
	}
	if SourceFile(src) != file || MCPServerName(src) != "odd#mcp:name" {
		t.Fatalf("file %q name %q", SourceFile(src), MCPServerName(src))
	}
	if other := MCPSource(file, "other", "", "h1"); other.Path == src.Path {
		t.Fatal("two servers share a trust key")
	}

	var out bytes.Buffer
	if PromptApprover(strings.NewReader("y\n"), &out)(src, Changed) != AnswerYes {
		t.Fatal("y did not approve")
	}
	got := out.String()
	for _, want := range []string{`MCP server "odd#mcp:name"`, "has changed since you approved it:", `command: "sh"`, "Trust it?"} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt lacks %q:\n%s", want, got)
		}
	}
	out.Reset()
	DescribeSource(&out, MCPSource(file, "x", "bad\x1bline", ""))
	if strings.Contains(out.String(), "\x1b") {
		t.Fatalf("a control character reached the terminal: %q", out.String())
	}
}

// C4: the prompt for one MCP server speaks of it in the singular.
func TestMCPPromptIsSingular(t *testing.T) {
	src := MCPSource(filepath.Join("repo", ".mcp.json"), "repo-stub", `command: "sh"`, "h1")
	var out bytes.Buffer
	PromptApprover(strings.NewReader("n\n"), &out)(src, Untrusted)
	got := out.String()
	if !strings.Contains(got, `" is not trusted yet:`) || strings.Contains(got, " are not trusted") {
		t.Errorf("untrusted prompt not singular:\n%s", got)
	}
	out.Reset()
	PromptApprover(strings.NewReader("n\n"), &out)(src, Changed)
	if got := out.String(); !strings.Contains(got, " has changed since you approved it:") || strings.Contains(got, "approved them") {
		t.Errorf("changed prompt not singular:\n%s", got)
	}
}
