// Package hooks loads, trusts and runs Celeste's lifecycle hooks (2.0 F0).
//
// Hooks come from ~/.celeste/hooks.json (global, trusted), .celeste/hooks.json
// files in the workspace and its ancestors (repo-local, untrusted until the
// user approves them), and grimoire "## Hooks" sections, which are converted
// on load to protocol v1.
package hooks

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/grimoire"
)

// Event names a point in a session where hooks run.
type Event string

const (
	EventPreToolUse       Event = "PreToolUse"
	EventPostToolUse      Event = "PostToolUse"
	EventSessionStart     Event = "SessionStart"
	EventUserPromptSubmit Event = "UserPromptSubmit"
	EventPreCompact       Event = "PreCompact"
	EventPostCompact      Event = "PostCompact"
	EventStop             Event = "Stop"
	EventSubagentStop     Event = "SubagentStop"
)

// Events lists every event.
var Events = []Event{
	EventPreToolUse, EventPostToolUse, EventSessionStart, EventUserPromptSubmit,
	EventPreCompact, EventPostCompact, EventStop, EventSubagentStop,
}

func (e Event) valid() bool {
	for _, x := range Events {
		if x == e {
			return true
		}
	}
	return false
}

// toolEvent reports whether e carries a tool name that a matcher filters on.
func (e Event) toolEvent() bool { return e == EventPreToolUse || e == EventPostToolUse }

// gating reports whether a failed hook blocks the action (fail closed).
func (e Event) gating() bool {
	return e == EventPreToolUse || e == EventUserPromptSubmit || e == EventPreCompact
}

// decides reports whether hook decisions affect control flow for e.
func (e Event) decides() bool {
	return e.gating() || e == EventStop || e == EventSubagentStop
}

// Decision is a hook's verdict.
type Decision string

const (
	Allow Decision = "allow"
	Deny  Decision = "deny"
	Ask   Decision = "ask"
)

const (
	// ProtocolV1 is a grimoire "## Hooks" entry: exit 0 allows, anything
	// else blocks with the output as the reason.
	ProtocolV1 = "v1"
	// ProtocolV2 reads one JSON object from stdout.
	ProtocolV2 = "v2"
	// DefaultTimeout and MaxTimeout are in seconds.
	DefaultTimeout = 30
	MaxTimeout     = 600
)

// Definition is one hook as written in hooks.json.
type Definition struct {
	Event    Event  `json:"event"`
	Matcher  string `json:"matcher,omitempty"`
	Command  string `json:"command"`
	Timeout  int    `json:"timeout,omitempty"`
	Protocol string `json:"protocol,omitempty"`
}

// normalize fills defaults and rejects definitions that can't run as written.
func (d Definition) normalize() (Definition, error) {
	if !d.Event.valid() {
		return d, fmt.Errorf("unknown event %q", d.Event)
	}
	if !utf8.ValidString(d.Command) {
		return d, errors.New("command contains invalid UTF-8")
	}
	if !utf8.ValidString(d.Matcher) {
		return d, errors.New("matcher contains invalid UTF-8")
	}
	d.Command = strings.TrimSpace(d.Command)
	if d.Command == "" {
		return d, errors.New("empty command")
	}
	if r := unsafeRune(d.Command); r >= 0 {
		return d, fmt.Errorf("command contains control or bidi character %U", r)
	}
	switch d.Protocol {
	case "":
		d.Protocol = ProtocolV2
	case ProtocolV1, ProtocolV2:
	default:
		return d, fmt.Errorf("unknown protocol %q (want %q or %q)", d.Protocol, ProtocolV1, ProtocolV2)
	}
	if d.Protocol == ProtocolV1 && !d.Event.toolEvent() {
		return d, fmt.Errorf("protocol v1 supports only PreToolUse and PostToolUse, not %s", d.Event)
	}
	d.Matcher = strings.TrimSpace(d.Matcher)
	if r := unsafeRune(d.Matcher); r >= 0 {
		return d, fmt.Errorf("matcher contains control or bidi character %U", r)
	}
	if d.Matcher == "" {
		d.Matcher = "*"
	}
	if !d.Event.toolEvent() && d.Matcher != "*" {
		return d, fmt.Errorf("matcher applies only to PreToolUse and PostToolUse, not %s", d.Event)
	}
	if d.Timeout == 0 {
		d.Timeout = DefaultTimeout
	}
	if d.Timeout < 0 || d.Timeout > MaxTimeout {
		return d, fmt.Errorf("timeout %d out of range 1..%d seconds", d.Timeout, MaxTimeout)
	}
	return d, nil
}

// unsafeRune returns the first rune that could hide or forge text on a
// terminal (C0/C1 controls, DEL, bidi embeddings, overrides and isolates),
// or -1. The approval prompt must show a person exactly what will run.
func unsafeRune(s string) rune {
	for _, r := range s {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) ||
			unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) {
			return r
		}
	}
	return -1
}

// safeText returns s, or s quoted when it holds a rune unsafeRune rejects
// or invalid UTF-8, for error text that may echo file content.
func safeText(s string) string {
	if unsafeRune(s) >= 0 || !utf8.ValidString(s) {
		return strconv.Quote(s)
	}
	return s
}

// matches reports whether d runs for event ev on tool.
func (d Definition) matches(ev Event, tool string) bool {
	if d.Event != ev {
		return false
	}
	return !ev.toolEvent() || d.Matcher == "*" || d.Matcher == tool
}

// ParseFile parses a hooks.json document. An unknown field or any invalid
// entry rejects the whole file, so a typo never silently drops a guard.
func ParseFile(data []byte) ([]Definition, error) {
	if !utf8.Valid(data) {
		return nil, errors.New("invalid hooks.json: invalid UTF-8")
	}
	var f struct {
		Hooks []Definition `json:"hooks"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("invalid hooks.json: %w", err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("invalid hooks.json: trailing data after the top-level object")
	}
	defs := make([]Definition, 0, len(f.Hooks))
	for i, d := range f.Hooks {
		n, err := d.normalize()
		if err != nil {
			return nil, fmt.Errorf("hooks[%d]: %w", i, err)
		}
		defs = append(defs, n)
	}
	return defs, nil
}

// FromGrimoire converts grimoire "## Hooks" entries to protocol v1
// definitions. Phases other than PreToolUse and PostToolUse never ran in
// 1.x; they are reported in skipped, not converted.
func FromGrimoire(entries []grimoire.HookEntry) (defs []Definition, skipped []string) {
	for _, e := range entries {
		d, err := Definition{Event: Event(e.Phase), Matcher: e.ToolName, Command: e.Command, Protocol: ProtocolV1}.normalize()
		if err != nil {
			skipped = append(skipped, fmt.Sprintf("%s %s: %v", strconv.Quote(e.Phase), strconv.Quote(e.ToolName), err))
			continue
		}
		defs = append(defs, d)
	}
	return defs, skipped
}

// Hash is the SHA-256 of the normalized definitions: exactly what a user
// approves. Formatting, key order and the rest of a grimoire don't change it.
func Hash(defs []Definition) string {
	b, _ := json.Marshal(defs) // strings and ints only; cannot fail
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
