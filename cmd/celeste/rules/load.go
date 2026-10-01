package rules

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Section is one grimoire file's "## Stream Rules" body.
type Section struct {
	Source string // the grimoire file
	Body   string // "### <name>" subsections, each a rule in the file format
}

// Load returns the built-ins, overridden by ~/.celeste/rules/*.md, then by
// the grimoire's "## Stream Rules" sections in order. A later rule
// replaces an earlier one of the same name,
// and enabled: false removes it. A file that does not parse is skipped with
// a warning; so is a rule whose only scope is thinking (no backend streams
// thinking at 2.0).
func Load(home string, grimoire []Section, warn func(string)) *Set {
	if warn == nil {
		warn = func(string) {}
	}
	byName := map[string]*Rule{}
	add := func(r *Rule) {
		if r.Disabled {
			delete(byName, r.Name)
			return
		}
		if onlyThinking(r) {
			warn(fmt.Sprintf("stream rule %s (%s): scope thinking never fires in this build; skipped", r.Name, r.Source))
			return
		}
		byName[r.Name] = r
	}
	for _, r := range Builtins() {
		add(r)
	}
	if home != "" {
		paths, _ := filepath.Glob(filepath.Join(home, ".celeste", "rules", "*.md"))
		sort.Strings(paths)
		for _, p := range paths {
			data, err := os.ReadFile(p)
			if err != nil {
				warn(fmt.Sprintf("stream rule %s: %v", p, err))
				continue
			}
			r, err := Parse(strings.TrimSuffix(filepath.Base(p), ".md"), p, data)
			if err != nil {
				warn("stream rule skipped: " + err.Error())
				continue
			}
			add(r)
		}
	}
	for _, g := range grimoire {
		for _, sec := range grimoireSections(g.Body) {
			r, err := Parse(sec.name, g.Source+"#"+sec.name, []byte(sec.body))
			if err != nil {
				warn("stream rule skipped: " + err.Error())
				continue
			}
			add(r)
		}
	}
	set := &Set{}
	for _, r := range byName {
		set.Rules = append(set.Rules, r)
	}
	sort.Slice(set.Rules, func(i, j int) bool { return set.Rules[i].Name < set.Rules[j].Name })
	return set
}

func onlyThinking(r *Rule) bool {
	for _, s := range r.Scopes {
		if s.Kind != ScopeThinking {
			return false
		}
	}
	return len(r.Scopes) > 0
}

type section struct{ name, body string }

// grimoireSections splits the "## Stream Rules" body on "### <name>"
// headings; each subsection is a rule in the file format.
func grimoireSections(body string) []section {
	var out []section
	var cur *section
	var b strings.Builder
	flush := func() {
		if cur != nil {
			cur.body = b.String()
			out = append(out, *cur)
		}
		b.Reset()
	}
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		if name, ok := strings.CutPrefix(line, "### "); ok {
			flush()
			cur = &section{name: strings.TrimSpace(name)}
			continue
		}
		if cur != nil {
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	flush()
	return out
}
