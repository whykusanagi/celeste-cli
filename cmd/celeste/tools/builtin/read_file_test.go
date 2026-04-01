package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadFileToolName(t *testing.T) {
	rt := NewReadFileTool("/tmp")
	assert.Equal(t, "read_file", rt.Name())
}

func TestReadFileToolProperties(t *testing.T) {
	rt := NewReadFileTool("/tmp")
	assert.True(t, rt.IsReadOnly())
	assert.True(t, rt.IsConcurrencySafe(nil))
}

func TestReadFileToolReadTempFile(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("line1\nline2\nline3"), 0644))

	rt := NewReadFileTool(dir)
	result, err := rt.Execute(context.Background(), map[string]any{
		"path": "test.txt",
	}, nil)
	require.NoError(t, err)
	assert.False(t, result.Error)

	var data map[string]any
	require.NoError(t, json.Unmarshal([]byte(result.Content), &data))
	assert.Contains(t, data["content"], "line1")
	assert.Contains(t, data["content"], "line3")
	assert.Equal(t, float64(3), data["total_lines"])
}

func TestReadFileToolLineRange(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("line1\nline2\nline3\nline4"), 0644))

	rt := NewReadFileTool(dir)
	result, err := rt.Execute(context.Background(), map[string]any{
		"path":       "test.txt",
		"start_line": 2,
		"end_line":   3,
	}, nil)
	require.NoError(t, err)
	assert.False(t, result.Error)

	var data map[string]any
	require.NoError(t, json.Unmarshal([]byte(result.Content), &data))
	assert.Equal(t, "line2\nline3", data["content"])
}

func TestReadFileToolPathTraversalPrevention(t *testing.T) {
	dir := t.TempDir()

	rt := NewReadFileTool(dir)
	result, err := rt.Execute(context.Background(), map[string]any{
		"path": "../../../etc/passwd",
	}, nil)
	require.NoError(t, err)
	assert.True(t, result.Error)
	assert.Contains(t, result.Content, "escapes workspace")
}

func TestReadFileToolRequiredField(t *testing.T) {
	rt := NewReadFileTool("/tmp")
	result, err := rt.Execute(context.Background(), map[string]any{}, nil)
	require.NoError(t, err)
	assert.True(t, result.Error)
	assert.Contains(t, result.Content, "path")
}
