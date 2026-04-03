package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// TodoItem represents a single task.
type TodoItem struct {
	ID     int    `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"` // "pending", "in_progress", "done"
}

// TodoStore is a thread-safe in-memory store for todo items.
type TodoStore struct {
	tasks  []TodoItem
	nextID int
	mu     sync.Mutex
}

// NewTodoStore creates an empty TodoStore.
func NewTodoStore() *TodoStore {
	return &TodoStore{nextID: 1}
}

// Create adds a new todo item and returns it.
func (s *TodoStore) Create(title string) TodoItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := TodoItem{
		ID:     s.nextID,
		Title:  title,
		Status: "pending",
	}
	s.nextID++
	s.tasks = append(s.tasks, item)
	return item
}

// Update changes the status of an item by ID.
func (s *TodoStore) Update(id int, status string) (TodoItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, item := range s.tasks {
		if item.ID == id {
			s.tasks[i].Status = status
			return s.tasks[i], nil
		}
	}
	return TodoItem{}, fmt.Errorf("todo item with id %d not found", id)
}

// List returns all items.
func (s *TodoStore) List() []TodoItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]TodoItem, len(s.tasks))
	copy(out, s.tasks)
	return out
}

// TodoTool is a built-in tool for managing task lists during a session.
type TodoTool struct {
	BaseTool
	store *TodoStore
}

// NewTodoTool creates a TodoTool with an empty in-memory store.
func NewTodoTool() *TodoTool {
	return &TodoTool{
		BaseTool: BaseTool{
			ToolName:        "todo",
			ToolDescription: "Manage a task list: create, update, or list tasks",
			ToolParameters: mustJSON(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action": map[string]any{
						"type":        "string",
						"description": "Action to perform: create, update, or list",
						"enum":        []string{"create", "update", "list"},
					},
					"title": map[string]any{
						"type":        "string",
						"description": "Task title (required for create)",
					},
					"id": map[string]any{
						"type":        "number",
						"description": "Task ID (required for update)",
					},
					"status": map[string]any{
						"type":        "string",
						"description": "New status (required for update): pending, in_progress, or done",
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
		store: NewTodoStore(),
	}
}

// Execute runs the todo action.
func (t *TodoTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	action := getStringArg(input, "action", "")

	switch action {
	case "create":
		title := getStringArg(input, "title", "")
		if title == "" {
			return resultFromMap(formatErrorResponse(
				"validation_error",
				"The 'title' parameter is required for create",
				"Please provide a title for the task.",
				map[string]any{"skill": "todo", "action": "create"},
			))
		}
		item := t.store.Create(title)
		return resultFromMap(map[string]any{
			"success": true,
			"item":    item,
		})

	case "update":
		id := getIntArg(input, "id", 0)
		if id == 0 {
			return resultFromMap(formatErrorResponse(
				"validation_error",
				"The 'id' parameter is required for update",
				"Please provide the task ID to update.",
				map[string]any{"skill": "todo", "action": "update"},
			))
		}
		status := getStringArg(input, "status", "")
		if status == "" {
			return resultFromMap(formatErrorResponse(
				"validation_error",
				"The 'status' parameter is required for update",
				"Please provide the new status: pending, in_progress, or done.",
				map[string]any{"skill": "todo", "action": "update"},
			))
		}
		if status != "pending" && status != "in_progress" && status != "done" {
			return resultFromMap(formatErrorResponse(
				"validation_error",
				fmt.Sprintf("Invalid status '%s'", status),
				"Status must be one of: pending, in_progress, done.",
				map[string]any{"skill": "todo", "action": "update"},
			))
		}
		item, err := t.store.Update(id, status)
		if err != nil {
			return resultFromMap(formatErrorResponse(
				"not_found",
				err.Error(),
				"Use the 'list' action to see available tasks.",
				map[string]any{"skill": "todo", "action": "update", "id": id},
			))
		}
		return resultFromMap(map[string]any{
			"success": true,
			"item":    item,
		})

	case "list":
		items := t.store.List()
		return resultFromMap(map[string]any{
			"count": len(items),
			"items": items,
		})

	default:
		return resultFromMap(formatErrorResponse(
			"validation_error",
			fmt.Sprintf("Unknown action '%s'", action),
			"Action must be one of: create, update, list.",
			map[string]any{"skill": "todo"},
		))
	}
}

// marshalResult is a helper that marshals the result to JSON content.
func marshalTodoResult(v any) (tools.ToolResult, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return tools.ToolResult{Error: true, Content: err.Error()}, nil
	}
	return tools.ToolResult{Content: string(data)}, nil
}
