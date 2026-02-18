package providers

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNewModelService verifies ModelService creation
func TestNewModelService(t *testing.T) {
	tests := []struct {
		name     string
		apiKey   string
		baseURL  string
		provider string
	}{
		{"OpenAI service", "test-key", "https://api.openai.com/v1", "openai"},
		{"Grok service", "test-key", "https://api.x.ai/v1", "grok"},
		{"Venice service", "test-key", "https://api.venice.ai/api/v1", "venice"},
		{"Empty base URL", "test-key", "", "openai"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewModelService(tt.apiKey, tt.baseURL, tt.provider)
			assert.NotNil(t, service, "Service should not be nil")
			assert.NotNil(t, service.client, "Client should be initialized")
			assert.Equal(t, tt.provider, service.provider, "Provider should match")
			assert.NotNil(t, service.detector, "Detector should be initialized")
		})
	}
}

// TestGetStaticModels tests static model lists for all providers
func TestGetStaticModels(t *testing.T) {
	tests := []struct {
		provider      string
		expectedCount int
		hasToolModels bool
		checkModelID  string // Specific model to verify
	}{
		{"grok", 4, true, "grok-4-1-fast"},
		{"openai", 4, true, "gpt-4o-mini"},
		{"venice", 3, true, "llama-3.3-70b"},
		{"anthropic", 2, true, "claude-sonnet-4-5-20250929"},
		{"vertex", 2, true, "gemini-1.5-pro"},
		{"openrouter", 2, true, "openai/gpt-4o-mini"},
		{"digitalocean", 1, false, "gpt-4o-mini"},
		{"unknown", 0, false, ""}, // Unknown provider should return empty
	}

	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			service := NewModelService("test-key", "", tt.provider)
			models := service.getStaticModels()

			assert.Equal(t, tt.expectedCount, len(models),
				"Provider %s should have %d models", tt.provider, tt.expectedCount)

			if tt.expectedCount > 0 {
				// Verify specific model exists
				found := false
				for _, m := range models {
					if m.ID == tt.checkModelID {
						found = true
						assert.Equal(t, tt.provider, m.Provider, "Provider should match")
						assert.NotEmpty(t, m.Name, "Model should have display name")
					}
				}
				assert.True(t, found, "Should find model %s", tt.checkModelID)

				// Verify tool support if expected
				if tt.hasToolModels {
					hasTools := false
					for _, m := range models {
						if m.SupportsTools {
							hasTools = true
							break
						}
					}
					assert.True(t, hasTools, "Provider should have at least one tool-capable model")
				}
			}
		})
	}
}

// TestGetBestToolModel verifies default tool model selection
func TestGetBestToolModel(t *testing.T) {
	tests := []struct {
		provider      string
		expectedModel string
	}{
		{"openai", "gpt-4o-mini"},
		{"grok", "grok-4-1-fast"},
		{"venice", ""}, // Venice uncensored has no tool model
		{"anthropic", "claude-sonnet-4-5-20250929"},
		{"gemini", "gemini-2.0-flash"},
		{"vertex", "gemini-2.0-flash"},
		{"openrouter", "openai/gpt-4o-mini"},
		{"digitalocean", ""}, // No preferred tool model
		{"unknown", ""},      // Unknown provider
	}

	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			service := NewModelService("test-key", "", tt.provider)
			model := service.GetBestToolModel()
			assert.Equal(t, tt.expectedModel, model,
				"Provider %s should recommend %s", tt.provider, tt.expectedModel)
		})
	}
}

// TestModelDetection tests the SupportsTools heuristic
func TestModelDetection(t *testing.T) {
	tests := []struct {
		provider     string
		modelID      string
		expectsTools bool
	}{
		// OpenAI
		{"openai", "gpt-4o-mini", true},
		{"openai", "gpt-4-turbo", true},
		{"openai", "gpt-3.5-turbo", true},
		{"openai", "davinci-002", false}, // Old model

		// Grok
		{"grok", "grok-4-1-fast", true},
		{"grok", "grok-4-1", true},
		{"grok", "grok-beta", true},
		{"grok", "grok-4-latest", true}, // Contains grok-4

		// Venice
		{"venice", "llama-3.3-70b", true},
		{"venice", "venice-uncensored", false}, // Explicitly no tools
		{"venice", "qwen3-235b", true},

		// Anthropic
		{"anthropic", "claude-sonnet-4-5-20250929", true},
		{"anthropic", "claude-3-opus", true},
		{"anthropic", "claude-2", false}, // Old version

		// Vertex
		{"vertex", "gemini-1.5-pro", true},
		{"vertex", "gemini-1.5-flash", true},

		// OpenRouter
		{"openrouter", "openai/gpt-4o-mini", true},
		{"openrouter", "anthropic/claude-sonnet-4-5", true},
		{"openrouter", "meta-llama/llama-2-70b", false}, // Old model

		// Unknown provider
		{"unknown", "any-model", false},
	}

	for _, tt := range tests {
		t.Run(tt.provider+"_"+tt.modelID, func(t *testing.T) {
			detector := NewModelDetection(tt.provider)
			result := detector.SupportsTools(tt.modelID)
			assert.Equal(t, tt.expectsTools, result,
				"Model %s on %s should have tools=%v", tt.modelID, tt.provider, tt.expectsTools)
		})
	}
}

// TestGetDefaultToolModel tests detector's default model retrieval
func TestGetDefaultToolModel(t *testing.T) {
	providers := []string{"openai", "grok", "venice", "anthropic", "gemini"}

	for _, provider := range providers {
		t.Run(provider, func(t *testing.T) {
			detector := NewModelDetection(provider)
			model := detector.GetDefaultToolModel()

			// Should match PreferredToolModel from registry
			caps, ok := Registry[provider]
			assert.True(t, ok, "Provider should exist in registry")
			assert.Equal(t, caps.PreferredToolModel, model,
				"Default tool model should match registry")
		})
	}

	// Test unknown provider
	t.Run("unknown", func(t *testing.T) {
		detector := NewModelDetection("unknown")
		model := detector.GetDefaultToolModel()
		assert.Empty(t, model, "Unknown provider should return empty string")
	})
}

// TestGetModelDisplayName tests name formatting
func TestGetModelDisplayName(t *testing.T) {
	tests := []struct {
		provider string
		modelID  string
		expected string
	}{
		{"openai", "gpt-4o-mini", "Gpt 4O Mini"},
		{"openai", "gpt-3.5-turbo", "Gpt 3.5 Turbo"},
		{"grok", "grok-4-1-fast", "Grok 4 1 Fast"},
		{"openrouter", "openai/gpt-4o-mini", "Gpt 4O Mini"}, // Removes prefix
		{"anthropic", "anthropic/claude-sonnet-4-5", "Claude Sonnet 4 5"},
		{"venice", "venice-uncensored", "Venice Uncensored"},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			service := NewModelService("test-key", "", tt.provider)
			name := service.getModelDisplayName(tt.modelID)
			assert.Equal(t, tt.expected, name, "Display name should be formatted")
		})
	}
}

// TestGetModelDescription tests description generation
func TestGetModelDescription(t *testing.T) {
	tests := []struct {
		provider    string
		modelID     string
		expectMatch string // Substring to match
	}{
		{"openai", "gpt-4o-mini", "Fast, affordable"},
		{"openai", "gpt-4-turbo", "Previous flagship"},
		{"grok", "grok-4-1-fast", "Best for tool calling"},
		{"anthropic", "claude-opus-4-5-20251101", "Most capable"},
		{"anthropic", "claude-sonnet-4-5-20250929", "advanced tool use"},
		{"venice", "venice-uncensored", "NSFW uncensored"},
		{"unknown", "random-model", "Available model"}, // Fallback
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			service := NewModelService("test-key", "", tt.provider)
			desc := service.getModelDescription(tt.modelID)
			assert.Contains(t, desc, tt.expectMatch,
				"Description for %s should contain '%s'", tt.modelID, tt.expectMatch)
		})
	}
}

// TestSortModelsByCapability tests model sorting
func TestSortModelsByCapability(t *testing.T) {
	models := []ModelInfo{
		{ID: "no-tools-1", SupportsTools: false},
		{ID: "with-tools-1", SupportsTools: true},
		{ID: "no-tools-2", SupportsTools: false},
		{ID: "with-tools-2", SupportsTools: true},
	}

	sortModelsByCapability(models)

	// Tool models should come first
	assert.True(t, models[0].SupportsTools, "First model should support tools")
	assert.True(t, models[1].SupportsTools, "Second model should support tools")
	assert.False(t, models[2].SupportsTools, "Third model should not support tools")
	assert.False(t, models[3].SupportsTools, "Fourth model should not support tools")
}

// TestFormatModelList tests model list formatting
func TestFormatModelList(t *testing.T) {
	models := []ModelInfo{
		{
			ID:            "gpt-4o-mini",
			SupportsTools: true,
			Description:   "Fast and affordable",
			ContextWindow: 128000,
		},
		{
			ID:            "venice-uncensored",
			SupportsTools: false,
			Description:   "NSFW model",
			ContextWindow: 0,
		},
	}

	t.Run("with highlighting", func(t *testing.T) {
		output := FormatModelList(models, true)
		assert.Contains(t, output, "Function Calling Enabled", "Should show section header")
		assert.Contains(t, output, "✓", "Should have checkmark for tool models")
		assert.Contains(t, output, "gpt-4o-mini", "Should include model ID")
		assert.Contains(t, output, "128k context", "Should show context window")
		assert.Contains(t, output, "no skills", "Should mark non-tool models")
	})

	t.Run("without highlighting", func(t *testing.T) {
		output := FormatModelList(models, false)
		assert.Contains(t, output, "gpt-4o-mini", "Should include model ID")
		assert.Contains(t, output, "venice-uncensored", "Should include all models")
		assert.NotContains(t, output, "Function Calling Enabled", "Should not have section headers")
	})
}

// TestStaticModelConsistency verifies all static models are properly configured
func TestStaticModelConsistency(t *testing.T) {
	providers := []string{"openai", "grok", "venice", "anthropic", "vertex", "openrouter", "digitalocean"}

	for _, provider := range providers {
		t.Run(provider, func(t *testing.T) {
			service := NewModelService("test-key", "", provider)
			models := service.getStaticModels()

			assert.NotEmpty(t, models, "Provider should have static models")

			for _, model := range models {
				// Every model should have basic fields
				assert.NotEmpty(t, model.ID, "Model should have ID")
				assert.NotEmpty(t, model.Name, "Model should have display name")
				assert.Equal(t, provider, model.Provider, "Provider should match")

				// If SupportsTools, should be documented
				if model.SupportsTools {
					assert.NotEmpty(t, model.Description, "Tool model should have description")
				}
			}
		})
	}
}

// TestGrokStaticModels specifically tests Grok models
func TestGrokStaticModels(t *testing.T) {
	service := NewModelService("test-key", "", "grok")
	models := service.getStaticModels()

	// Find grok-4-1-fast (recommended tool model)
	var fastModel *ModelInfo
	for i := range models {
		if models[i].ID == "grok-4-1-fast" {
			fastModel = &models[i]
			break
		}
	}

	assert.NotNil(t, fastModel, "Should have grok-4-1-fast model")
	assert.True(t, fastModel.SupportsTools, "grok-4-1-fast should support tools")
	assert.Equal(t, 2000000, fastModel.ContextWindow, "Should have 2M context")
	assert.Contains(t, fastModel.Description, "2M context", "Should mention context in description")
}

// TestOpenAIStaticModels specifically tests OpenAI models
func TestOpenAIStaticModels(t *testing.T) {
	service := NewModelService("test-key", "", "openai")
	models := service.getStaticModels()

	assert.Equal(t, 4, len(models), "Should have 4 OpenAI models")

	// All OpenAI models should support tools
	for _, model := range models {
		assert.True(t, model.SupportsTools, "All OpenAI models should support tools: %s", model.ID)
	}

	// Verify gpt-4o-mini exists (default)
	found := false
	for _, model := range models {
		if model.ID == "gpt-4o-mini" {
			found = true
			assert.Equal(t, 128000, model.ContextWindow, "Should have 128k context")
		}
	}
	assert.True(t, found, "Should have gpt-4o-mini")
}

// TestVeniceStaticModels specifically tests Venice models
func TestVeniceStaticModels(t *testing.T) {
	service := NewModelService("test-key", "", "venice")
	models := service.getStaticModels()

	// Find venice-uncensored
	var uncensored *ModelInfo
	for i := range models {
		if models[i].ID == "venice-uncensored" {
			uncensored = &models[i]
			break
		}
	}

	assert.NotNil(t, uncensored, "Should have venice-uncensored model")
	assert.False(t, uncensored.SupportsTools, "venice-uncensored should NOT support tools")
	assert.Contains(t, uncensored.Description, "NSFW", "Should mention NSFW")
}

// TestAnthropicStaticModels specifically tests Anthropic models
func TestAnthropicStaticModels(t *testing.T) {
	service := NewModelService("test-key", "", "anthropic")
	models := service.getStaticModels()

	assert.Equal(t, 2, len(models), "Should have 2 Claude models")

	// All should support tools
	for _, model := range models {
		assert.True(t, model.SupportsTools, "All Claude models should support tools")
		assert.Equal(t, 200000, model.ContextWindow, "Should have 200k context")
	}
}

// TestDigitalOceanStaticModels tests DigitalOcean special case
func TestDigitalOceanStaticModels(t *testing.T) {
	service := NewModelService("test-key", "", "digitalocean")
	models := service.getStaticModels()

	assert.Equal(t, 1, len(models), "Should have 1 DigitalOcean model")
	assert.Equal(t, "gpt-4o-mini", models[0].ID, "Should be gpt-4o-mini")
	assert.False(t, models[0].SupportsTools, "DigitalOcean should not support local skills")
	assert.Contains(t, models[0].Description, "no local skills", "Should mention no local skills")
}
