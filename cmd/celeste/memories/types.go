// Package memories implements a persistent memory system for project context.
// Memories are stored as markdown files with YAML frontmatter under
// ~/.celeste/projects/<hash>/memories/.
package memories

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Memory represents a single memory entry with frontmatter metadata and content.
type Memory struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Type        string `yaml:"type"` // user, feedback, project, reference
	Created     string `yaml:"created"`
	Project     string `yaml:"project"`
	Content     string `yaml:"-"` // everything after frontmatter
}

// ValidTypes lists the allowed memory types.
var ValidTypes = []string{"user", "feedback", "project", "reference"}

// IsValidType checks if a type string is a valid memory type.
func IsValidType(t string) bool {
	for _, vt := range ValidTypes {
		if t == vt {
			return true
		}
	}
	return false
}

// ParseMemory parses a markdown file with YAML frontmatter into a Memory.
// Frontmatter is delimited by "---" lines.
func ParseMemory(data []byte) (*Memory, error) {
	str := string(data)

	// Must start with "---"
	if !strings.HasPrefix(str, "---") {
		return nil, fmt.Errorf("missing frontmatter delimiter")
	}

	// Find end of frontmatter.
	rest := str[3:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return nil, fmt.Errorf("missing closing frontmatter delimiter")
	}

	frontmatter := strings.TrimSpace(rest[:idx])
	content := strings.TrimSpace(rest[idx+4:]) // skip "\n---"

	var m Memory
	if err := yaml.Unmarshal([]byte(frontmatter), &m); err != nil {
		return nil, fmt.Errorf("failed to parse frontmatter: %w", err)
	}
	m.Content = content
	return &m, nil
}

// Serialize renders the memory back to markdown with YAML frontmatter.
func (m *Memory) Serialize() []byte {
	var buf bytes.Buffer

	buf.WriteString("---\n")
	fm, _ := yaml.Marshal(m)
	buf.Write(fm)
	buf.WriteString("---\n")
	if m.Content != "" {
		buf.WriteString("\n")
		buf.WriteString(m.Content)
		buf.WriteString("\n")
	}
	return buf.Bytes()
}

// NewMemory creates a Memory with the current timestamp.
func NewMemory(name, description, memType, project, content string) *Memory {
	return &Memory{
		Name:        name,
		Description: description,
		Type:        memType,
		Created:     time.Now().Format(time.RFC3339),
		Project:     project,
		Content:     content,
	}
}
