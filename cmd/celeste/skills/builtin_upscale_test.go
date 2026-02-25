package skills

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpscaleImageSkill(t *testing.T) {
	skill := UpscaleImageSkill()

	assert.Equal(t, "upscale_image", skill.Name)
	assert.NotEmpty(t, skill.Description)

	props, ok := skill.Parameters["properties"].(map[string]interface{})
	require.True(t, ok, "parameters should have properties")
	assert.Contains(t, props, "image_path")
	assert.Contains(t, props, "scale")
	assert.Contains(t, props, "creativity")

	required, ok := skill.Parameters["required"].([]string)
	require.True(t, ok, "parameters should have required list")
	assert.Contains(t, required, "image_path")
}

func TestUpscaleImageHandler_MissingVeniceConfig(t *testing.T) {
	loader := NewMockConfigLoaderWithErrors()
	args := map[string]interface{}{"image_path": "/tmp/test.png"}

	_, err := UpscaleImageHandler(args, loader)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Venice.ai not configured")
}

func TestUpscaleImageHandler_MissingImagePath(t *testing.T) {
	loader := NewMockConfigLoader()
	args := map[string]interface{}{}

	_, err := UpscaleImageHandler(args, loader)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "image_path is required")
}

func TestUpscaleImageHandler_EmptyImagePath(t *testing.T) {
	loader := NewMockConfigLoader()
	args := map[string]interface{}{"image_path": ""}

	_, err := UpscaleImageHandler(args, loader)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "image_path is required")
}

func TestUpscaleImageRegistered(t *testing.T) {
	registry := NewRegistry()
	loader := NewMockConfigLoader()

	RegisterBuiltinSkills(registry, loader)

	skill, exists := registry.GetSkill("upscale_image")
	assert.True(t, exists, "upscale_image should be registered")
	assert.Equal(t, "upscale_image", skill.Name)
}
