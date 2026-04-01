// cmd/celeste/permissions/config_test.go
package permissions

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, ModeDefault, cfg.Mode)

	// Should have sudo and su in alwaysDeny
	assert.GreaterOrEqual(t, len(cfg.AlwaysDeny), 2)

	hasSudo := false
	hasSu := false
	for _, rule := range cfg.AlwaysDeny {
		if rule.ToolPattern == "bash(sudo *)" {
			hasSudo = true
		}
		if rule.ToolPattern == "bash(su *)" {
			hasSu = true
		}
	}
	assert.True(t, hasSudo, "default config should deny sudo")
	assert.True(t, hasSu, "default config should deny su")

	// Default alwaysAllow should include read tools
	assert.NotEmpty(t, cfg.AlwaysAllow)
}

func TestSaveAndLoadConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "permissions.json")

	original := PermissionConfig{
		Mode: ModeStrict,
		AlwaysAllow: []Rule{
			{ToolPattern: "read_file", Decision: Allow},
			{ToolPattern: "list_files", Decision: Allow},
		},
		AlwaysDeny: []Rule{
			{ToolPattern: "bash(sudo *)", Decision: Deny},
			{ToolPattern: "bash(rm -rf *)", Decision: Deny},
		},
		PatternRules: []Rule{
			{ToolPattern: "bash(git *)", Decision: Allow},
		},
	}

	// Save
	err := SaveConfig(path, &original)
	require.NoError(t, err)

	// Verify file exists
	_, err = os.Stat(path)
	require.NoError(t, err)

	// Load
	loaded, err := LoadConfig(path)
	require.NoError(t, err)

	assert.Equal(t, original.Mode, loaded.Mode)
	assert.Equal(t, len(original.AlwaysAllow), len(loaded.AlwaysAllow))
	assert.Equal(t, len(original.AlwaysDeny), len(loaded.AlwaysDeny))
	assert.Equal(t, len(original.PatternRules), len(loaded.PatternRules))

	for i, rule := range original.AlwaysAllow {
		assert.Equal(t, rule.ToolPattern, loaded.AlwaysAllow[i].ToolPattern)
	}
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.json")

	cfg, err := LoadConfig(path)
	require.NoError(t, err, "missing file should return default config, not error")
	assert.Equal(t, ModeDefault, cfg.Mode)
	assert.NotEmpty(t, cfg.AlwaysDeny, "should have default deny rules")
}

func TestLoadConfig_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	err := os.WriteFile(path, []byte("{invalid json"), 0644)
	require.NoError(t, err)

	_, err = LoadConfig(path)
	assert.Error(t, err)
}

func TestLoadConfig_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.json")
	err := os.WriteFile(path, []byte("{}"), 0644)
	require.NoError(t, err)

	cfg, err := LoadConfig(path)
	require.NoError(t, err)
	assert.Equal(t, ModeDefault, cfg.Mode, "empty config should default to ModeDefault")
}

func TestLoadConfig_InvalidMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "invalid_mode.json")
	err := os.WriteFile(path, []byte(`{"mode":"yolo"}`), 0644)
	require.NoError(t, err)

	_, err = LoadConfig(path)
	assert.Error(t, err, "invalid mode should return error")
}

func TestSaveConfig_CreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "deep", "permissions.json")

	err := SaveConfig(path, &PermissionConfig{Mode: ModeDefault})
	require.NoError(t, err)

	_, err = os.Stat(path)
	require.NoError(t, err)
}

func TestConfigJSON_Format(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "permissions.json")

	cfg := PermissionConfig{
		Mode: ModeDefault,
		AlwaysAllow: []Rule{
			{ToolPattern: "read_file", Decision: Allow},
		},
		AlwaysDeny: []Rule{
			{ToolPattern: "bash(sudo *)", Decision: Deny},
		},
	}

	err := SaveConfig(path, &cfg)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	content := string(data)
	assert.Contains(t, content, `"mode"`)
	assert.Contains(t, content, `"default"`)
	assert.Contains(t, content, `"always_allow"`)
	assert.Contains(t, content, `"always_deny"`)
	assert.Contains(t, content, `"read_file"`)
	assert.Contains(t, content, `"bash(sudo *)"`)
}

func TestDefaultConfigPath(t *testing.T) {
	path := DefaultConfigPath()
	assert.Contains(t, path, ".celeste")
	assert.Contains(t, path, "permissions.json")
}
