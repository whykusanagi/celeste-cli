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
	strike      = regexp.MustCompile(`~~[^~\n]+~~`) // markdown strikethrough
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
	content := h.Value
	if content == "" {
		content, _ = fieldText(h.Call.Input[h.Scope.Field])
	}
	content = fencedBlock.ReplaceAllString(content, "")
	content = quoteLine.ReplaceAllString(content, "")
	content = strike.ReplaceAllString(content, "")
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
