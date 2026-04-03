package memories

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// IndexEntry represents a single entry in the MEMORY.md index.
type IndexEntry struct {
	Name        string
	File        string
	Description string
}

// Index manages the MEMORY.md file that summarizes all memories for system prompt injection.
type Index struct {
	path    string // e.g. ~/.celeste/projects/<hash>/memories/MEMORY.md
	entries []IndexEntry
}

// LoadIndex loads or creates an Index from a MEMORY.md file path.
func LoadIndex(path string) (*Index, error) {
	idx := &Index{path: path}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return idx, nil // empty index
		}
		return nil, err
	}

	idx.entries = parseIndex(string(data))
	return idx, nil
}

// Add adds an entry to the index. If an entry with the same name exists, it is replaced.
func (idx *Index) Add(entry IndexEntry) error {
	for i, e := range idx.entries {
		if e.Name == entry.Name {
			idx.entries[i] = entry
			return nil
		}
	}
	idx.entries = append(idx.entries, entry)
	return nil
}

// Remove removes an entry by name.
func (idx *Index) Remove(name string) error {
	for i, e := range idx.entries {
		if e.Name == name {
			idx.entries = append(idx.entries[:i], idx.entries[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("entry '%s' not found in index", name)
}

// Entries returns a copy of the index entries.
func (idx *Index) Entries() []IndexEntry {
	out := make([]IndexEntry, len(idx.entries))
	copy(out, idx.entries)
	return out
}

// Render renders the index as markdown for system prompt injection.
// Caps output at 200 lines.
func (idx *Index) Render() string {
	if len(idx.entries) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("# Memories\n\n")

	lineCount := 2
	for _, e := range idx.entries {
		line := fmt.Sprintf("- **%s** (`%s`): %s\n", e.Name, e.File, e.Description)
		lineCount++
		if lineCount > 200 {
			sb.WriteString("- *(truncated, too many memories)*\n")
			break
		}
		sb.WriteString(line)
	}
	return sb.String()
}

// Save writes the index to disk as MEMORY.md.
func (idx *Index) Save() error {
	if err := os.MkdirAll(strings.TrimSuffix(idx.path, "/MEMORY.md"), 0755); err != nil {
		return err
	}

	var sb strings.Builder
	sb.WriteString("# Memory Index\n\n")
	sb.WriteString("<!-- Auto-generated. Do not edit manually. -->\n\n")

	for _, e := range idx.entries {
		sb.WriteString(fmt.Sprintf("- **%s** | `%s` | %s\n", e.Name, e.File, e.Description))
	}

	return os.WriteFile(idx.path, []byte(sb.String()), 0644)
}

// parseIndex parses a MEMORY.md file into entries.
// Expected format: - **Name** | `file` | Description
func parseIndex(content string) []IndexEntry {
	var entries []IndexEntry
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "- **") {
			continue
		}

		// Parse: - **Name** | `file` | Description
		line = strings.TrimPrefix(line, "- **")
		parts := strings.SplitN(line, "**", 2)
		if len(parts) < 2 {
			continue
		}
		name := parts[0]
		rest := strings.TrimPrefix(parts[1], " | ")

		fields := strings.SplitN(rest, " | ", 2)
		if len(fields) < 2 {
			continue
		}
		file := strings.Trim(fields[0], "`")
		desc := strings.TrimSpace(fields[1])

		entries = append(entries, IndexEntry{
			Name:        name,
			File:        file,
			Description: desc,
		})
	}
	return entries
}
