package acp

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

// promptText turns a prompt's content blocks into the user message
// (ruling 5): text blocks joined with blank lines, an embedded text
// resource as <file path="…">, a resource link as [file: <uri>]. Images,
// audio and binary resources (not advertised) are dropped with a log line.
// A prompt with nothing left is an error.
func promptText(blocks []ContentBlock, logf func(format string, args ...any)) (string, error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	var parts []string
	for _, b := range blocks {
		switch b.Type {
		case BlockText:
			if strings.TrimSpace(b.Text) != "" {
				parts = append(parts, b.Text)
			}
		case BlockResource:
			if b.Resource == nil || b.Resource.Text == "" {
				uri := ""
				if b.Resource != nil {
					uri = b.Resource.URI
				}
				logf("acp: dropping a resource without text (%s)", uri)
				continue
			}
			parts = append(parts, fmt.Sprintf("<file path=%q>\n%s\n</file>", uriPath(b.Resource.URI), strings.TrimSuffix(b.Resource.Text, "\n")))
		case BlockResourceLink:
			if b.URI == "" {
				logf("acp: dropping a resource link without a uri")
				continue
			}
			parts = append(parts, "[file: "+b.URI+"]")
		default:
			logf("acp: dropping a %s block (not supported)", b.Type)
		}
	}
	if len(parts) == 0 {
		return "", errors.New("the prompt has no text celeste can use")
	}
	return strings.Join(parts, "\n\n"), nil
}

// uriPath is the local, percent-decoded path of a file:// URI, or the URI
// unchanged (any other scheme, or one that does not parse).
func uriPath(uri string) string {
	if !strings.HasPrefix(uri, "file://") {
		return uri
	}
	u, err := url.Parse(uri)
	if err != nil || u.Path == "" {
		return uri
	}
	p := u.Path
	// file:///C:/x on Windows: drop the slash before the drive letter.
	if len(p) >= 3 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}
	return p
}

// toolKinds maps celeste's tools to ACP tool kinds (ruling 7); any other
// tool is "other".
var toolKinds = map[string]string{
	"read_file": ToolKindRead, "list_files": ToolKindRead, "git_status": ToolKindRead,
	"git_log": ToolKindRead, "load_skill": ToolKindRead,
	"search": ToolKindSearch, "find_tools": ToolKindSearch,
	"write_file": ToolKindEdit, "patch_file": ToolKindEdit, "splice_file": ToolKindEdit,
	"edit_lines": ToolKindEdit,
	"bash":       ToolKindExecute,
	"web_fetch":  ToolKindFetch, "web_search": ToolKindFetch,
}

// toolKind is the ACP kind of a celeste tool (ruling 7).
func toolKind(name string) string {
	if k, ok := toolKinds[name]; ok {
		return k
	}
	switch {
	case strings.HasPrefix(name, "recall_"):
		return ToolKindRead
	case strings.HasPrefix(name, "code_"):
		return ToolKindSearch
	}
	return ToolKindOther
}

// titleArgs are the arguments a title shows, in order of preference.
var titleArgs = []string{"path", "file_path", "command", "query", "pattern", "url", "symbol", "name", "action"}

// maxTitleRunes bounds the argument part of a title.
const maxTitleRunes = 80

// toolTitle is "<tool>: <main argument>" on one line, the argument cut at
// 80 runes; just the tool's name when it has none of titleArgs.
func toolTitle(name string, input map[string]any) string {
	for _, k := range titleArgs {
		v, ok := input[k].(string)
		if !ok {
			continue
		}
		v, _, _ = strings.Cut(strings.TrimSpace(v), "\n")
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if r := []rune(v); len(r) > maxTitleRunes {
			v = string(r[:maxTitleRunes])
		}
		return name + ": " + v
	}
	return name
}

// summaryTitle is a title from a permission request's input summary, used
// when the call's own title is not known.
// Only the summary's first line is used (it lists every argument, one per
// line), cut at a carriage return too.
func summaryTitle(name, summary string) string {
	first, _, _ := strings.Cut(summary, "\n")
	first, _, _ = strings.Cut(first, "\r")
	return toolTitle(name, map[string]any{"path": first})
}

// toolLocations is the file a call touches, when its input has a path:
// absolute, relative paths resolved against the workspace (ACP locations
// are absolute).
func toolLocations(input map[string]any, workspace string) []Location {
	p, _ := input["path"].(string)
	if p == "" {
		p, _ = input["file_path"].(string)
	}
	if strings.TrimSpace(p) == "" {
		return nil
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(workspace, p)
	}
	return []Location{{Path: filepath.Clean(p)}}
}
