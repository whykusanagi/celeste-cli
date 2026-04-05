// Package grimoire provides .grimoire project context file handling.
package grimoire

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// GrimoireMetadata holds the embedded metadata from the grimoire file.
type GrimoireMetadata struct {
	LastUpdated    time.Time
	GitHash        string
	GitBranch      string
	GitCommitCount int
}

// Grimoire represents a fully resolved project context.
type Grimoire struct {
	Sources      []string          // file paths that contributed (ordered by priority)
	Bindings     []string          // project facts (language, structure, commands)
	Rituals      []string          // behavioral rules (always/never do)
	Incantations []IncludeRef      // @path includes with resolved content
	Wards        []string          // protected areas
	Hooks        []HookEntry       // pre/post tool execution commands
	RawSections  map[string]string // unparsed section content by heading
	Meta         GrimoireMetadata  // embedded metadata (last updated, git hash, etc.)
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

// StalenessInfo returns how stale the grimoire is relative to current git state.
// Returns a human-readable message, or "" if metadata is unavailable.
func (g *Grimoire) StalenessInfo(currentDir string) string {
	if g.Meta.GitHash == "" {
		return ""
	}

	// Check current commit count
	currentCount := gitCommand(currentDir, "rev-list", "--count", "HEAD")
	if currentCount == "" {
		return ""
	}
	current, err := strconv.Atoi(currentCount)
	if err != nil {
		return ""
	}

	behind := current - g.Meta.GitCommitCount
	if behind <= 0 {
		return ""
	}

	age := ""
	if !g.Meta.LastUpdated.IsZero() {
		age = fmt.Sprintf(" (%s ago)", timeSince(g.Meta.LastUpdated))
	}

	if behind > 50 {
		return fmt.Sprintf("WARNING: Grimoire is %d commits behind HEAD%s — consider updating", behind, age)
	} else if behind > 10 {
		return fmt.Sprintf("Note: Grimoire is %d commits behind HEAD%s", behind, age)
	}
	return ""
}

func timeSince(t time.Time) string {
	d := time.Since(t)
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// Render produces the grimoire content for injection into a system prompt.
func (g *Grimoire) Render() string {
	if g.IsEmpty() {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("# Project Grimoire\n\n")

	// Show metadata if available
	if g.Meta.GitHash != "" {
		sb.WriteString(fmt.Sprintf("_Last updated: %s | git: %s (%s)_\n\n",
			g.Meta.LastUpdated.Format("2006-01-02 15:04"),
			g.Meta.GitHash,
			g.Meta.GitBranch))
	}

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

	// Render any custom sections (Architecture, Structure, Issues, etc.)
	// These are sections she writes that aren't part of the standard schema.
	if len(g.RawSections) > 0 {
		// Sort for deterministic output
		keys := make([]string, 0, len(g.RawSections))
		for k := range g.RawSections {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, name := range keys {
			body := g.RawSections[name]
			if body != "" {
				sb.WriteString(fmt.Sprintf("## %s\n%s\n\n", name, body))
			}
		}
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
		len(g.Incantations) == 0 && len(g.Wards) == 0 && len(g.Hooks) == 0 &&
		len(g.RawSections) == 0
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
