package skills

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateSkillDefinition(t *testing.T) {
	valid := Skill{
		Name:        "test_skill",
		Description: "Valid test skill",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"input": map[string]interface{}{
					"type": "string",
				},
			},
			"required": []string{"input"},
		},
	}

	require.NoError(t, ValidateSkillDefinition(valid))

	invalid := valid
	invalid.Parameters = map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"input": map[string]interface{}{
				"type": "string",
			},
		},
		"required": []string{"missing_field"},
	}
	require.Error(t, ValidateSkillDefinition(invalid))
}

func TestLoadSkillsSkipsInvalidCustomSchema(t *testing.T) {
	tmpDir := t.TempDir()
	registry := NewRegistry()
	registry.SetSkillsDir(tmpDir)

	invalid := Skill{
		Name:        "broken_skill",
		Description: "Missing parameters.type",
		Parameters: map[string]interface{}{
			"properties": map[string]interface{}{
				"value": map[string]interface{}{
					"type": "string",
				},
			},
			"required": []string{"value"},
		},
	}

	data, err := json.Marshal(invalid)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(tmpDir, "broken_skill.json"), data, 0644)
	require.NoError(t, err)

	err = registry.LoadSkills()
	require.NoError(t, err)

	_, exists := registry.GetSkill("broken_skill")
	assert.False(t, exists, "invalid custom skill should be skipped")
}

func TestLoadSkillsLoadsValidCustomSchema(t *testing.T) {
	tmpDir := t.TempDir()
	registry := NewRegistry()
	registry.SetSkillsDir(tmpDir)

	valid := Skill{
		Name:        "custom_echo",
		Description: "Echoes back user input",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"text": map[string]interface{}{
					"type": "string",
				},
			},
			"required": []string{"text"},
		},
	}

	data, err := json.Marshal(valid)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(tmpDir, "custom_echo.json"), data, 0644)
	require.NoError(t, err)

	err = registry.LoadSkills()
	require.NoError(t, err)

	loaded, exists := registry.GetSkill("custom_echo")
	require.True(t, exists, "valid custom skill should load")
	assert.Equal(t, "custom_echo", loaded.Name)
}

func TestSaveSkillRejectsInvalidSchema(t *testing.T) {
	tmpDir := t.TempDir()
	registry := NewRegistry()
	registry.SetSkillsDir(tmpDir)

	err := registry.SaveSkill(Skill{
		Name:        "bad_save",
		Description: "Bad schema",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"value": map[string]interface{}{
					"type": "string",
				},
			},
			"required": []string{"unknown"},
		},
	})
	require.Error(t, err)
}
