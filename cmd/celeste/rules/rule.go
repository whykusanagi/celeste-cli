// Package rules holds celeste's stream rules (2.0 W3, #175): a regex
// condition on the model's streamed text or a tool call's arguments, and a
// reminder that joins the conversation only when the rule fires. Rules come
// from the built-ins, ~/.celeste/rules/*.md and the grimoire's
// "## Stream Rules" section. A rule costs no context until it fires.
package rules

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ScopeKind is what a rule's condition is matched against.
type ScopeKind string

const (
	ScopeText     ScopeKind = "text"      // the model's streamed reply
	ScopeThinking ScopeKind = "thinking"  // no backend streams thinking at 2.0: parsed, never fires
	ScopeToolArgs ScopeKind = "tool_args" // one argument of one tool's calls
)

// Scope is one place a condition is matched: text, thinking, or
// tool_args:<tool>.<field>.
type Scope struct {
	Kind  ScopeKind
	Tool  string
	Field string
}

func (s Scope) String() string {
	if s.Kind == ScopeToolArgs {
		return fmt.Sprintf("tool_args:%s.%s", s.Tool, s.Field)
	}
	return string(s.Kind)
}

// Action is what a fired rule does.
type Action string

const (
	// Interrupt cuts the request short and re-runs the turn with the rule's
	// reminder; for tool_args, the turn's calls never run.
	Interrupt Action = "interrupt"
	// Append adds the reminder before the next request, after the turn's
	// tool results. After a final reply it waits for the next run.
	Append Action = "append"
	// Queue holds the reminder for the next run (the next user turn).
	Queue Action = "queue"
)

// Rule is one parsed rule.
type Rule struct {
	Name      string
	Condition *regexp.Regexp
	Scopes    []Scope
	Action    Action
	Once      bool // repeat: once (per session)
	Gap       int  // repeat: after-gap:N, in requests
	Message   string
	Source    string // file path, grimoire path, or "builtin"
	Disabled  bool   // enabled: false (removes a rule of the same name)
	// guard, for built-ins, vetoes a match the regex alone cannot judge
	// (an exempt path, a TTS call that did run).
	guard func(*Facts, Hit) bool
	// wholeValue, for built-ins whose condition matches any value (the
	// guard decides): Hit.Text is the whole argument value, not the one
	// character the condition matched.
	wholeValue bool
}

// Set is the rules in effect, sorted by name.
type Set struct{ Rules []*Rule }

// Len is the number of rules (0 for a nil Set).
func (s *Set) Len() int {
	if s == nil {
		return 0
	}
	return len(s.Rules)
}

// Parse reads one rule: YAML-style "key: value" frontmatter between "---"
// lines, then the reminder text. name is the default name (the file's base
// name). Keys: name, condition (RE2 regex, required unless enabled: false),
// scope (comma-separated; default text), action (default append), repeat
// (once | after-gap:N; default once), enabled (default true).
func Parse(name, source string, data []byte) (*Rule, error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	front, body, ok := splitFrontmatter(text)
	if !ok {
		return nil, fmt.Errorf("%s: no --- frontmatter", source)
	}
	r := &Rule{Name: name, Action: Append, Once: true, Source: source, Message: strings.TrimSpace(body)}
	scope := "text"
	var cond string
	for _, line := range strings.Split(front, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, found := strings.Cut(line, ":")
		if !found {
			return nil, fmt.Errorf("%s: bad frontmatter line %q", source, line)
		}
		key, val = strings.TrimSpace(key), unquote(strings.TrimSpace(val))
		switch key {
		case "name":
			r.Name = val
		case "condition":
			cond = val
		case "scope":
			scope = val
		case "action":
			r.Action = Action(val)
		case "repeat":
			if err := r.setRepeat(val); err != nil {
				return nil, fmt.Errorf("%s: %w", source, err)
			}
		case "enabled":
			b, err := strconv.ParseBool(val)
			if err != nil {
				return nil, fmt.Errorf("%s: enabled: %w", source, err)
			}
			r.Disabled = !b
		default:
			return nil, fmt.Errorf("%s: unknown key %q", source, key)
		}
	}
	if r.Name == "" {
		return nil, fmt.Errorf("%s: no name", source)
	}
	if r.Disabled {
		return r, nil
	}
	if cond == "" {
		return nil, fmt.Errorf("%s: no condition", source)
	}
	re, err := regexp.Compile(cond)
	if err != nil {
		return nil, fmt.Errorf("%s: condition: %w", source, err)
	}
	r.Condition = re
	switch r.Action {
	case Interrupt, Append, Queue:
	default:
		return nil, fmt.Errorf("%s: action %q (want interrupt, append or queue)", source, r.Action)
	}
	for _, s := range strings.Split(scope, ",") {
		sc, err := parseScope(strings.TrimSpace(s))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", source, err)
		}
		r.Scopes = append(r.Scopes, sc)
	}
	if r.Message == "" {
		return nil, fmt.Errorf("%s: no reminder text after the frontmatter", source)
	}
	return r, nil
}

func (r *Rule) setRepeat(v string) error {
	if v == "once" {
		r.Once, r.Gap = true, 0
		return nil
	}
	if n, ok := strings.CutPrefix(v, "after-gap:"); ok {
		gap, err := strconv.Atoi(n)
		if err != nil || gap < 1 {
			return fmt.Errorf("repeat %q: the gap must be a whole number of requests, 1 or more", v)
		}
		r.Once, r.Gap = false, gap
		return nil
	}
	return fmt.Errorf("repeat %q (want once or after-gap:N)", v)
}

func parseScope(s string) (Scope, error) {
	switch s {
	case "text":
		return Scope{Kind: ScopeText}, nil
	case "thinking":
		return Scope{Kind: ScopeThinking}, nil
	}
	if rest, ok := strings.CutPrefix(s, "tool_args:"); ok {
		tool, field, found := strings.Cut(rest, ".")
		if found && tool != "" && field != "" {
			return Scope{Kind: ScopeToolArgs, Tool: tool, Field: field}, nil
		}
	}
	return Scope{}, fmt.Errorf("scope %q (want text, thinking or tool_args:<tool>.<field>)", s)
}

func splitFrontmatter(text string) (front, body string, ok bool) {
	text = strings.TrimLeft(text, "\n")
	rest, found := strings.CutPrefix(text, "---\n")
	if !found {
		return "", "", false
	}
	front, body, found = strings.Cut(rest, "\n---")
	if !found {
		return "", "", false
	}
	body = strings.TrimPrefix(body, "\n")
	return front, body, true
}

// unquote strips one pair of matching quotes. No escapes are processed: a
// regex like "\bgit\b" keeps its backslashes.
func unquote(v string) string {
	if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
		return v[1 : len(v)-1]
	}
	return v
}
