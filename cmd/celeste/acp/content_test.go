package acp

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPromptTextJoinsTextAndResources(t *testing.T) {
	var logged []string
	logf := func(format string, args ...any) { logged = append(logged, format) }
	got, err := promptText([]ContentBlock{
		TextBlock("explain this"),
		{Type: BlockResource, Resource: &ResourceContents{URI: "file:///w/a.go", Text: "package a\n"}},
		{Type: BlockResourceLink, URI: "file:///w/b.go", Name: "b.go"},
		{Type: BlockImage, Data: "aGk=", MimeType: "image/png"},
		{Type: BlockResource, Resource: &ResourceContents{URI: "file:///w/c.bin", Blob: "AAEC"}},
		TextBlock("thanks"),
	}, logf)
	if err != nil {
		t.Fatal(err)
	}
	want := "explain this\n\n<file path=\"/w/a.go\">\npackage a\n</file>\n\n[file: file:///w/b.go]\n\nthanks"
	if got != want {
		t.Fatalf("promptText =\n%q\nwant\n%q", got, want)
	}
	if len(logged) != 2 {
		t.Fatalf("the image and the binary resource must each be logged as dropped: %v", logged)
	}
}

func TestPromptTextKeepsNonFileURIs(t *testing.T) {
	got, err := promptText([]ContentBlock{{Type: BlockResource, Resource: &ResourceContents{URI: "zed://selection", Text: "x"}}}, nil)
	if err != nil || got != "<file path=\"zed://selection\">\nx\n</file>" {
		t.Fatalf("promptText = %q, %v", got, err)
	}
}

func TestPromptTextEmptyIsAnError(t *testing.T) {
	for _, blocks := range [][]ContentBlock{
		nil,
		{TextBlock("  \n")},
		{{Type: BlockImage, Data: "aGk=", MimeType: "image/png"}},
	} {
		if _, err := promptText(blocks, nil); err == nil {
			t.Fatalf("promptText(%+v) = nil error, want one", blocks)
		}
	}
}

func TestToolKinds(t *testing.T) {
	for name, want := range map[string]string{
		"read_file": ToolKindRead, "list_files": ToolKindRead, "git_status": ToolKindRead, "git_log": ToolKindRead,
		"recall_tool_result": ToolKindRead, "load_skill": ToolKindRead,
		"search": ToolKindSearch, "code_search": ToolKindSearch, "code_graph": ToolKindSearch, "find_tools": ToolKindSearch,
		"write_file": ToolKindEdit, "patch_file": ToolKindEdit, "splice_file": ToolKindEdit, "edit_lines": ToolKindEdit,
		"bash":      ToolKindExecute,
		"web_fetch": ToolKindFetch, "web_search": ToolKindFetch,
		"todo": ToolKindOther, "mcp__stub__echo": ToolKindOther, "": ToolKindOther,
	} {
		if got := toolKind(name); got != want {
			t.Errorf("toolKind(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestToolTitle(t *testing.T) {
	if got := toolTitle("read_file", map[string]any{"path": "a.go"}); got != "read_file: a.go" {
		t.Fatalf("read_file title = %q", got)
	}
	if got := toolTitle("bash", map[string]any{"command": "go test ./..."}); got != "bash: go test ./..." {
		t.Fatalf("bash title = %q", got)
	}
	long := toolTitle("bash", map[string]any{"command": strings.Repeat("é", 100) + "\nsecond line"})
	if n := len([]rune(strings.TrimPrefix(long, "bash: "))); n != 80 || strings.Contains(long, "\n") {
		t.Fatalf("a long command must be cut to 80 runes on one line, got %d: %q", n, long)
	}
	if got := toolTitle("bash", map[string]any{"command": "echo a\necho b"}); got != "bash: echo a" {
		t.Fatalf("multi-line command title = %q", got)
	}
	if got := toolTitle("list_files", map[string]any{}); got != "list_files" {
		t.Fatalf("no-argument title = %q", got)
	}
	if got := toolTitle("web_fetch", map[string]any{"url": "https://example.com"}); got != "web_fetch: https://example.com" {
		t.Fatalf("url title = %q", got)
	}
}

func TestToolLocations(t *testing.T) {
	ws, other := t.TempDir(), t.TempDir()
	if got := toolLocations(map[string]any{"path": "a.go"}, ws); len(got) != 1 || got[0].Path != filepath.Join(ws, "a.go") {
		t.Fatalf("relative path location = %+v", got)
	}
	abs := filepath.Join(other, "b.go")
	if got := toolLocations(map[string]any{"path": abs}, ws); len(got) != 1 || got[0].Path != abs {
		t.Fatalf("absolute path location = %+v", got)
	}
	if got := toolLocations(map[string]any{"command": "ls"}, ws); got != nil {
		t.Fatalf("no path, no locations: %+v", got)
	}
}

func TestURIPathDecodes(t *testing.T) {
	for uri, want := range map[string]string{
		"file:///Users/me/My%20Project/a.go": "/Users/me/My Project/a.go",
		"file:///C:/w/a%23b.go":              "C:/w/a#b.go",
		"file:///w/a.go":                     "/w/a.go",
		"untitled:Untitled-1":                "untitled:Untitled-1",
		"file:///w/bad%zz.go":                "file:///w/bad%zz.go",
	} {
		if got := uriPath(uri); got != want {
			t.Errorf("uriPath(%q) = %q, want %q", uri, got, want)
		}
	}
}

// A permission request's fallback title is the summary's first line only:
// the summary lists every argument, one per line, each up to 4 KiB.
func TestSummaryTitleIsFirstLine(t *testing.T) {
	summary := "go test ./...\ntimeout: 600\nworkdir: " + strings.Repeat("x", 4096)
	got := summaryTitle("bash", summary)
	if got != "bash: go test ./..." {
		t.Fatalf("summaryTitle = %q", got)
	}
	if got := summaryTitle("bash", "a\rb\nc"); strings.ContainsAny(got, "\r\n") {
		t.Fatalf("summaryTitle kept a line break: %q", got)
	}
}
