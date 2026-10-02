package jev

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// pathToken finds absolute and home-relative paths: /a/b, ~/a, ~user/a,
// C:\a or C:/a, after the start of the text, a space, a quote, a bracket,
// '=', ',' or ':'. A URL's "//host" is never one (the first segment may not
// start with a slash), nor is a relative path (./a, a/b, and/or).
var pathToken = regexp.MustCompile("(^|[\\s\"'`=(\\[{<,:])((?:~[A-Za-z0-9._-]*|[A-Za-z]:)?[/\\\\][^/\\\\\\s\"'`<>|*?(){}\\[\\],;]+(?:[/\\\\]+[^/\\\\\\s\"'`<>|*?(){}\\[\\],;]*)*)")

// PathPlaceholder replaces a path outside the workspace.
const PathPlaceholder = "<path>"

// RedactPaths rewrites file paths before text leaves the machine (2.0 W3):
// a path inside workspace becomes workspace-relative ("." for the
// workspace itself); any other absolute or ~ path becomes <path>. An empty
// workspace makes every absolute path <path>.
func RedactPaths(s, workspace string) string {
	if !strings.ContainsAny(s, "/\\") {
		return s
	}
	ws := normPath(workspace)
	home := ""
	if h, err := os.UserHomeDir(); err == nil {
		home = normPath(h)
	}
	return pathToken.ReplaceAllStringFunc(s, func(m string) string {
		g := pathToken.FindStringSubmatch(m)
		lead, p := g[1], g[2]
		trimmed := strings.TrimRight(p, ".:")
		tail := p[len(trimmed):]
		return lead + relOrPlaceholder(trimmed, ws, home) + tail
	})
}

func relOrPlaceholder(p, ws, home string) string {
	full := p
	if strings.HasPrefix(p, "~") {
		if home == "" || !(strings.HasPrefix(p, "~/") || strings.HasPrefix(p, "~\\")) {
			return PathPlaceholder
		}
		full = home + "/" + p[2:]
	}
	full = normPath(full)
	if ws == "" || full == "" {
		return PathPlaceholder
	}
	if samePath(full, ws) {
		return "."
	}
	prefix := ws
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	if len(full) > len(prefix) && samePath(full[:len(prefix)], prefix) {
		return full[len(prefix):]
	}
	return PathPlaceholder
}

// normPath is p with forward slashes, cleaned ("" stays "").
func normPath(p string) string {
	if p == "" {
		return ""
	}
	return path.Clean(strings.ReplaceAll(filepath.ToSlash(p), "\\", "/"))
}

func samePath(a, b string) bool {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// RedactAll is Redact (secrets) and RedactPaths (file paths): what any
// text sent off the machine goes through.
func RedactAll(s, workspace string) string {
	return RedactPaths(Redact(s), workspace)
}

// RedactValue applies RedactAll to every string in v, a JSON-shaped value
// (a string, or anything that marshals to JSON). It returns v unchanged
// when v does not marshal (the request that would carry it fails anyway).
func RedactValue(v any, workspace string) any {
	switch x := v.(type) {
	case nil:
		return nil
	case string:
		return RedactAll(x, workspace)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var g any
	if json.Unmarshal(b, &g) != nil {
		return v
	}
	return redactWalk(g, workspace)
}

func redactWalk(v any, workspace string) any {
	switch x := v.(type) {
	case string:
		return RedactAll(x, workspace)
	case map[string]any:
		for k, e := range x {
			x[k] = redactWalk(e, workspace)
		}
	case []any:
		for i, e := range x {
			x[i] = redactWalk(e, workspace)
		}
	}
	return v
}
