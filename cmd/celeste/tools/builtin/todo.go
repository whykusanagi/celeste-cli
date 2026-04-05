package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// TodoItem represents a single task.
type TodoItem struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status"` // "pending", "in_progress", "done"
}

// TodoStore is a thread-safe file-backed store for todo items.
type TodoStore struct {
	tasks    []TodoItem
	nextID   int
	mu       sync.Mutex
	filePath string // empty = in-memory only
}

// NewTodoStore creates a TodoStore. If workspace is non-empty, persists to
// .celeste/tasks.json in the workspace directory.
func NewTodoStore(workspace string) *TodoStore {
	s := &TodoStore{nextID: 1}
	if workspace != "" {
		s.filePath = filepath.Join(workspace, ".celeste", "tasks.json")
		s.load()
	}
	return s
}

// load reads tasks from disk.
func (s *TodoStore) load() {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return
	}
	var state struct {
		Tasks  []TodoItem `json:"tasks"`
		NextID int        `json:"next_id"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return
	}
	s.tasks = state.Tasks
	s.nextID = state.NextID
	if s.nextID < 1 {
		s.nextID = 1
	}
}

// save writes tasks to disk.
func (s *TodoStore) save() {
	if s.filePath == "" {
		return
	}
	os.MkdirAll(filepath.Dir(s.filePath), 0755)
	state := struct {
		Tasks  []TodoItem `json:"tasks"`
		NextID int        `json:"next_id"`
	}{Tasks: s.tasks, NextID: s.nextID}
	data, _ := json.MarshalIndent(state, "", "  ")
	os.WriteFile(s.filePath, data, 0644)
}

// Create adds a new todo item and returns it.
func (s *TodoStore) Create(title, description string) TodoItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := TodoItem{
		ID:          s.nextID,
		Title:       title,
		Description: description,
		Status:      "pending",
	}
	s.nextID++
	s.tasks = append(s.tasks, item)
	s.save()
	return item
}

// Update changes the status of an item by ID.
func (s *TodoStore) Update(id int, status string) (TodoItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, item := range s.tasks {
		if item.ID == id {
			s.tasks[i].Status = status
			s.save()
			return s.tasks[i], nil
		}
	}
	return TodoItem{}, fmt.Errorf("todo item with id %d not found", id)
}

// Delete removes an item by ID.
func (s *TodoStore) Delete(id int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, item := range s.tasks {
		if item.ID == id {
			s.tasks = append(s.tasks[:i], s.tasks[i+1:]...)
			s.save()
			return nil
		}
	}
	return fmt.Errorf("todo item with id %d not found", id)
}

// List returns all items.
func (s *TodoStore) List() []TodoItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]TodoItem, len(s.tasks))
	copy(out, s.tasks)
	return out
}

// ClearDone removes all completed items.
func (s *TodoStore) ClearDone() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.tasks[:0]
	removed := 0
	for _, item := range s.tasks {
		if item.Status == "done" {
			removed++
		} else {
			kept = append(kept, item)
		}
	}
	s.tasks = kept
	s.save()
	return removed
}

// TodoTool is a built-in tool for managing task lists during a session.
type TodoTool struct {
	BaseTool
	store *TodoStore
}

// NewTodoTool creates a TodoTool with file-backed persistence in the workspace.
func NewTodoTool(workspace string) *TodoTool {
	return &TodoTool{
		BaseTool: BaseTool{
			ToolName: "todo",
			ToolDescription: "Manage a persistent task list for tracking work.\n\n" +
				"Tasks persist to .celeste/tasks.json so they survive session restarts and compaction.\n" +
				"Use this to break complex work into steps and track progress.\n\n" +
				"Actions:\n" +
				"- create: Add a new task (requires title)\n" +
				"- update: Change task status by ID (pending, in_progress, done)\n" +
				"- delete: Remove a task by ID\n" +
				"- list: Show all tasks\n" +
				"- clear_done: Remove all completed tasks",
			ToolParameters: mustJSON(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action": map[string]any{
						"type":        "string",
						"description": "Action: create, update, delete, list, clear_done",
						"enum":        []string{"create", "update", "delete", "list", "clear_done"},
					},
					"title": map[string]any{
						"type":        "string",
						"description": "Task title (required for create)",
					},
					"description": map[string]any{
						"type":        "string",
						"description": "Task description (optional for create)",
					},
					"id": map[string]any{
						"type":        "number",
						"description": "Task ID (required for update/delete)",
					},
					"status": map[string]any{
						"type":        "string",
						"description": "New status (required for update): pending, in_progress, done",
						"enum":        []string{"pending", "in_progress", "done"},
					},
				},
				"required": []string{"action"},
			}),
			ReadOnly:        false,
			ConcurrencySafe: true,
			Interrupt:       tools.InterruptCancel,
			RequiredFields:  []string{"action"},
		},
		store: NewTodoStore(workspace),
	}
}

// GetStore returns the underlying store (for TUI access).
func (t *TodoTool) GetStore() *TodoStore {
	return t.store
}

// Execute runs the todo action.
func (t *TodoTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	action := getStringArg(input, "action", "")

	switch action {
	case "create":
		title := getStringArg(input, "title", "")
		if title == "" {
			return tools.ToolResult{Error: true, Content: "title is required for create"}, nil
		}
		desc := getStringArg(input, "description", "")
		item := t.store.Create(title, desc)
		data, _ := json.Marshal(map[string]any{"created": item})
		return tools.ToolResult{Content: string(data)}, nil

	case "update":
		id := getIntArg(input, "id", 0)
		status := getStringArg(input, "status", "")
		if id == 0 {
			return tools.ToolResult{Error: true, Content: "id is required for update"}, nil
		}
		if status != "pending" && status != "in_progress" && status != "done" {
			return tools.ToolResult{Error: true, Content: "status must be: pending, in_progress, done"}, nil
		}
		item, err := t.store.Update(id, status)
		if err != nil {
			return tools.ToolResult{Error: true, Content: err.Error()}, nil
		}
		data, _ := json.Marshal(map[string]any{"updated": item})
		return tools.ToolResult{Content: string(data)}, nil

	case "delete":
		id := getIntArg(input, "id", 0)
		if id == 0 {
			return tools.ToolResult{Error: true, Content: "id is required for delete"}, nil
		}
		if err := t.store.Delete(id); err != nil {
			return tools.ToolResult{Error: true, Content: err.Error()}, nil
		}
		return tools.ToolResult{Content: fmt.Sprintf("Deleted task %d", id)}, nil

	case "list":
		items := t.store.List()
		data, _ := json.Marshal(map[string]any{"count": len(items), "tasks": items})
		return tools.ToolResult{Content: string(data)}, nil

	case "clear_done":
		removed := t.store.ClearDone()
		return tools.ToolResult{Content: fmt.Sprintf("Removed %d completed tasks", removed)}, nil

	default:
		return tools.ToolResult{Error: true, Content: "action must be: create, update, delete, list, clear_done"}, nil
	}
}
