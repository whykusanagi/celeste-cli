package builtin

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func initGitRepo(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("git tools use sh -c which is not available on Windows")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "command %v failed: %s", args, string(out))
	}
	run("git", "init")
	run("git", "config", "user.email", "test@test.com")
	run("git", "config", "user.name", "Test User")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "file.txt"), []byte("hello\n"), 0644))
	run("git", "add", "file.txt")
	run("git", "commit", "-m", "initial commit")
	return dir
}

func TestGitStatusToolName(t *testing.T) {
	tool := NewGitStatusTool("/tmp")
	assert.Equal(t, "git_status", tool.Name())
	assert.True(t, tool.IsReadOnly())
	assert.True(t, tool.IsConcurrencySafe(nil))
}

func TestGitStatusToolExecute(t *testing.T) {
	dir := initGitRepo(t)

	// Create an untracked file so status is non-empty.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new\n"), 0644))

	tool := NewGitStatusTool(dir)
	result, err := tool.Execute(context.Background(), map[string]any{}, nil)
	require.NoError(t, err)
	assert.False(t, result.Error)

	var data map[string]any
	require.NoError(t, json.Unmarshal([]byte(result.Content), &data))
	assert.NotEmpty(t, data["branch"])
	assert.Contains(t, data["status"], "new.txt")
}

func TestGitStatusToolExecute_EmptyInput(t *testing.T) {
	dir := initGitRepo(t)
	tool := NewGitStatusTool(dir)
	result, err := tool.Execute(context.Background(), nil, nil)
	require.NoError(t, err)
	assert.False(t, result.Error)
}
