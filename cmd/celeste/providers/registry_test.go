package providers

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestProviderRegistryExists verifies the registry is populated
func TestProviderRegistryExists(t *testing.T) {
	assert.NotNil(t, Registry, "Registry should not be nil")
	assert.NotEmpty(t, Registry, "Registry should contain providers")
}

// TestProviderCount verifies we have all 9 expected providers
func TestProviderCount(t *testing.T) {
	expectedProviders := []string{
		"openai", "grok", "venice",
		"anthropic", "gemini", "vertex",
		"openrouter", "digitalocean", "elevenlabs",
	}

	assert.Equal(t, len(expectedProviders), len(Registry),
		"Registry should contain exactly 9 providers")

	for _, name := range expectedProviders {
		_, exists := Registry[name]
		assert.True(t, exists, "Provider '%s' should exist in registry", name)
	}
}

// TestGetProvider tests retrieving individual providers
func TestGetProvider(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		wantOk   bool
	}{
		{"OpenAI exists", "openai", true},
		{"Grok exists", "grok", true},
		{"Venice exists", "venice", true},
		{"Anthropic exists", "anthropic", true},
		{"Gemini exists", "gemini", true},
		{"Unknown provider", "unknown", false},
		{"Empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			caps, ok := GetProvider(tt.provider)
			assert.Equal(t, tt.wantOk, ok, "GetProvider should return correct existence")

			if tt.wantOk {
				assert.NotNil(t, caps, "Capabilities should not be nil for existing provider")
				assert.NotEmpty(t, caps.Name, "Provider should have a name")
			}
		})
	}
}

// TestListProviders tests the provider listing function
func TestListProviders(t *testing.T) {
	providers := ListProviders()

	assert.NotEmpty(t, providers, "ListProviders should return providers")
	assert.Equal(t, 9, len(providers), "Should return all 9 providers")

	// Verify all expected providers are in the list
	providerMap := make(map[string]bool)
	for _, p := range providers {
		providerMap[p] = true
	}

	assert.True(t, providerMap["openai"], "List should include openai")
	assert.True(t, providerMap["grok"], "List should include grok")
	assert.True(t, providerMap["venice"], "List should include venice")
	assert.True(t, providerMap["anthropic"], "List should include anthropic")
	assert.True(t, providerMap["gemini"], "List should include gemini")
}

// TestGetToolCallingProviders tests filtering tool-capable providers
func TestGetToolCallingProviders(t *testing.T) {
	toolProviders := GetToolCallingProviders()

	assert.NotEmpty(t, toolProviders, "Should return at least one tool-capable provider")

	// Verify all returned providers actually support function calling
	for _, name := range toolProviders {
		caps, ok := GetProvider(name)
		assert.True(t, ok, "Provider %s should exist", name)
		assert.True(t, caps.SupportsFunctionCalling,
			"Provider %s should support function calling", name)
	}

	// Verify known tool providers are included
	toolProviderMap := make(map[string]bool)
	for _, p := range toolProviders {
		toolProviderMap[p] = true
	}

	assert.True(t, toolProviderMap["openai"], "OpenAI should support tools")
	assert.True(t, toolProviderMap["grok"], "Grok should support tools")
	assert.False(t, toolProviderMap["venice"], "Venice should not support tools")
}

// TestDetectProvider tests provider detection from URLs
func TestDetectProvider(t *testing.T) {
	tests := []struct {
		name     string
		baseURL  string
		expected string
	}{
		{
			name:     "OpenAI URL",
			baseURL:  "https://api.openai.com/v1",
			expected: "openai",
		},
		{
			name:     "Grok URL",
			baseURL:  "https://api.x.ai/v1",
			expected: "grok",
		},
		{
			name:     "Venice URL",
			baseURL:  "https://api.venice.ai/api/v1",
			expected: "venice",
		},
		{
			name:     "Anthropic URL",
			baseURL:  "https://api.anthropic.com/v1",
			expected: "anthropic",
		},
		{
			name:     "Gemini URL",
			baseURL:  "https://generativelanguage.googleapis.com/v1beta/openai",
			expected: "gemini",
		},
		{
			name:     "Partial OpenAI match",
			baseURL:  "https://openai.com/some/path",
			expected: "openai",
		},
		{
			name:     "Unknown URL",
			baseURL:  "https://example.com/api",
			expected: "unknown",
		},
		{
			name:     "Empty URL",
			baseURL:  "",
			expected: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetectProvider(tt.baseURL)
			assert.Equal(t, tt.expected, result,
				"DetectProvider should correctly identify provider from URL")
		})
	}
}

// TestProviderCapabilities tests that all providers have valid configurations
func TestProviderCapabilities(t *testing.T) {
	for name, caps := range Registry {
		t.Run(name, func(t *testing.T) {
			// Every provider should have a name
			assert.NotEmpty(t, caps.Name, "Provider should have a display name")

			// Most providers should have a base URL (except special cases like DigitalOcean)
			if name != "digitalocean" {
				assert.NotEmpty(t, caps.BaseURL, "Provider should have a base URL")
			}

			// If provider supports function calling, it should have a preferred tool model
			if caps.SupportsFunctionCalling {
				assert.NotEmpty(t, caps.PreferredToolModel,
					"Tool-capable provider should have a preferred tool model")
			}

			// Most providers should have a default model (except voice APIs like ElevenLabs)
			if name != "elevenlabs" {
				assert.NotEmpty(t, caps.DefaultModel, "Provider should have a default model")
			}

			// Verify OpenAI compatibility flag is set correctly
			if name == "openai" || name == "grok" || name == "venice" {
				assert.True(t, caps.IsOpenAICompatible,
					"%s should be OpenAI compatible", name)
			}
		})
	}
}

// TestOpenAIProvider specifically tests the OpenAI provider (gold standard)
func TestOpenAIProvider(t *testing.T) {
	caps, ok := GetProvider("openai")
	assert.True(t, ok, "OpenAI provider should exist")

	assert.Equal(t, "OpenAI", caps.Name)
	assert.Equal(t, "https://api.openai.com/v1", caps.BaseURL)
	assert.True(t, caps.SupportsFunctionCalling)
	assert.True(t, caps.SupportsModelListing)
	assert.True(t, caps.SupportsTokenTracking)
	assert.True(t, caps.IsOpenAICompatible)
	assert.True(t, caps.RequiresAPIKey)
	assert.NotEmpty(t, caps.DefaultModel)
	assert.NotEmpty(t, caps.PreferredToolModel)
}

// TestGrokProvider specifically tests the Grok provider
func TestGrokProvider(t *testing.T) {
	caps, ok := GetProvider("grok")
	assert.True(t, ok, "Grok provider should exist")

	assert.Equal(t, "xAI Grok", caps.Name)
	assert.Equal(t, "https://api.x.ai/v1", caps.BaseURL)
	assert.True(t, caps.SupportsFunctionCalling)
	assert.True(t, caps.SupportsModelListing)
	assert.True(t, caps.SupportsTokenTracking)
	assert.True(t, caps.IsOpenAICompatible)
	assert.Contains(t, caps.Notes, "2M context", "Grok should mention 2M context")
}

// TestVeniceProvider specifically tests the Venice provider
func TestVeniceProvider(t *testing.T) {
	caps, ok := GetProvider("venice")
	assert.True(t, ok, "Venice provider should exist")

	assert.Equal(t, "Venice.ai", caps.Name)
	assert.False(t, caps.SupportsFunctionCalling, "Venice uncensored should not support function calling")
	assert.True(t, caps.SupportsModelListing)
	assert.True(t, caps.IsOpenAICompatible)
	assert.Empty(t, caps.PreferredToolModel, "Venice should have no tool model")
}

// TestAnthropicProvider tests the Anthropic provider configuration
func TestAnthropicProvider(t *testing.T) {
	caps, ok := GetProvider("anthropic")
	assert.True(t, ok, "Anthropic provider should exist")

	assert.Equal(t, "Anthropic Claude", caps.Name)
	assert.True(t, caps.SupportsFunctionCalling)
	assert.False(t, caps.SupportsModelListing, "Anthropic has fixed model list")
	assert.NotEmpty(t, caps.PreferredToolModel)
}

// TestGeminiProvider tests the Gemini provider configuration
func TestGeminiProvider(t *testing.T) {
	caps, ok := GetProvider("gemini")
	assert.True(t, ok, "Gemini provider should exist")

	assert.Equal(t, "Google Gemini AI (AI Studio)", caps.Name)
	assert.True(t, caps.SupportsFunctionCalling)
	assert.False(t, caps.IsOpenAICompatible, "Gemini uses native Google GenAI SDK")
	assert.Contains(t, caps.BaseURL, "generativelanguage.googleapis.com")
	assert.Contains(t, caps.Notes, "aistudio.google.com", "Should mention AI Studio")
}

// TestProviderNotes verifies important notes are documented
func TestProviderNotes(t *testing.T) {
	tests := []struct {
		provider      string
		shouldContain string
	}{
		{"openai", "Gold standard"},
		{"grok", "2M context"},
		{"venice", "NSFW"},
		{"anthropic", "Native API"},
		{"gemini", "aistudio.google.com"},
	}

	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			caps, ok := GetProvider(tt.provider)
			assert.True(t, ok, "Provider should exist")
			assert.Contains(t, caps.Notes, tt.shouldContain,
				"Provider notes should contain important information")
		})
	}
}
