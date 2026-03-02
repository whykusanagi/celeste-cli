package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/skills"
)

func TestResolveWorkspacePathBlocksTraversal(t *testing.T) {
	workspace := t.TempDir()

	_, err := resolveWorkspacePath(workspace, "../outside.txt")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "escapes workspace")

	inside, err := resolveWorkspacePath(workspace, "subdir/file.txt")
	require.NoError(t, err)
	assert.True(t, filepath.HasPrefix(inside, workspace))
}

func TestDevSkillsReadWriteSearchFlow(t *testing.T) {
	workspace := t.TempDir()
	registry := skills.NewRegistry()
	err := RegisterDevSkills(registry, workspace)
	require.NoError(t, err)

	_, err = registry.Execute("dev_write_file", map[string]interface{}{
		"path":    "notes/todo.txt",
		"content": "hello\nceleste\nagent",
	})
	require.NoError(t, err)

	res, err := registry.Execute("dev_read_file", map[string]interface{}{
		"path": "notes/todo.txt",
	})
	require.NoError(t, err)
	resMap, ok := res.(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, resMap["content"], "celeste")

	searchRes, err := registry.Execute("dev_search_files", map[string]interface{}{
		"pattern": "agent",
	})
	require.NoError(t, err)
	searchMap, ok := searchRes.(map[string]interface{})
	require.True(t, ok)
	matches, ok := searchMap["matches"].([]map[string]interface{})
	if !ok {
		generic, ok2 := searchMap["matches"].([]interface{})
		require.True(t, ok2)
		require.NotEmpty(t, generic)
	} else {
		require.NotEmpty(t, matches)
	}

	listRes, err := registry.Execute("dev_list_files", map[string]interface{}{"path": "notes"})
	require.NoError(t, err)
	listMap, ok := listRes.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, 1, listMap["count"])
}

func TestDevRunCommandExecutesInWorkspace(t *testing.T) {
	workspace := t.TempDir()
	registry := skills.NewRegistry()
	err := RegisterDevSkills(registry, workspace)
	require.NoError(t, err)

	result, err := registry.Execute("dev_run_command", map[string]interface{}{
		"command": "pwd",
	})
	require.NoError(t, err)
	resMap, ok := result.(map[string]interface{})
	require.True(t, ok)

	output, _ := resMap["output"].(string)
	assert.Contains(t, output, workspace)

	_, statErr := os.Stat(filepath.Join(workspace, ".."))
	assert.NoError(t, statErr)
}
