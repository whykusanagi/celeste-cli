package builtin

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTodoCreate(t *testing.T) {
	tool := NewTodoTool()
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"action": "create",
		"title":  "Write tests",
	}, nil)
	require.NoError(t, err)
	assert.False(t, result.Error)

	var resp map[string]any
	require.NoError(t, json.Unmarshal([]byte(result.Content), &resp))
	assert.True(t, resp["success"].(bool))

	item := resp["item"].(map[string]any)
	assert.Equal(t, "Write tests", item["title"])
	assert.Equal(t, "pending", item["status"])
	assert.Equal(t, float64(1), item["id"])
}

func TestTodoCreateRequiresTitle(t *testing.T) {
	tool := NewTodoTool()
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"action": "create",
	}, nil)
	require.NoError(t, err)

	var resp map[string]any
	require.NoError(t, json.Unmarshal([]byte(result.Content), &resp))
	assert.Equal(t, "validation_error", resp["error_type"])
}

func TestTodoUpdate(t *testing.T) {
	tool := NewTodoTool()
	ctx := context.Background()

	// Create first.
	_, err := tool.Execute(ctx, map[string]any{
		"action": "create",
		"title":  "Do stuff",
	}, nil)
	require.NoError(t, err)

	// Update to done.
	result, err := tool.Execute(ctx, map[string]any{
		"action": "update",
		"id":     1,
		"status": "done",
	}, nil)
	require.NoError(t, err)

	var resp map[string]any
	require.NoError(t, json.Unmarshal([]byte(result.Content), &resp))
	assert.True(t, resp["success"].(bool))
	item := resp["item"].(map[string]any)
	assert.Equal(t, "done", item["status"])
}

func TestTodoUpdateNotFound(t *testing.T) {
	tool := NewTodoTool()
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"action": "update",
		"id":     999,
		"status": "done",
	}, nil)
	require.NoError(t, err)

	var resp map[string]any
	require.NoError(t, json.Unmarshal([]byte(result.Content), &resp))
	assert.Equal(t, "not_found", resp["error_type"])
}

func TestTodoUpdateInvalidStatus(t *testing.T) {
	tool := NewTodoTool()
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"action": "update",
		"id":     1,
		"status": "bogus",
	}, nil)
	require.NoError(t, err)

	var resp map[string]any
	require.NoError(t, json.Unmarshal([]byte(result.Content), &resp))
	assert.Equal(t, "validation_error", resp["error_type"])
}

func TestTodoList(t *testing.T) {
	tool := NewTodoTool()
	ctx := context.Background()

	// Create two items.
	tool.Execute(ctx, map[string]any{"action": "create", "title": "A"}, nil)
	tool.Execute(ctx, map[string]any{"action": "create", "title": "B"}, nil)

	result, err := tool.Execute(ctx, map[string]any{"action": "list"}, nil)
	require.NoError(t, err)

	var resp map[string]any
	require.NoError(t, json.Unmarshal([]byte(result.Content), &resp))
	assert.Equal(t, float64(2), resp["count"])
	items := resp["items"].([]any)
	assert.Len(t, items, 2)
}

func TestTodoListEmpty(t *testing.T) {
	tool := NewTodoTool()
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{"action": "list"}, nil)
	require.NoError(t, err)

	var resp map[string]any
	require.NoError(t, json.Unmarshal([]byte(result.Content), &resp))
	assert.Equal(t, float64(0), resp["count"])
}

func TestTodoUnknownAction(t *testing.T) {
	tool := NewTodoTool()
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{"action": "delete"}, nil)
	require.NoError(t, err)

	var resp map[string]any
	require.NoError(t, json.Unmarshal([]byte(result.Content), &resp))
	assert.Equal(t, "validation_error", resp["error_type"])
}

func TestTodoStoreThreadSafety(t *testing.T) {
	store := NewTodoStore()
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func(n int) {
			store.Create("task")
			store.List()
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}
	assert.Len(t, store.List(), 10)
}
