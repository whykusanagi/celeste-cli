package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/gittest"
)

func initGitRepo(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("git tools use sh -c which is not available on Windows")
	}
	dir := t.TempDir()
	run := func(args ...string) { gittest.Run(t, dir, args[1:]...) }
	run("git", "init")
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

// Aikido 806869318: git_status is read-only and auto-allowed, so it must
// not run a program the repository's config names (core.fsmonitor), which
// a sandboxed command could have planted.
func TestGitStatusRunsNoRepositoryPrograms(t *testing.T) {
	dir := initGitRepo(t)
	marker := filepath.Join(t.TempDir(), "ran")
	gittest.Run(t, dir, "config", "core.fsmonitor", "echo x >> '"+marker+"'; false")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "file.txt"), []byte("changed\n"), 0o644))
	if _, err := NewGitStatusTool(dir).Execute(context.Background(), map[string]any{}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("git_status ran the repository's core.fsmonitor")
	}
}
