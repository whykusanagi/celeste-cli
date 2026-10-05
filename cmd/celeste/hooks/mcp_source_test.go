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
	if PromptApprover(strings.NewReader("y\n"), &out)(src, Changed) != true {
		t.Fatal("y did not approve")
	}
	got := out.String()
	for _, want := range []string{`MCP server "odd#mcp:name"`, "have changed", `command: "sh"`, "Trust it?"} {
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
