package grimoire

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse_FullGrimoire(t *testing.T) {
	input := `# Grimoire: celeste-cli

## Bindings
- This is a Go project using Bubble Tea for TUI
- Module path: github.com/whykusanagi/celeste-cli

## Rituals
- Always run tests before committing
- Use conventional commit messages

## Incantations
@./docs/ARCHITECTURE.md
@./docs/STYLE_GUIDE.md

## Wards
- Do not modify cmd/celeste/prompts/celeste.go without permission

## Hooks

### PreToolUse
- bash: cd {{workspace}} && go vet ./... 2>&1 | head -5

### PostToolUse
- write_file: cd {{workspace}} && gofmt -w {{path}}
`

	g, err := Parse(input, "/repo")
	require.NoError(t, err)

	assert.Len(t, g.Bindings, 2)
	assert.Contains(t, g.Bindings[0], "Go project")
	assert.Len(t, g.Rituals, 2)
	assert.Len(t, g.Incantations, 2)
	assert.Equal(t, "@./docs/ARCHITECTURE.md", g.Incantations[0].Path)
	assert.Len(t, g.Wards, 1)
	assert.Len(t, g.Hooks, 2)
	assert.Equal(t, "PreToolUse", g.Hooks[0].Phase)
	assert.Equal(t, "bash", g.Hooks[0].ToolName)
	assert.Equal(t, "PostToolUse", g.Hooks[1].Phase)
}

func TestParse_EmptyInput(t *testing.T) {
	g, err := Parse("", "/repo")
	require.NoError(t, err)
	assert.True(t, g.IsEmpty())
}

func TestParse_BindingsOnly(t *testing.T) {
	input := `## Bindings
- Language: Go
- Framework: Bubble Tea
`
	g, err := Parse(input, "/repo")
	require.NoError(t, err)
	assert.Len(t, g.Bindings, 2)
	assert.Empty(t, g.Rituals)
}

func TestParse_UnknownSectionsIgnored(t *testing.T) {
	input := `## Bindings
- Language: Go

## CustomSection
- This should be preserved in RawSections
`
	g, err := Parse(input, "/repo")
	require.NoError(t, err)
	assert.Len(t, g.Bindings, 1)
	assert.Contains(t, g.RawSections, "CustomSection")
}

func TestParse_IncantationsWithHomePath(t *testing.T) {
	input := `## Incantations
@./local/file.md
@~/global/file.md
`
	g, err := Parse(input, "/repo")
	require.NoError(t, err)
	assert.Len(t, g.Incantations, 2)
	assert.Equal(t, "@./local/file.md", g.Incantations[0].Path)
	assert.Equal(t, "@~/global/file.md", g.Incantations[1].Path)
}

func TestParse_HooksMultipleTools(t *testing.T) {
	input := `## Hooks

### PreToolUse
- bash: echo "pre-bash"
- write_file: echo "pre-write"

### PostToolUse
- bash: echo "post-bash"
`
	g, err := Parse(input, "/repo")
	require.NoError(t, err)
	assert.Len(t, g.Hooks, 3)
	assert.Equal(t, "PreToolUse", g.Hooks[0].Phase)
	assert.Equal(t, "bash", g.Hooks[0].ToolName)
	assert.Equal(t, "PreToolUse", g.Hooks[1].Phase)
	assert.Equal(t, "write_file", g.Hooks[1].ToolName)
	assert.Equal(t, "PostToolUse", g.Hooks[2].Phase)
}

// "## Stream Rules" is for the rules package (2.0 W3): parsed apart and
// never rendered into the prompt.
func TestParse_StreamRulesNotRendered(t *testing.T) {
	g, err := Parse("## Rituals\n- test first\n\n## Stream Rules\n### no-todo\n---\ncondition: TODO\n---\nNo TODOs.\n", "")
	assert.NoError(t, err)
	assert.Len(t, g.StreamRules, 1)
	assert.Contains(t, g.StreamRules[0].Body, "### no-todo")
	assert.NotContains(t, g.Render(), "Stream Rules")
	assert.NotContains(t, g.Render(), "No TODOs.")
}
