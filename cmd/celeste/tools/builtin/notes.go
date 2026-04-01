package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// Note represents a note entry.
type Note struct {
	Title   string    `json:"title"`
	Content string    `json:"content"`
	Created time.Time `json:"created"`
	Updated time.Time `json:"updated"`
}

// getNotesPath returns the path to notes.json.
func getNotesPath() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".celeste", "notes.json")
}

// NoteSaveTool saves a note.
type NoteSaveTool struct {
	BaseTool
}

// NewNoteSaveTool creates a NoteSaveTool.
func NewNoteSaveTool() *NoteSaveTool {
	return &NoteSaveTool{
		BaseTool: BaseTool{
			ToolName:        "save_note",
			ToolDescription: "Save a note with an optional title",
			ToolParameters: mustJSON(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"title": map[string]interface{}{
						"type":        "string",
						"description": "Optional note title",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "Note content",
					},
				},
				"required": []string{"content"},
			}),
			ReadOnly:        false,
			ConcurrencySafe: false,
			Interrupt:       tools.InterruptCancel,
			RequiredFields:  []string{"content"},
		},
	}
}

func (t *NoteSaveTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	content, ok := input["content"].(string)
	if !ok || content == "" {
		return resultFromMap(formatErrorResponse(
			"validation_error",
			"The 'content' parameter is required",
			"Please provide the note content you want to save.",
			map[string]interface{}{
				"skill": "save_note",
				"field": "content",
			},
		))
	}

	title := ""
	if t, ok := input["title"].(string); ok {
		title = t
	} else {
		lines := strings.Split(content, "\n")
		title = strings.TrimSpace(lines[0])
		if len(title) > 50 {
			title = title[:50] + "..."
		}
	}

	notesPath := getNotesPath()
	var notes map[string]Note
	if data, err := os.ReadFile(notesPath); err == nil {
		_ = json.Unmarshal(data, &notes)
	} else {
		notes = make(map[string]Note)
	}

	now := time.Now()
	if existing, exists := notes[title]; exists {
		existing.Content = content
		existing.Updated = now
		notes[title] = existing
	} else {
		notes[title] = Note{
			Title:   title,
			Content: content,
			Created: now,
			Updated: now,
		}
	}

	os.MkdirAll(filepath.Dir(notesPath), 0755)
	data, err := json.MarshalIndent(notes, "", "  ")
	if err != nil {
		return resultFromMap(formatErrorResponse(
			"internal_error",
			"Failed to save note",
			"An internal error occurred while saving the note. Please try again.",
			map[string]interface{}{
				"skill": "save_note",
				"error": err.Error(),
			},
		))
	}
	if err := os.WriteFile(notesPath, data, 0644); err != nil {
		return resultFromMap(formatErrorResponse(
			"internal_error",
			"Failed to save note file",
			"An internal error occurred while saving the note. Please try again.",
			map[string]interface{}{
				"skill": "save_note",
				"error": err.Error(),
			},
		))
	}

	return resultFromMap(map[string]interface{}{
		"title":   title,
		"success": true,
	})
}

// NoteGetTool retrieves a note by title.
type NoteGetTool struct {
	BaseTool
}

// NewNoteGetTool creates a NoteGetTool.
func NewNoteGetTool() *NoteGetTool {
	return &NoteGetTool{
		BaseTool: BaseTool{
			ToolName:        "get_note",
			ToolDescription: "Retrieve a note by title",
			ToolParameters: mustJSON(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"title": map[string]interface{}{
						"type":        "string",
						"description": "Note title to retrieve",
					},
				},
				"required": []string{"title"},
			}),
			ReadOnly:        true,
			ConcurrencySafe: true,
			Interrupt:       tools.InterruptCancel,
			RequiredFields:  []string{"title"},
		},
	}
}

func (t *NoteGetTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	title, ok := input["title"].(string)
	if !ok || title == "" {
		return resultFromMap(formatErrorResponse(
			"validation_error",
			"The 'title' parameter is required",
			"Please provide the title of the note you want to retrieve.",
			map[string]interface{}{
				"skill": "get_note",
				"field": "title",
			},
		))
	}

	notesPath := getNotesPath()
	var notes map[string]Note
	if data, err := os.ReadFile(notesPath); err != nil {
		return resultFromMap(formatErrorResponse(
			"not_found",
			fmt.Sprintf("Note '%s' not found", title),
			"The note file does not exist or the note with this title was not found.",
			map[string]interface{}{
				"skill": "get_note",
				"title": title,
			},
		))
	} else {
		if err := json.Unmarshal(data, &notes); err != nil {
			return resultFromMap(formatErrorResponse(
				"internal_error",
				"Failed to parse notes file",
				"The notes file may be corrupted. Please try again.",
				map[string]interface{}{
					"skill": "get_note",
					"error": err.Error(),
				},
			))
		}
	}

	note, exists := notes[title]
	if !exists {
		return resultFromMap(formatErrorResponse(
			"not_found",
			fmt.Sprintf("Note '%s' not found", title),
			"No note exists with this title. Use 'list_notes' to see available notes.",
			map[string]interface{}{
				"skill": "get_note",
				"title": title,
			},
		))
	}

	return resultFromMap(map[string]interface{}{
		"title":   note.Title,
		"content": note.Content,
		"created": note.Created.Format(time.RFC3339),
		"updated": note.Updated.Format(time.RFC3339),
	})
}

// NoteListTool lists all saved notes.
type NoteListTool struct {
	BaseTool
}

// NewNoteListTool creates a NoteListTool.
func NewNoteListTool() *NoteListTool {
	return &NoteListTool{
		BaseTool: BaseTool{
			ToolName:        "list_notes",
			ToolDescription: "List all saved notes",
			ToolParameters: mustJSON(map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
				"required":   []string{},
			}),
			ReadOnly:        true,
			ConcurrencySafe: true,
			Interrupt:       tools.InterruptCancel,
		},
	}
}

func (t *NoteListTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	notesPath := getNotesPath()
	var notes map[string]Note
	if data, err := os.ReadFile(notesPath); err == nil {
		_ = json.Unmarshal(data, &notes)
	} else {
		notes = make(map[string]Note)
	}

	noteList := make([]map[string]interface{}, 0, len(notes))
	for _, note := range notes {
		noteList = append(noteList, map[string]interface{}{
			"title":   note.Title,
			"created": note.Created.Format(time.RFC3339),
			"updated": note.Updated.Format(time.RFC3339),
		})
	}

	return resultFromMap(map[string]interface{}{
		"count": len(noteList),
		"notes": noteList,
	})
}
