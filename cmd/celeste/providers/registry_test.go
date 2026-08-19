package providers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProviderRegistryExists verifies the registry is populated
func TestProviderRegistryExists(t *testing.T) {
	assert.NotNil(t, Registry, "Registry should not be nil")
	assert.NotEmpty(t, Registry, "Registry should contain providers")
}

// TestProviderCount verifies we have all expected providers
func TestProviderCount(t *testing.T) {
	expectedProviders := []string{
		"openai", "grok", "venice",
		"anthropic", "gemini", "vertex",
		"openrouter", "digitalocean", "elevenlabs",
		"sakana", "local",
	}

	assert.Equal(t, len(expectedProviders), len(Registry),
		"Registry should contain exactly %d providers", len(expectedProviders))

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
	assert.Equal(t, 11, len(providers), "Should return all 11 providers")
	assert.Equal(t, []string{
		"anthropic",
		"digitalocean",
		"elevenlabs",
		"gemini",
		"grok",
		"local",
		"openai",
		"openrouter",
		"sakana",
		"venice",
		"vertex",
	}, providers, "Provider list should be deterministic and sorted")

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
	assert.Equal(t, []string{
		"anthropic",
		"gemini",
		"grok",
		"local",
		"openai",
		"openrouter",
		"sakana",
		"vertex",
	}, toolProviders, "Tool provider list should be deterministic and sorted")

	// Verify all returned providers actually support function calling
	for _, name := range toolProviders {
		caps, ok := GetProvider(name)
		require.True(t, ok, "Provider %s should exist", name)
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
			name:     "Sakana URL",
			baseURL:  "https://api.sakana.ai/v1",
			expected: "sakana",
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

			// Most providers should have a base URL (except special cases like
			// DigitalOcean, and "local", whose URL is whatever host and port the
			// user's server listens on — an empty value here is what keeps
			// detection port-independent).
			if name != "digitalocean" && name != "local" {
				assert.NotEmpty(t, caps.BaseURL, "Provider should have a base URL")
			}

			// If provider supports function calling, it should have a preferred tool
			// model. "local" is exempt: the model name is whatever the user's
			// server expects, and mlx-vlm wants a full filesystem path.
			if caps.SupportsFunctionCalling && name != "local" {
				assert.NotEmpty(t, caps.PreferredToolModel,
					"Tool-capable provider should have a preferred tool model")
			}

			// Most providers should have a default model (except voice APIs like
			// ElevenLabs, and "local"). Inventing a default for a local server
			// would be actively harmful: mlx-vlm treats an unrecognised model
			// name as a HuggingFace repo id and 404s.
			if name != "elevenlabs" && name != "local" {
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
	assert.Contains(t, caps.Notes, "grok-4.20-0309-non-reasoning", "Grok notes should mention the default model")
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
		{"grok", "grok-4.20-0309-non-reasoning"},
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

// A local OpenAI-compatible server (mlx-vlm, Ollama, LM Studio, llama.cpp) used
// to detect as "unknown", which meant GetProvider returned ok=false, skills
// never enabled, and celeste sent no `tools` array at all. The model then
// improvised an unparseable text <tool_call> block. These pin the detection.
func TestDetectProvider_LocalEndpoints(t *testing.T) {
	for _, tc := range []struct{ name, baseURL string }{
		{"mlx-vlm loopback IP", "http://127.0.0.1:8080/v1"},
		{"Ollama localhost", "http://localhost:11434/v1"},
		{"LM Studio localhost", "http://localhost:1234/v1"},
		{"bind-all address", "http://0.0.0.0:8080/v1"},
		{"IPv6 loopback", "http://[::1]:8080/v1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, "local", DetectProvider(tc.baseURL))
		})
	}
}

// The local branch sits last in the switch, so it must not have stolen routing
// from any hosted provider, and must not swallow the two "unknown" cases the
// existing suite depends on. The empty string is the one to watch: it contains
// no host substring, so it has to keep falling through.
func TestDetectProvider_LocalBranchStealsNothing(t *testing.T) {
	for _, tc := range []struct{ baseURL, want string }{
		{"https://api.openai.com/v1", "openai"},
		{"https://openai.com/some/path", "openai"},
		{"https://api.x.ai/v1", "grok"},
		{"https://api.sakana.ai/v1", "sakana"},
		{"https://generativelanguage.googleapis.com/v1beta", "gemini"},
		{"https://example.com/api", "unknown"},
		{"", "unknown"},
	} {
		assert.Equal(t, tc.want, DetectProvider(tc.baseURL),
			"routing changed for %q", tc.baseURL)
	}
}

// SupportsTools defaults to false, so the registry entry and the detection
// branch are not enough on their own — this is the third of the three changes
// and the easiest to leave out.
func TestSupportsTools_Local(t *testing.T) {
	d := NewModelDetection("local")
	// A local model name is arbitrary; no heuristic applies, so every name is
	// tool-capable.
	assert.True(t, d.SupportsTools("/Users/me/models/qwen3.8-27b/4-bit"))
	assert.True(t, d.SupportsTools("llama3.2"))
	assert.True(t, d.SupportsTools(""))

	// And the hosted heuristics are untouched.
	assert.True(t, NewModelDetection("openai").SupportsTools("gpt-4.1-nano"))
	assert.False(t, NewModelDetection("openai").SupportsTools("text-embedding-3-small"))
}

// A local server's /v1/models advertises only its embedding model, so a picker
// built on that listing would show the wrong thing.
func TestLocalProvider_Capabilities(t *testing.T) {
	caps, ok := GetProvider("local")
	require.True(t, ok, "local provider must be registered")
	assert.True(t, caps.SupportsFunctionCalling)
	assert.False(t, caps.SupportsModelListing, "local /v1/models does not list the loaded chat model")
	assert.False(t, caps.RequiresAPIKey)
	assert.True(t, caps.IsOpenAICompatible)
	assert.Empty(t, caps.BaseURL, "must stay empty so detection is port-independent")
}

// The TUI enables skills via `if caps, ok := GetProvider(provider); ok { ... }`
// (tui/app.go:3155 and :1377). Before "local" existed, ok was false for any
// localhost URL, so skillsEnabled kept its zero value and the whole tool
// surface stayed dark. This pins the three properties that path depends on.
func TestLocalProvider_DrivesTUISkillGate(t *testing.T) {
	provider := DetectProvider("http://127.0.0.1:8080/v1")
	caps, ok := GetProvider(provider)

	require.True(t, ok, "GetProvider must succeed or the TUI leaves skills disabled")
	assert.True(t, caps.SupportsFunctionCalling, "drives skillsEnabled")

	// The same block auto-selects caps.PreferredToolModel when the model is
	// unset. It must stay empty for local: overwriting a user's model would
	// break mlx-vlm, which needs the full filesystem path and 404s on anything
	// it cannot resolve as a HuggingFace repo id.
	assert.Empty(t, caps.PreferredToolModel,
		"must not auto-select a model for a local server")
}
