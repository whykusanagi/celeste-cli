// Package grimoire provides .grimoire project context file handling.
package grimoire

import (
	"fmt"
	"strings"
)

// Grimoire represents a fully resolved project context.
type Grimoire struct {
	Sources      []string          // file paths that contributed (ordered by priority)
	Bindings     []string          // project facts (language, structure, commands)
	Rituals      []string          // behavioral rules (always/never do)
	Incantations []IncludeRef      // @path includes with resolved content
	Wards        []string          // protected areas
	Hooks        []HookEntry       // pre/post tool execution commands
	RawSections  map[string]string // unparsed section content by heading
}

// IncludeRef represents an @include directive with its resolved content.
type IncludeRef struct {
	Path     string // original @path from grimoire
	Resolved string // absolute path after resolution
	Content  string // file content (empty if unresolved)
	Error    string // non-empty if resolution failed
}

// HookEntry represents a pre/post tool execution hook.
type HookEntry struct {
	Phase    string // "PreToolUse" or "PostToolUse"
	ToolName string // tool name pattern (e.g., "bash", "write_file")
	Command  string // shell command to execute
}

// Render produces the grimoire content for injection into a system prompt.
func (g *Grimoire) Render() string {
	if g.IsEmpty() {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("# Project Grimoire\n\n")

	if len(g.Bindings) > 0 {
		sb.WriteString("## Bindings\n")
		for _, b := range g.Bindings {
			sb.WriteString(fmt.Sprintf("- %s\n", b))
		}
		sb.WriteString("\n")
	}

	if len(g.Rituals) > 0 {
		sb.WriteString("## Rituals\n")
		for _, r := range g.Rituals {
			sb.WriteString(fmt.Sprintf("- %s\n", r))
		}
		sb.WriteString("\n")
	}

	if len(g.Incantations) > 0 {
		sb.WriteString("## Incantations\n")
		for _, inc := range g.Incantations {
			if inc.Error != "" {
				sb.WriteString(fmt.Sprintf("<!-- @%s: %s -->\n", inc.Path, inc.Error))
			} else if inc.Content != "" {
				sb.WriteString(fmt.Sprintf("### %s\n%s\n\n", inc.Path, inc.Content))
			}
		}
	}

	if len(g.Wards) > 0 {
		sb.WriteString("## Wards\n")
		for _, w := range g.Wards {
			sb.WriteString(fmt.Sprintf("- %s\n", w))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// TotalSize returns the total byte size of the rendered grimoire.
func (g *Grimoire) TotalSize() int {
	return len(g.Render())
}

// IsEmpty returns true if the grimoire has no content.
func (g *Grimoire) IsEmpty() bool {
	return len(g.Bindings) == 0 && len(g.Rituals) == 0 &&
		len(g.Incantations) == 0 && len(g.Wards) == 0 && len(g.Hooks) == 0
}

// MaxSize is the maximum total grimoire context size in bytes.
const MaxSize = 25 * 1024 // 25KB

// Merge combines multiple grimoires into one, preserving order.
// Later grimoires take precedence for RawSections keys.
func Merge(grimoires ...*Grimoire) *Grimoire {
	merged := &Grimoire{
		RawSections: make(map[string]string),
	}
	for _, g := range grimoires {
		if g == nil {
			continue
		}
		merged.Sources = append(merged.Sources, g.Sources...)
		merged.Bindings = append(merged.Bindings, g.Bindings...)
		merged.Rituals = append(merged.Rituals, g.Rituals...)
		merged.Incantations = append(merged.Incantations, g.Incantations...)
		merged.Wards = append(merged.Wards, g.Wards...)
		merged.Hooks = append(merged.Hooks, g.Hooks...)
		for k, v := range g.RawSections {
			merged.RawSections[k] = v
		}
	}
	return merged
}

