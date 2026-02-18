package skills

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewRegistry tests registry creation
func TestNewRegistry(t *testing.T) {
	registry := NewRegistry()
	require.NotNil(t, registry)
	assert.NotNil(t, registry.skills)
	assert.NotNil(t, registry.handlers)
}

// TestRegisterSkill tests skill registration
func TestRegisterSkill(t *testing.T) {
	registry := NewRegistry()

	skill := Skill{
		Name:        "test_skill",
		Description: "A test skill",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"param1": map[string]interface{}{
					"type":        "string",
					"description": "Test parameter",
				},
			},
			"required": []string{"param1"},
		},
	}

	registry.RegisterSkill(skill)

	// Verify skill was registered
	registeredSkill, exists := registry.GetSkill("test_skill")
	assert.True(t, exists, "skill should be registered")
	assert.Equal(t, skill.Name, registeredSkill.Name)
	assert.Equal(t, skill.Description, registeredSkill.Description)
}

// TestRegisterHandler tests handler registration
func TestRegisterHandler(t *testing.T) {
	registry := NewRegistry()

	called := false
	handler := func(args map[string]interface{}) (interface{}, error) {
		called = true
		return map[string]interface{}{"success": true}, nil
	}

	registry.RegisterHandler("test_handler", handler)

	// Register skill definition (required for Execute to work)
	registry.RegisterSkill(Skill{Name: "test_handler", Description: "Test handler"})

	// Verify handler was registered
	result, err := registry.Execute("test_handler", map[string]interface{}{})
	require.NoError(t, err)
	assert.True(t, called, "handler should have been called")

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.True(t, resultMap["success"].(bool))
}

// TestGetSkill tests skill retrieval
func TestGetSkill(t *testing.T) {
	registry := NewRegistry()

	// Test non-existent skill
	_, exists := registry.GetSkill("nonexistent")
	assert.False(t, exists, "non-existent skill should not be found")

	// Register and retrieve
	skill := Skill{
		Name:        "existing_skill",
		Description: "An existing skill",
		Parameters:  map[string]interface{}{},
	}
	registry.RegisterSkill(skill)

	retrieved, exists := registry.GetSkill("existing_skill")
	assert.True(t, exists, "existing skill should be found")
	assert.Equal(t, skill.Name, retrieved.Name)
}

// TestExecute tests skill execution
func TestExecute(t *testing.T) {
	registry := NewRegistry()

	// Test non-existent skill
	_, err := registry.Execute("nonexistent", map[string]interface{}{})
	assert.Error(t, err, "should error for non-existent skill")
	assert.Contains(t, err.Error(), "skill not found", "error should indicate missing skill")

	// Test skill without handler
	registry.RegisterSkill(Skill{Name: "no_handler", Description: "Skill without handler"})
	_, err = registry.Execute("no_handler", map[string]interface{}{})
	assert.Error(t, err, "should error for skill without handler")
	assert.Contains(t, err.Error(), "no handler", "error should indicate missing handler")

	// Register and execute successful handler
	registry.RegisterSkill(Skill{Name: "success_handler", Description: "Success handler"})
	registry.RegisterHandler("success_handler", func(args map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{
			"status": "success",
			"input":  args["data"],
		}, nil
	})

	result, err := registry.Execute("success_handler", map[string]interface{}{"data": "test"})
	require.NoError(t, err)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "success", resultMap["status"])
	assert.Equal(t, "test", resultMap["input"])

	// Register and execute handler with arguments
	registry.RegisterSkill(Skill{Name: "echo_handler", Description: "Echo handler"})
	registry.RegisterHandler("echo_handler", func(args map[string]interface{}) (interface{}, error) {
		message, ok := args["message"].(string)
		if !ok {
			return map[string]interface{}{"error": "missing message"}, nil
		}
		return map[string]interface{}{"echo": message}, nil
	})

	result, err = registry.Execute("echo_handler", map[string]interface{}{"message": "hello"})
	require.NoError(t, err)

	resultMap, ok = result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "hello", resultMap["echo"])
}

// TestGetAllSkills tests listing all registered skills
func TestGetAllSkills(t *testing.T) {
	registry := NewRegistry()

	// Empty registry
	skills := registry.GetAllSkills()
	assert.Empty(t, skills, "new registry should have no skills")

	// Add some skills
	skill1 := Skill{Name: "skill1", Description: "First skill"}
	skill2 := Skill{Name: "skill2", Description: "Second skill"}
	skill3 := Skill{Name: "skill3", Description: "Third skill"}

	registry.RegisterSkill(skill1)
	registry.RegisterSkill(skill2)
	registry.RegisterSkill(skill3)

	skills = registry.GetAllSkills()
	assert.Len(t, skills, 3, "should have 3 registered skills")

	// Verify all skills are present
	skillNames := make(map[string]bool)
	for _, skill := range skills {
		skillNames[skill.Name] = true
	}

	assert.True(t, skillNames["skill1"])
	assert.True(t, skillNames["skill2"])
	assert.True(t, skillNames["skill3"])
}

// TestSkillOverwrite tests that registering a skill with the same name overwrites
func TestSkillOverwrite(t *testing.T) {
	registry := NewRegistry()

	skill1 := Skill{Name: "test", Description: "Original description"}
	skill2 := Skill{Name: "test", Description: "Updated description"}

	registry.RegisterSkill(skill1)
	retrieved1, exists := registry.GetSkill("test")
	require.True(t, exists)
	assert.Equal(t, "Original description", retrieved1.Description)

	registry.RegisterSkill(skill2)
	retrieved2, exists := registry.GetSkill("test")
	require.True(t, exists)
	assert.Equal(t, "Updated description", retrieved2.Description)
}

// TestHandlerOverwrite tests that registering a handler with the same name overwrites
func TestHandlerOverwrite(t *testing.T) {
	registry := NewRegistry()

	call1 := false
	call2 := false

	handler1 := func(args map[string]interface{}) (interface{}, error) {
		call1 = true
		return map[string]interface{}{"version": 1}, nil
	}

	handler2 := func(args map[string]interface{}) (interface{}, error) {
		call2 = true
		return map[string]interface{}{"version": 2}, nil
	}

	registry.RegisterSkill(Skill{Name: "test", Description: "Test"})
	registry.RegisterHandler("test", handler1)
	result1, err := registry.Execute("test", map[string]interface{}{})
	require.NoError(t, err)
	assert.True(t, call1)
	assert.False(t, call2)
	assert.Equal(t, 1, result1.(map[string]interface{})["version"].(int))

	// Reset flags
	call1 = false
	call2 = false

	registry.RegisterHandler("test", handler2)
	result2, err := registry.Execute("test", map[string]interface{}{})
	require.NoError(t, err)
	assert.False(t, call1)
	assert.True(t, call2)
	assert.Equal(t, 2, result2.(map[string]interface{})["version"].(int))
}

// TestBuiltinSkillsRegistration tests that all builtin skills register correctly
func TestBuiltinSkillsRegistration(t *testing.T) {
	registry := NewRegistry()

	// Create mock config loader
	mockConfig := NewMockConfigLoader()

	// Register builtin skills
	RegisterBuiltinSkills(registry, mockConfig)

	// List expected skill names (22 active skills)
	// Note: nsfw_mode, generate_content, generate_image are disabled (unimplemented)
	expectedSkills := []string{
		"tarot_reading",
		"get_weather",
		"convert_units",
		"convert_timezone",
		"generate_hash",
		"base64_encode",
		"base64_decode",
		"generate_uuid",
		"generate_password",
		"convert_currency",
		"generate_qr_code",
		"check_twitch_live",
		"get_youtube_videos",
		"set_reminder",
		"list_reminders",
		"save_note",
		"get_note",
		"list_notes",
		"ipfs",
		"alchemy",
		"blockmon",
		"wallet_security",
	}

	skills := registry.GetAllSkills()
	assert.Len(t, skills, len(expectedSkills), "should have all builtin skills registered")

	// Verify each expected skill exists
	for _, skillName := range expectedSkills {
		skill, exists := registry.GetSkill(skillName)
		assert.True(t, exists, "skill %s should be registered", skillName)
		assert.Equal(t, skillName, skill.Name)
		assert.NotEmpty(t, skill.Description, "skill %s should have a description", skillName)
		assert.NotNil(t, skill.Parameters, "skill %s should have parameters", skillName)
	}
}

// TestSkillParameters tests that skills have properly structured parameters
func TestSkillParameters(t *testing.T) {
	testCases := []struct {
		name           string
		skillFunc      func() Skill
		requiredParams []string
		optionalParams []string
	}{
		{
			name:           "UUID Generator",
			skillFunc:      UUIDGeneratorSkill,
			requiredParams: []string{},
			optionalParams: []string{},
		},
		{
			name:           "Base64 Encode",
			skillFunc:      Base64EncodeSkill,
			requiredParams: []string{"text"},
			optionalParams: []string{},
		},
		{
			name:           "Base64 Decode",
			skillFunc:      Base64DecodeSkill,
			requiredParams: []string{"encoded"},
			optionalParams: []string{},
		},
		{
			name:           "Hash Generator",
			skillFunc:      HashGeneratorSkill,
			requiredParams: []string{"text", "algorithm"},
			optionalParams: []string{},
		},
		{
			name:           "Password Generator",
			skillFunc:      PasswordGeneratorSkill,
			requiredParams: []string{},
			optionalParams: []string{"length", "include_symbols", "include_numbers"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			skill := tc.skillFunc()

			assert.NotEmpty(t, skill.Name, "skill should have a name")
			assert.NotEmpty(t, skill.Description, "skill should have a description")
			assert.NotNil(t, skill.Parameters, "skill should have parameters")

			params := skill.Parameters

			// Check required fields exist
			if len(tc.requiredParams) > 0 {
				requiredRaw, ok := params["required"]
				require.True(t, ok, "should have required field")

				// Convert to []string
				requiredSlice, ok := requiredRaw.([]string)
				require.True(t, ok, "required should be []string")
				assert.ElementsMatch(t, tc.requiredParams, requiredSlice, "required parameters should match")
			}

			// Check properties exist
			propertiesRaw, ok := params["properties"]
			require.True(t, ok, "should have properties field")
			properties, ok := propertiesRaw.(map[string]interface{})
			require.True(t, ok, "properties should be a map")

			for _, param := range tc.requiredParams {
				assert.Contains(t, properties, param, "should have property for %s", param)
			}

			for _, param := range tc.optionalParams {
				assert.Contains(t, properties, param, "should have property for %s", param)
			}
		})
	}
}

// TestHasHandler tests handler existence checking
func TestHasHandler(t *testing.T) {
	registry := NewRegistry()

	// Test non-existent handler
	assert.False(t, registry.HasHandler("nonexistent"), "should return false for non-existent handler")

	// Register handler
	registry.RegisterHandler("test_handler", func(args map[string]interface{}) (interface{}, error) {
		return nil, nil
	})

	// Test registered handler
	assert.True(t, registry.HasHandler("test_handler"), "should return true for registered handler")
}

// TestDeleteSkill tests skill deletion
func TestDeleteSkill(t *testing.T) {
	registry := NewRegistry()

	// Register a skill
	skill := Skill{Name: "to_delete", Description: "Will be deleted"}
	registry.RegisterSkill(skill)

	// Verify it exists
	_, exists := registry.GetSkill("to_delete")
	assert.True(t, exists, "skill should exist before deletion")

	// Delete the skill
	err := registry.DeleteSkill("to_delete")
	require.NoError(t, err, "deletion should succeed")

	// Verify it's gone
	_, exists = registry.GetSkill("to_delete")
	assert.False(t, exists, "skill should not exist after deletion")

	// Try deleting non-existent skill (should succeed silently)
	err = registry.DeleteSkill("nonexistent")
	assert.NoError(t, err, "deleting non-existent skill should not error (silently succeeds)")
}

// TestCount tests skill counting
func TestCount(t *testing.T) {
	registry := NewRegistry()

	// Empty registry
	assert.Equal(t, 0, registry.Count(), "new registry should have 0 skills")

	// Add skills
	registry.RegisterSkill(Skill{Name: "skill1", Description: "First"})
	assert.Equal(t, 1, registry.Count(), "should have 1 skill")

	registry.RegisterSkill(Skill{Name: "skill2", Description: "Second"})
	assert.Equal(t, 2, registry.Count(), "should have 2 skills")

	registry.RegisterSkill(Skill{Name: "skill3", Description: "Third"})
	assert.Equal(t, 3, registry.Count(), "should have 3 skills")

	// Overwriting should not change count
	registry.RegisterSkill(Skill{Name: "skill1", Description: "Updated"})
	assert.Equal(t, 3, registry.Count(), "overwriting should not change count")

	// Delete a skill
	registry.DeleteSkill("skill2")
	assert.Equal(t, 2, registry.Count(), "should have 2 skills after deletion")
}

// TestGetToolDefinitions tests OpenAI tool format generation
func TestGetToolDefinitions(t *testing.T) {
	registry := NewRegistry()

	// Empty registry
	tools := registry.GetToolDefinitions()
	assert.Empty(t, tools, "empty registry should return no tools")

	// Register some skills
	skill1 := Skill{
		Name:        "test_skill_1",
		Description: "First test skill",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"param1": map[string]interface{}{
					"type":        "string",
					"description": "First parameter",
				},
			},
			"required": []string{"param1"},
		},
	}

	skill2 := Skill{
		Name:        "test_skill_2",
		Description: "Second test skill",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"param2": map[string]interface{}{
					"type":        "number",
					"description": "Second parameter",
				},
			},
		},
	}

	registry.RegisterSkill(skill1)
	registry.RegisterSkill(skill2)

	// Get tool definitions
	tools = registry.GetToolDefinitions()
	assert.Len(t, tools, 2, "should have 2 tool definitions")

	// Verify structure
	for _, tool := range tools {
		assert.Equal(t, "function", tool["type"], "tool type should be function")

		functionDef, ok := tool["function"].(map[string]interface{})
		require.True(t, ok, "tool should have function definition as map")

		assert.NotEmpty(t, functionDef["name"], "function should have a name")
		assert.NotEmpty(t, functionDef["description"], "function should have a description")
		assert.NotNil(t, functionDef["parameters"], "function should have parameters")
	}

	// Find specific skill in tools
	var tool1 map[string]interface{}
	for _, tool := range tools {
		functionDef := tool["function"].(map[string]interface{})
		if functionDef["name"] == "test_skill_1" {
			tool1 = tool
			break
		}
	}
	require.NotNil(t, tool1, "should find test_skill_1 in tools")

	tool1Func := tool1["function"].(map[string]interface{})
	assert.Equal(t, "First test skill", tool1Func["description"])
}

// TestSetSkillsDir tests setting custom skills directory
func TestSetSkillsDir(t *testing.T) {
	registry := NewRegistry()

	// Set custom directory
	customDir := "/custom/skills/path"
	registry.SetSkillsDir(customDir)

	// Note: No direct way to verify, but we're ensuring no panic
	// The actual directory is used in LoadSkills
	assert.NotPanics(t, func() {
		registry.SetSkillsDir(customDir)
	}, "setting skills directory should not panic")
}
