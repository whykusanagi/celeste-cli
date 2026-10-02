package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/gittest"
)

func TestGitLogToolName(t *testing.T) {
	tool := NewGitLogTool("/tmp")
	assert.Equal(t, "git_log", tool.Name())
	assert.True(t, tool.IsReadOnly())
	assert.True(t, tool.IsConcurrencySafe(nil))
}

func TestGitLogToolExecute(t *testing.T) {
	dir := initGitRepo(t)

	// Add a second commit.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "second.txt"), []byte("two\n"), 0644))
	gittest.Run(t, dir, "add", "second.txt")
	gittest.Run(t, dir, "commit", "-m", "second commit")

	tool := NewGitLogTool(dir)
	result, err := tool.Execute(context.Background(), map[string]any{
		"count": 5,
	}, nil)
	require.NoError(t, err)
	assert.False(t, result.Error)

	var data map[string]any
	require.NoError(t, json.Unmarshal([]byte(result.Content), &data))
	commits, ok := data["commits"].([]any)
	require.True(t, ok)
	assert.Len(t, commits, 2)

	first := commits[0].(map[string]any)
	assert.Equal(t, "second commit", first["message"])
	assert.NotEmpty(t, first["hash"])
	assert.NotEmpty(t, first["author"])
	assert.NotEmpty(t, first["date"])
}

func TestGitLogToolExecute_WithPath(t *testing.T) {
	dir := initGitRepo(t)

	// Add a second file and commit.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "other.txt"), []byte("other\n"), 0644))
	run := func(args ...string) { gittest.Run(t, dir, args[1:]...) }
	run("git", "add", "other.txt")
	run("git", "commit", "-m", "add other")

	tool := NewGitLogTool(dir)
	result, err := tool.Execute(context.Background(), map[string]any{
		"path": "other.txt",
	}, nil)
	require.NoError(t, err)
	assert.False(t, result.Error)

	var data map[string]any
	require.NoError(t, json.Unmarshal([]byte(result.Content), &data))
	commits := data["commits"].([]any)
	assert.Len(t, commits, 1)
	assert.Equal(t, "add other", commits[0].(map[string]any)["message"])
}

func TestGitLogToolExecute_NotARepo(t *testing.T) {
	dir := t.TempDir()
	tool := NewGitLogTool(dir)
	result, err := tool.Execute(context.Background(), map[string]any{}, nil)
	require.NoError(t, err)
	assert.True(t, result.Error)
}

func TestGitLogToolExecute_WithAuthor(t *testing.T) {
	dir := initGitRepo(t)
	tool := NewGitLogTool(dir)
	result, err := tool.Execute(context.Background(), map[string]any{
		"author": gittest.Name,
	}, nil)
	require.NoError(t, err)
	assert.False(t, result.Error)

	var data map[string]any
	require.NoError(t, json.Unmarshal([]byte(result.Content), &data))
	commits := data["commits"].([]any)
	assert.GreaterOrEqual(t, len(commits), 1)
}
