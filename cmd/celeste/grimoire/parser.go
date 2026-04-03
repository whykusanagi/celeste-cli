package grimoire

import (
	"strings"
)

// Parse parses a .grimoire markdown file into a Grimoire struct.
// It splits on ## headings and maps content to known section types.
func Parse(content string, baseDir string) (*Grimoire, error) {
	g := &Grimoire{
		RawSections: make(map[string]string),
	}

	if strings.TrimSpace(content) == "" {
		return g, nil
	}

	// Split into sections by ## headings
	sections := splitSections(content)

	for name, body := range sections {
		switch name {
		case "Bindings":
			g.Bindings = parseListItems(body)
		case "Rituals":
			g.Rituals = parseListItems(body)
		case "Wards":
			g.Wards = parseListItems(body)
		case "Incantations":
			g.Incantations = parseIncantations(body)
		case "Hooks":
			g.Hooks = parseHooks(body)
		default:
			g.RawSections[name] = body
		}
	}

	return g, nil
}

// splitSections splits markdown content by ## headings.
// Returns a map of section name -> section body (content after the heading).
func splitSections(content string) map[string]string {
	sections := make(map[string]string)
	lines := strings.Split(content, "\n")

	var currentSection string
	var currentBody strings.Builder

	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			// Save previous section
			if currentSection != "" {
				sections[currentSection] = strings.TrimSpace(currentBody.String())
			}
			currentSection = strings.TrimSpace(strings.TrimPrefix(line, "## "))
			currentBody.Reset()
		} else if currentSection != "" {
			currentBody.WriteString(line)
			currentBody.WriteString("\n")
		}
		// Lines before the first ## heading are ignored (title, etc.)
	}

	// Save last section
	if currentSection != "" {
		sections[currentSection] = strings.TrimSpace(currentBody.String())
	}

	return sections
}

// parseListItems extracts "- item" entries from a section body.
func parseListItems(body string) []string {
	var items []string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "- ") {
			item := strings.TrimPrefix(line, "- ")
			if item != "" {
				items = append(items, item)
			}
		}
	}
	return items
}

// parseIncantations extracts @./path and @~/path references.
func parseIncantations(body string) []IncludeRef {
	var refs []IncludeRef
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "@./") || strings.HasPrefix(line, "@~/") {
			refs = append(refs, IncludeRef{Path: line})
		}
	}
	return refs
}

// parseHooks parses hook entries from ### PreToolUse / ### PostToolUse sub-sections.
func parseHooks(body string) []HookEntry {
	var hooks []HookEntry
	var currentPhase string

	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "### ") {
			currentPhase = strings.TrimSpace(strings.TrimPrefix(trimmed, "### "))
			continue
		}
		if currentPhase != "" && strings.HasPrefix(trimmed, "- ") {
			entry := strings.TrimPrefix(trimmed, "- ")
			colonIdx := strings.Index(entry, ":")
			if colonIdx > 0 {
				toolName := strings.TrimSpace(entry[:colonIdx])
				command := strings.TrimSpace(entry[colonIdx+1:])
				hooks = append(hooks, HookEntry{
					Phase:    currentPhase,
					ToolName: toolName,
					Command:  command,
				})
			}
		}
	}

	return hooks
}
