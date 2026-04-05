package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/memories"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// SaveMemoryTool allows the LLM to persist learned facts about the project
// or user across conversations.
type SaveMemoryTool struct {
	BaseTool
	workspace string
}

// NewSaveMemoryTool creates a tool for saving memories to the project store.
func NewSaveMemoryTool(workspace string) *SaveMemoryTool {
	return &SaveMemoryTool{
		BaseTool: BaseTool{
			ToolName: "save_memory",
			ToolDescription: "Save a memory about this project or user for future conversations.\n\n" +
				"Memories persist across sessions and help you remember:\n" +
				"- Project patterns and architecture decisions\n" +
				"- User preferences and workflow habits\n" +
				"- Important context that isn't in the code\n" +
				"- Feedback the user gave about your approach\n\n" +
				"Types:\n" +
				"- project: Facts about the project (architecture, decisions, constraints)\n" +
				"- user: Information about the user (role, preferences, expertise)\n" +
				"- feedback: User corrections or confirmations about your approach\n" +
				"- reference: Pointers to external resources (URLs, docs, tools)\n\n" +
				"Use a descriptive name (slug-style) and keep content concise.",
			ToolParameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"name": {
						"type": "string",
						"description": "Short slug-style name for the memory (e.g., 'auth-uses-jwt', 'user-prefers-tdd')"
					},
					"type": {
						"type": "string",
						"enum": ["project", "user", "feedback", "reference"],
						"description": "Memory type: project, user, feedback, or reference"
					},
					"description": {
						"type": "string",
						"description": "One-line description of what this memory captures"
					},
					"content": {
						"type": "string",
						"description": "The memory content — what you learned"
					}
				},
				"required": ["name", "type", "content"]
			}`),
			ReadOnly:        false,
			ConcurrencySafe: true,
			RequiredFields:  []string{"name", "type", "content"},
		},
		workspace: workspace,
	}
}

func (t *SaveMemoryTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	if err := t.ValidateInput(input); err != nil {
		return tools.ToolResult{Error: true, Content: err.Error()}, nil
	}

	name := getStringArg(input, "name", "")
	memType := getStringArg(input, "type", "project")
	description := getStringArg(input, "description", "")
	content := getStringArg(input, "content", "")

	if !memories.IsValidType(memType) {
		return tools.ToolResult{
			Error:   true,
			Content: fmt.Sprintf("Invalid type '%s'. Use: project, user, feedback, reference", memType),
		}, nil
	}

	// Sanitize name to slug
	name = strings.ToLower(name)
	name = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		if r == ' ' {
			return '-'
		}
		return -1
	}, name)
	if name == "" {
		return tools.ToolResult{Error: true, Content: "Name is required"}, nil
	}

	if description == "" {
		// Auto-generate description from content
		description = content
		if len(description) > 80 {
			description = description[:77] + "..."
		}
	}

	store := memories.NewStore(t.workspace)
	mem := memories.NewMemory(name, description, memType, t.workspace, content)

	if err := store.Save(mem); err != nil {
		return tools.ToolResult{Error: true, Content: fmt.Sprintf("Failed to save memory: %v", err)}, nil
	}

	// Update memory index
	indexPath := filepath.Join(store.BaseDir(), "MEMORY.md")
	idx, _ := memories.LoadIndex(indexPath)
	if idx != nil {
		_ = idx.Add(memories.IndexEntry{
			Name:        name,
			File:        name + ".md",
			Description: description,
		})
		_ = idx.Save()
	}

	return tools.ToolResult{
		Content: fmt.Sprintf("Memory saved: %s [%s]\n%s", name, memType, description),
	}, nil
}
