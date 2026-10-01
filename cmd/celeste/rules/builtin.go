package rules

import (
	"embed"
	"path"
	"regexp"
	"sort"
	"strings"
)

//go:embed builtin/*.md
var builtinFS embed.FS

// guards are the built-ins' Go-side checks, by rule name.
var guards = map[string]func(*Facts, Hit) bool{
	"persona-voice-in-files":      voiceInFileGuard,
	"unbacked-audio-claim":        func(f *Facts, _ Hit) bool { return !f.TTSRan },
	"task-complete-before-verify": func(f *Facts, _ Hit) bool { return f.Unverified() && !f.RuntimeVerifies },
	"destructive-bash":            func(_ *Facts, h Hit) bool { return destructiveBash(h) },
}

// Builtins returns the shipped rules (spec §5 W3), parsed fresh each call.
// A user or grimoire rule of the same name replaces one; enabled: false
// turns it off.
func Builtins() []*Rule {
	entries, _ := builtinFS.ReadDir("builtin")
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	out := make([]*Rule, 0, len(names))
	for _, n := range names {
		data, _ := builtinFS.ReadFile("builtin/" + n)
		r, err := Parse(strings.TrimSuffix(n, ".md"), "builtin", data)
		if err != nil {
			panic("rules: built-in " + n + ": " + err.Error()) // caught by TestBuiltinsParse
		}
		r.guard = guards[r.Name]
		out = append(out, r)
	}
	return out
}

var (
	fencedBlock = regexp.MustCompile("(?s)```.*?(```|$)|(?s)~~~.*?(~~~|$)")
	quoteLine   = regexp.MustCompile(`(?m)^[ \t]*>.*$`)
)

// voiceInFileGuard fires only on voice outside fenced code and quotation
// lines, in a file outside docs and persona paths: quoting her voice in
// documentation, or persona sources, are not flagged (spec §9, Celeste).
func voiceInFileGuard(_ *Facts, h Hit) bool {
	if h.Call == nil {
		return false
	}
	p, _ := h.Call.Input["path"].(string)
	if exemptPath(p) {
		return false
	}
	content, _ := fieldText(h.Call.Input[h.Scope.Field])
	content = fencedBlock.ReplaceAllString(content, "")
	content = quoteLine.ReplaceAllString(content, "")
	if tildeIsSyntax(p) {
		// A trailing ~ is a backup pattern, a home directory or a LaTeX
		// tie there, never a sung word.
		content = strings.ReplaceAll(content, "~", "")
	}
	return h.Rule.Condition.MatchString(content)
}

// exemptPath: any directory named doc or docs, or any path segment that
// mentions persona.
func exemptPath(p string) bool {
	p = strings.ToLower(strings.ReplaceAll(p, "\\", "/"))
	segs := strings.Split(path.Clean(p), "/")
	for i, s := range segs {
		if strings.Contains(s, "persona") {
			return true
		}
		if i < len(segs)-1 && (s == "doc" || s == "docs") {
			return true
		}
	}
	return false
}

// tildeIsSyntax: dotfiles (.gitignore), shell scripts and TeX use ~ as
// syntax.
func tildeIsSyntax(p string) bool {
	base := strings.ToLower(path.Base(strings.ReplaceAll(p, "\\", "/")))
	if strings.HasPrefix(base, ".") {
		return true
	}
	switch path.Ext(base) {
	case ".sh", ".bash", ".zsh", ".fish", ".tex", ".sty", ".cls", ".bib":
		return true
	}
	return false
}

var (
	gitPushForce = regexp.MustCompile(`(?i)\bgit\s+push\b[^\n;&|]*\s(--force|-f)(\s|$)`)
	shellSplit   = regexp.MustCompile(`&&|\|\||[;|\n]`)
	// buildDirs are build output a project regenerates: removing one inside
	// the workspace is routine, not destructive.
	buildDirs = map[string]bool{"build": true, "dist": true, "node_modules": true, "target": true, ".cache": true, "out": true, "coverage": true}
)

// destructiveBash fires on a force push, or on an rm that is both
// recursive and forced (-rf, -r -f, --recursive --force) unless every path
// it removes is a build directory (or inside one) under the workspace.
func destructiveBash(h Hit) bool {
	if h.Call == nil {
		return true
	}
	cmd, _ := h.Call.Input["command"].(string)
	for _, seg := range shellSplit.Split(cmd, -1) {
		if gitPushForce.MatchString(seg) {
			return true
		}
		fields := strings.Fields(seg)
		for i, f := range fields {
			if f == "rm" && rmRecursiveForce(fields[i+1:]) {
				return true
			}
		}
	}
	return false
}

func rmRecursiveForce(args []string) bool {
	recursive, force := false, false
	var targets []string
	for _, a := range args {
		switch {
		case a == "--recursive":
			recursive = true
		case a == "--force":
			force = true
		case strings.HasPrefix(a, "--"):
		case strings.HasPrefix(a, "-") && len(a) > 1:
			recursive = recursive || strings.ContainsAny(a, "rR")
			force = force || strings.Contains(a, "f")
		default:
			targets = append(targets, a)
		}
	}
	if !recursive || !force {
		return false
	}
	if len(targets) == 0 {
		return true
	}
	for _, t := range targets {
		if !buildDirTarget(t) {
			return true
		}
	}
	return false
}

// buildDirTarget: a relative path inside the workspace whose first element
// is a build directory.
func buildDirTarget(t string) bool {
	t = strings.Trim(t, `"'`)
	if t == "" || strings.ContainsAny(t, "$~*?`\\") || strings.HasPrefix(t, "/") {
		return false
	}
	clean := path.Clean(t)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return false
	}
	first, _, _ := strings.Cut(clean, "/")
	return buildDirs[first]
}
