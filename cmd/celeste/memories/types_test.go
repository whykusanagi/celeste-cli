package memories

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseMemory(t *testing.T) {
	data := []byte(`---
name: test-memory
description: A test memory
type: feedback
created: "2026-03-15T10:00:00Z"
project: myproject
---

This is the content of the memory.
It can span multiple lines.
`)
	m, err := ParseMemory(data)
	require.NoError(t, err)
	assert.Equal(t, "test-memory", m.Name)
	assert.Equal(t, "A test memory", m.Description)
	assert.Equal(t, "feedback", m.Type)
	assert.Equal(t, "myproject", m.Project)
	assert.Contains(t, m.Content, "This is the content")
	assert.Contains(t, m.Content, "multiple lines")
}

func TestParseMemoryMissingFrontmatter(t *testing.T) {
	_, err := ParseMemory([]byte("no frontmatter here"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing frontmatter")
}

func TestParseMemoryMissingClosingDelimiter(t *testing.T) {
	_, err := ParseMemory([]byte("---\nname: test\n"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "closing frontmatter")
}

func TestSerializeRoundTrip(t *testing.T) {
	original := &Memory{
		Name:        "roundtrip",
		Description: "Test roundtrip",
		Type:        "project",
		Created:     "2026-03-15T10:00:00Z",
		Project:     "celeste",
		Content:     "Some important content.",
	}

	data := original.Serialize()
	parsed, err := ParseMemory(data)
	require.NoError(t, err)
	assert.Equal(t, original.Name, parsed.Name)
	assert.Equal(t, original.Description, parsed.Description)
	assert.Equal(t, original.Type, parsed.Type)
	assert.Equal(t, original.Content, parsed.Content)
}

func TestSerializeEmptyContent(t *testing.T) {
	m := &Memory{
		Name: "empty",
		Type: "user",
	}
	data := m.Serialize()
	assert.Contains(t, string(data), "---")
	assert.Contains(t, string(data), "name: empty")
}

func TestIsValidType(t *testing.T) {
	assert.True(t, IsValidType("user"))
	assert.True(t, IsValidType("feedback"))
	assert.True(t, IsValidType("project"))
	assert.True(t, IsValidType("reference"))
	assert.False(t, IsValidType("invalid"))
	assert.False(t, IsValidType(""))
}

func TestNewMemory(t *testing.T) {
	m := NewMemory("test", "desc", "user", "proj", "content")
	assert.Equal(t, "test", m.Name)
	assert.Equal(t, "desc", m.Description)
	assert.Equal(t, "user", m.Type)
	assert.Equal(t, "proj", m.Project)
	assert.Equal(t, "content", m.Content)
	assert.NotEmpty(t, m.Created)
}
