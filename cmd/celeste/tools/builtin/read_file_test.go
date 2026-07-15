package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func TestReadFile_BudgetTruncatesOnLineBoundary(t *testing.T) {
	dir := t.TempDir()
	var lines []string
	for i := 1; i <= 10; i++ {
		lines = append(lines, fmt.Sprintf("line%02d-%s", i, strings.Repeat("x", 90))) // 97 bytes each
	}
	full := strings.Join(lines, "\n")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "big.txt"), []byte(full), 0644))

	rt := NewReadFileTool(dir, WithMaxResultBytes(250))
	res, err := rt.Execute(context.Background(), map[string]any{"path": "big.txt"}, nil)
	require.NoError(t, err)

	var d map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Content), &d))
	got := d["content"].(string)

	assert.LessOrEqual(t, len(got), 250, "content must not exceed the byte budget")
	assert.True(t, d["truncated"].(bool))
	for _, ln := range strings.Split(got, "\n") {
		assert.Equal(t, 97, len(ln), "each returned line must be complete (line-aligned cut)")
	}
	assert.Equal(t, float64(len(full)), d["total_bytes"])
	assert.Equal(t, float64(len(got)), d["returned_bytes"])
	require.Contains(t, d, "next_offset_line")
	assert.Greater(t, int(d["next_offset_line"].(float64)), 1)
}

func TestReadFile_SmallFileNotTruncated(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "s.txt"), []byte("a\nb\nc"), 0644))
	rt := NewReadFileTool(dir, WithMaxResultBytes(1000))
	res, err := rt.Execute(context.Background(), map[string]any{"path": "s.txt"}, nil)
	require.NoError(t, err)
	var d map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Content), &d))
	assert.False(t, d["truncated"].(bool))
	assert.NotContains(t, d, "next_offset_line")
	assert.Equal(t, "a\nb\nc", d["content"])
}

func TestReadFile_MinifiedSingleLineIsByteBounded(t *testing.T) {
	// The incident's case: whole payload on one line. A range read must stay
	// byte-bounded and never emit the 131072-byte poison blob.
	dir := t.TempDir()
	payload := strings.Repeat("A", 200_000) // one 200 KB line, no newlines
	require.NoError(t, os.WriteFile(filepath.Join(dir, "min.html"), []byte(payload), 0644))

	rt := NewReadFileTool(dir, WithMaxResultBytes(4096))
	res, err := rt.Execute(context.Background(), map[string]any{
		"path": "min.html", "start_line": 1, "end_line": 220,
	}, nil)
	require.NoError(t, err)
	var d map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Content), &d))
	got := d["content"].(string)
	assert.LessOrEqual(t, len(got), 4096, "single giant line must still be byte-bounded")
	assert.True(t, d["truncated"].(bool))
	assert.Equal(t, float64(200_000), d["total_bytes"])
	assert.Equal(t, float64(2), d["next_offset_line"])
}
