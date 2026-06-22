// Package providers handles LLM provider capabilities and model management.
package providers

import "sort"

// ProviderCapabilities defines what a provider supports.
type ProviderCapabilities struct {
	Name                    string
	BaseURL                 string
	SupportsFunctionCalling bool
	SupportsModelListing    bool
	SupportsTokenTracking   bool // Returns usage data with stream_options
	DefaultModel            string
	PreferredToolModel      string // Best model for function calling
	RequiresAPIKey          bool
	IsOpenAICompatible      bool
	Notes                   string
}

// ModelInfo represents metadata about a model.
type ModelInfo struct {
	ID            string
	Name          string
	SupportsTools bool
	ContextWindow int
	Description   string
	Provider      string
}

// Registry holds all supported provider configurations.
// Ordered by priority: Popular/Tested → OpenAI-Compatible → Untested
var Registry = map[string]ProviderCapabilities{
	// --- Tier 1: Fully Tested & Supported ---

	"openai": {
		Name:                    "OpenAI",
		BaseURL:                 "https://api.openai.com/v1",
		SupportsFunctionCalling: true,
		SupportsModelListing:    true,
		SupportsTokenTracking:   true, // Full support via stream_options
		DefaultModel:            "gpt-4.1-nano",
		PreferredToolModel:      "gpt-4.1-nano",
		RequiresAPIKey:          true,
		IsOpenAICompatible:      true,
		Notes:                   "Native function calling support. Gold standard implementation.",
	},

	"grok": {
		Name:                    "xAI Grok",
		BaseURL:                 "https://api.x.ai/v1",
		SupportsFunctionCalling: true,
		SupportsModelListing:    true,
		SupportsTokenTracking:   true, // OpenAI-compatible token tracking
		DefaultModel:            "grok-4.20-0309-non-reasoning",
		PreferredToolModel:      "grok-4.20-0309-non-reasoning", // non-reasoning: reliable tool use, no reasoning burn, no grok-4.3 routing (#51)
		RequiresAPIKey:          true,
		IsOpenAICompatible:      true,
		Notes:                   "Default grok-4.20-0309-non-reasoning: reliable tool calling, no reasoning-token burn, never routes to the cost-prohibitive grok-4.3. Avoid grok-4-1-* (they route to grok-4.3).",
	},

	"venice": {
		Name:                    "Venice.ai",
		BaseURL:                 "https://api.venice.ai/api/v1",
		SupportsFunctionCalling: false, // venice-uncensored doesn't support it
		SupportsModelListing:    true,
		SupportsTokenTracking:   true, // Returns usage data
		DefaultModel:            "venice-uncensored",
		PreferredToolModel:      "", // No tool calling support in uncensored mode
		RequiresAPIKey:          true,
		IsOpenAICompatible:      true,
		Notes:                   "NSFW mode uses Venice. No function calling in uncensored mode. Image generation available.",
	},

	// --- Tier 2: OpenAI-Compatible (Needs Testing) ---

	"anthropic": {
		Name:                    "Anthropic Claude",
		BaseURL:                 "https://api.anthropic.com/v1",
		SupportsFunctionCalling: true,
		SupportsModelListing:    false, // Anthropic has fixed model list
		SupportsTokenTracking:   false, // Uses native API with different usage format
		DefaultModel:            "claude-sonnet-4-5-20250929",
		PreferredToolModel:      "claude-sonnet-4-5-20250929",
		RequiresAPIKey:          true,
		IsOpenAICompatible:      false, // Has compatibility layer but native API differs
		Notes:                   "Advanced tool use features. OpenAI SDK compatibility is for testing only. Native API recommended.",
	},

	"gemini": {
		Name:                    "Google Gemini AI (AI Studio)",
		BaseURL:                 "https://generativelanguage.googleapis.com/v1beta",
		SupportsFunctionCalling: true,
		SupportsModelListing:    false,
		SupportsTokenTracking:   true,
		DefaultModel:            "gemini-2.0-flash",
		PreferredToolModel:      "gemini-2.0-flash",
		RequiresAPIKey:          true,  // Simple API key from https://aistudio.google.com/apikey
		IsOpenAICompatible:      false, // Uses native Google GenAI SDK
		Notes:                   "RECOMMENDED: Native Google GenAI SDK with automatic authentication. Simple API keys (AIza...), free tier available. Full function calling support with streaming. Get key: https://aistudio.google.com/apikey",
	},

	"vertex": {
		Name:                    "Google Vertex AI (Cloud)",
		BaseURL:                 "https://aiplatform.googleapis.com/v1/projects/PROJECT_ID/locations/LOCATION",
		SupportsFunctionCalling: true,
		SupportsModelListing:    false,
		SupportsTokenTracking:   true,
		DefaultModel:            "gemini-2.0-flash",
		PreferredToolModel:      "gemini-2.0-flash",
		RequiresAPIKey:          false, // Uses ADC or service account - NO manual token needed!
		IsOpenAICompatible:      false, // Uses native Google GenAI SDK
		Notes:                   "ENTERPRISE: Native Google GenAI SDK with automatic authentication. No manual token refresh! Use: (1) gcloud auth application-default login OR (2) Service account JSON. Tokens auto-refresh indefinitely. Requires GCP project + billing.",
	},

	"openrouter": {
		Name:                    "OpenRouter",
		BaseURL:                 "https://openrouter.ai/api/v1",
		SupportsFunctionCalling: true,
		SupportsModelListing:    true,
		SupportsTokenTracking:   true, // OpenAI-compatible
		DefaultModel:            "openai/gpt-4.1-nano",
		PreferredToolModel:      "openai/gpt-4.1-nano",
		RequiresAPIKey:          true,
		IsOpenAICompatible:      true,
		Notes:                   "Aggregator for multiple providers. Full OpenAI compatibility. Parallel function calling supported.",
	},

	"sakana": {
		Name:                    "Sakana AI",
		BaseURL:                 "https://api.sakana.ai/v1",
		SupportsFunctionCalling: true,
		SupportsModelListing:    true, // exposes /v1/models
		SupportsTokenTracking:   true, // OpenAI-compatible
		DefaultModel:            "fugu",
		PreferredToolModel:      "fugu",
		RequiresAPIKey:          true,
		IsOpenAICompatible:      true,
		// Exposes /v1/chat/completions, /v1/responses, and /v1/models. We use chat
		// completions (the default OpenAI backend); Sakana recommends the Responses API
		// for best performance — that'd be a future backend, not required for support.
		// Reasoning effort is fixed server-side (default high; high/xhigh/max only),
		// so celeste's o-series reasoning_effort injection correctly skips Fugu.
		Notes: "Fugu / Fugu Ultra (1M context, deep reasoning). Fugu Ultra routes 1-3 expert agents. OpenAI-compatible chat completions. Install: curl -fsSL https://sakana.ai/fugu/install | bash",
	},

	// --- Tier 3: Limited or No Function Calling ---

	"digitalocean": {
		Name:                    "DigitalOcean Gradient",
		BaseURL:                 "",    // Agent-specific URL
		SupportsFunctionCalling: false, // Requires cloud-hosted functions
		SupportsModelListing:    false,
		SupportsTokenTracking:   true, // Returns usage data with stream_options.include_usage
		DefaultModel:            "gpt-4.1-nano",
		PreferredToolModel:      "",
		RequiresAPIKey:          true,
		IsOpenAICompatible:      true,
		Notes:                   "Agent API with RAG capabilities. Token tracking supported via stream_options. Function calling requires cloud-hosted functions.",
	},

	"elevenlabs": {
		Name:                    "ElevenLabs",
		BaseURL:                 "https://api.elevenlabs.io/v1",
		SupportsFunctionCalling: false, // Voice AI focused, unclear tool support
		SupportsModelListing:    false,
		SupportsTokenTracking:   false, // Voice-focused API, no token tracking
		DefaultModel:            "",
		PreferredToolModel:      "",
		RequiresAPIKey:          true,
		IsOpenAICompatible:      false,
		Notes:                   "Voice AI provider. Function calling support unknown.",
	},

	// --- Future Consideration (Not Implementing Yet) ---

	// AWS Bedrock - Too complex, requires AWS SDK
	// Azure OpenAI - Different auth model, enterprise-focused
	// GCP Model Garden - Vertex AI is sufficient for Google
}

// GetProvider returns provider capabilities by name.
func GetProvider(name string) (ProviderCapabilities, bool) {
	caps, ok := Registry[name]
	return caps, ok
}

// ListProviders returns all provider names.
func ListProviders() []string {
	providers := make([]string, 0, len(Registry))
	for name := range Registry {
		providers = append(providers, name)
	}
	sort.Strings(providers)
	return providers
}

// GetToolCallingProviders returns only providers that support function calling.
func GetToolCallingProviders() []string {
	var providers []string
	for name, caps := range Registry {
		if caps.SupportsFunctionCalling {
			providers = append(providers, name)
		}
	}
	sort.Strings(providers)
	return providers
}

// DetectProvider attempts to detect provider from base URL.
func DetectProvider(baseURL string) string {
	for name, caps := range Registry {
		if caps.BaseURL != "" && caps.BaseURL == baseURL {
			return name
		}
	}

	// Check partial matches
	switch {
	case contains(baseURL, "openai.com"):
		return "openai"
	case contains(baseURL, "x.ai"):
		return "grok"
	case contains(baseURL, "venice.ai"):
		return "venice"
	case contains(baseURL, "anthropic.com"):
		return "anthropic"
	case contains(baseURL, "generativelanguage.googleapis.com"):
		return "gemini"
	case contains(baseURL, "aiplatform.googleapis.com") || contains(baseURL, "vertexai"):
		return "vertex"
	case contains(baseURL, "openrouter.ai"):
		return "openrouter"
	case contains(baseURL, "sakana.ai"):
		return "sakana"
	case contains(baseURL, "digitalocean"):
		return "digitalocean"
	case contains(baseURL, "elevenlabs.io"):
		return "elevenlabs"
	default:
		return "unknown"
	}
}

// contains is a helper for string matching.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || stringContains(s, substr))
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ModelDetection provides heuristics for detecting model capabilities.
type ModelDetection struct {
	provider string
}

// NewModelDetection creates a model detection helper.
func NewModelDetection(provider string) *ModelDetection {
	return &ModelDetection{provider: provider}
}

// SupportsTools determines if a model supports function calling.
func (d *ModelDetection) SupportsTools(modelID string) bool {
	switch d.provider {
	case "openai":
		// All gpt-4* and gpt-3.5-turbo* support tools
		return contains(modelID, "gpt-4") || contains(modelID, "gpt-3.5-turbo")

	case "grok":
		// xAI text models support tools: grok-4.3, grok-4.20-* (reasoning /
		// non-reasoning / multi-agent), grok-build-0.1, grok-beta (docs.x.ai).
		// grok-imagine-* (image/video) and voice models do NOT — they don't match
		// "grok-4"/"grok-build" so they correctly return false here.
		return contains(modelID, "grok-build") || contains(modelID, "grok-4") || contains(modelID, "grok-beta")

	case "venice":
		// Prefer Venice's live catalog (model_spec.capabilities.supportsFunctionCalling).
		// The old name heuristic was wrong both ways (some *-uncensored models DO
		// support tools; some do not). Fall back to it only if the catalog is down.
		if supported, known := VeniceToolSupport(modelID); known {
			return supported
		}
		return !contains(modelID, "uncensored")

	case "anthropic":
		// All Claude 3+ models support tools
		return contains(modelID, "claude-3") || contains(modelID, "claude-4") || contains(modelID, "claude-sonnet")

	case "vertex":
		// Gemini 1.5+ supports function calling
		return contains(modelID, "gemini")

	case "sakana":
		// Fugu models support parallel tool calls (supports_parallel_tool_calls).
		return contains(modelID, "fugu")

	case "openrouter":
		// Prefer OpenRouter's live catalog (authoritative per-model capability:
		// supported_parameters includes "tools"). Falls through to a name
		// heuristic only when the catalog is unreachable.
		if supported, known := OpenRouterToolSupport(modelID); known {
			return supported
		}
		return contains(modelID, "gpt-") || contains(modelID, "claude-") || contains(modelID, "gemini-")

	default:
		return false
	}
}

// GetDefaultToolModel returns the best model for tool calling.
func (d *ModelDetection) GetDefaultToolModel() string {
	caps, ok := Registry[d.provider]
	if !ok {
		return ""
	}
	return caps.PreferredToolModel
}
