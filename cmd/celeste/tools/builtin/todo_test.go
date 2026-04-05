package builtin

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTodoCreate(t *testing.T) {
	tool := NewTodoTool(t.TempDir())
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"action": "create",
		"title":  "Write tests",
	}, nil)
	require.NoError(t, err)
	assert.False(t, result.Error)
	assert.Contains(t, result.Content, "Write tests")
	assert.Contains(t, result.Content, "pending")
}

func TestTodoCreateRequiresTitle(t *testing.T) {
	tool := NewTodoTool(t.TempDir())
	result, err := tool.Execute(context.Background(), map[string]any{
		"action": "create",
	}, nil)
	require.NoError(t, err)
	assert.True(t, result.Error)
	assert.Contains(t, result.Content, "title")
}

func TestTodoUpdate(t *testing.T) {
	tool := NewTodoTool(t.TempDir())
	ctx := context.Background()

	tool.Execute(ctx, map[string]any{"action": "create", "title": "Do stuff"}, nil)

	result, err := tool.Execute(ctx, map[string]any{
		"action": "update", "id": 1, "status": "done",
	}, nil)
	require.NoError(t, err)
	assert.False(t, result.Error)
	assert.Contains(t, result.Content, "done")
}

func TestTodoUpdateNotFound(t *testing.T) {
	tool := NewTodoTool(t.TempDir())
	result, err := tool.Execute(context.Background(), map[string]any{
		"action": "update", "id": 999, "status": "done",
	}, nil)
	require.NoError(t, err)
	assert.True(t, result.Error)
}

func TestTodoDelete(t *testing.T) {
	tool := NewTodoTool(t.TempDir())
	ctx := context.Background()

	tool.Execute(ctx, map[string]any{"action": "create", "title": "Delete me"}, nil)

	result, err := tool.Execute(ctx, map[string]any{"action": "delete", "id": 1}, nil)
	require.NoError(t, err)
	assert.False(t, result.Error)

	listResult, _ := tool.Execute(ctx, map[string]any{"action": "list"}, nil)
	assert.Contains(t, listResult.Content, `"count":0`)
}

func TestTodoClearDone(t *testing.T) {
	tool := NewTodoTool(t.TempDir())
	ctx := context.Background()

	tool.Execute(ctx, map[string]any{"action": "create", "title": "A"}, nil)
	tool.Execute(ctx, map[string]any{"action": "create", "title": "B"}, nil)
	tool.Execute(ctx, map[string]any{"action": "update", "id": 1, "status": "done"}, nil)

	result, err := tool.Execute(ctx, map[string]any{"action": "clear_done"}, nil)
	require.NoError(t, err)
	assert.Contains(t, result.Content, "1")

	listResult, _ := tool.Execute(ctx, map[string]any{"action": "list"}, nil)
	assert.Contains(t, listResult.Content, `"count":1`)
}

func TestTodoList(t *testing.T) {
	tool := NewTodoTool(t.TempDir())
	ctx := context.Background()

	tool.Execute(ctx, map[string]any{"action": "create", "title": "A"}, nil)
	tool.Execute(ctx, map[string]any{"action": "create", "title": "B"}, nil)

	result, err := tool.Execute(ctx, map[string]any{"action": "list"}, nil)
	require.NoError(t, err)

	var resp map[string]any
	require.NoError(t, json.Unmarshal([]byte(result.Content), &resp))
	assert.Equal(t, float64(2), resp["count"])
}

func TestTodoPersistence(t *testing.T) {
	dir := t.TempDir()

	// Create tasks with first tool instance
	tool1 := NewTodoTool(dir)
	tool1.Execute(context.Background(), map[string]any{
		"action": "create", "title": "Persistent task",
	}, nil)

	// Create second tool instance — should load from disk
	tool2 := NewTodoTool(dir)
	result, err := tool2.Execute(context.Background(), map[string]any{"action": "list"}, nil)
	require.NoError(t, err)
	assert.Contains(t, result.Content, "Persistent task")
	assert.Contains(t, result.Content, `"count":1`)
}

func TestTodoStoreThreadSafety(t *testing.T) {
	store := NewTodoStore(t.TempDir())
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func() {
			store.Create("task", "")
			store.List()
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
	assert.Len(t, store.List(), 10)
}
