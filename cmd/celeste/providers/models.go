// Package providers handles LLM provider model listing and management.
package providers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sashabaranov/go-openai"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// ModelService handles model listing and metadata.
type ModelService struct {
	client   *openai.Client
	provider string
	detector *ModelDetection
}

// NewModelService creates a new model service for a provider.
func NewModelService(apiKey, baseURL, provider string) *ModelService {
	config := openai.DefaultConfig(apiKey)
	if baseURL != "" {
		config.BaseURL = baseURL
	}

	return &ModelService{
		client:   openai.NewClientWithConfig(config),
		provider: provider,
		detector: NewModelDetection(provider),
	}
}

// ListModels fetches available models from the provider API.
// Returns error if provider doesn't support listing or API fails.
func (s *ModelService) ListModels(ctx context.Context) ([]ModelInfo, error) {
	caps, ok := Registry[s.provider]
	if !ok {
		return nil, fmt.Errorf("unknown provider: %s", s.provider)
	}

	if !caps.SupportsModelListing {
		// Provider doesn't support dynamic listing, return static models
		return s.getStaticModels(), nil
	}

	// Add timeout to context
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Call OpenAI-compatible /v1/models endpoint
	models, err := s.client.ListModels(ctx)
	if err != nil {
		// Fallback to static models if API fails
		return s.getStaticModels(), fmt.Errorf("API call failed, using static models: %w", err)
	}

	// Convert to our ModelInfo structure
	var result []ModelInfo
	for _, m := range models.Models {
		info := ModelInfo{
			ID:            m.ID,
			Name:          s.getModelDisplayName(m.ID),
			Provider:      s.provider,
			SupportsTools: s.detector.SupportsTools(m.ID),
			Description:   s.getModelDescription(m.ID),
		}
		result = append(result, info)
	}

	// Sort: Tool-capable models first
	sortModelsByCapability(result)

	return result, nil
}

// GetBestToolModel returns the recommended model for function calling.
func (s *ModelService) GetBestToolModel() string {
	return s.detector.GetDefaultToolModel()
}

// ValidateModel checks if a model exists and returns its capabilities.
func (s *ModelService) ValidateModel(ctx context.Context, modelID string) (ModelInfo, error) {
	models, err := s.ListModels(ctx)
	if err != nil {
		// If listing fails, do basic validation
		return ModelInfo{
			ID:            modelID,
			Name:          modelID,
			Provider:      s.provider,
			SupportsTools: s.detector.SupportsTools(modelID),
			Description:   "Model validation unavailable",
		}, nil
	}

	// Find model in list
	for _, m := range models {
		if m.ID == modelID {
			return m, nil
		}
	}

	return ModelInfo{}, fmt.Errorf("model %s not found for provider %s", modelID, s.provider)
}

// getStaticModels returns hardcoded model list when API isn't available.
func (s *ModelService) getStaticModels() []ModelInfo {
	switch s.provider {
	case "grok":
		return []ModelInfo{
			{
				ID:            "grok-4-1-fast",
				Name:          "Grok 4.1 Fast",
				Provider:      "grok",
				SupportsTools: true,
				ContextWindow: 2000000, // 2M tokens
				Description:   "Best for tool calling (2M context, optimized for agentic tasks)",
			},
			{
				ID:            "grok-4-1",
				Name:          "Grok 4.1",
				Provider:      "grok",
				SupportsTools: true,
				ContextWindow: 131072,
				Description:   "High-quality reasoning with tool support",
			},
			{
				ID:            "grok-beta",
				Name:          "Grok Beta",
				Provider:      "grok",
				SupportsTools: true,
				ContextWindow: 131072,
				Description:   "Beta version with tool calling",
			},
			{
				ID:            "grok-4-latest",
				Name:          "Grok 4 Latest",
				Provider:      "grok",
				SupportsTools: false, // Not optimized for tools
				ContextWindow: 131072,
				Description:   "Latest general model (limited tool support)",
			},
		}

	case "openai":
		return []ModelInfo{
			{
				ID:            "gpt-4o-mini",
				Name:          "GPT-4o Mini",
				Provider:      "openai",
				SupportsTools: true,
				ContextWindow: 128000,
				Description:   "Fast, affordable, smart for everyday tasks",
			},
			{
				ID:            "gpt-4o",
				Name:          "GPT-4o",
				Provider:      "openai",
				SupportsTools: true,
				ContextWindow: 128000,
				Description:   "High intelligence flagship model",
			},
			{
				ID:            "gpt-4-turbo",
				Name:          "GPT-4 Turbo",
				Provider:      "openai",
				SupportsTools: true,
				ContextWindow: 128000,
				Description:   "Previous flagship with vision and tools",
			},
			{
				ID:            "gpt-3.5-turbo",
				Name:          "GPT-3.5 Turbo",
				Provider:      "openai",
				SupportsTools: true,
				ContextWindow: 16385,
				Description:   "Fast and affordable legacy model",
			},
		}

	case "venice":
		return []ModelInfo{
			{
				ID:            "venice-uncensored",
				Name:          "Venice Uncensored",
				Provider:      "venice",
				SupportsTools: false,
				Description:   "NSFW uncensored chat (no function calling)",
			},
			{
				ID:            "llama-3.3-70b",
				Name:          "Llama 3.3 70B",
				Provider:      "venice",
				SupportsTools: true,
				Description:   "Open source model with tool support",
			},
			{
				ID:            "qwen3-235b",
				Name:          "Qwen 3 235B",
				Provider:      "venice",
				SupportsTools: true,
				Description:   "Large open model with function calling",
			},
		}

	case "anthropic":
		return []ModelInfo{
			{
				ID:            "claude-sonnet-4-5-20250929",
				Name:          "Claude Sonnet 4.5",
				Provider:      "anthropic",
				SupportsTools: true,
				ContextWindow: 200000,
				Description:   "Latest Sonnet with advanced tool use",
			},
			{
				ID:            "claude-opus-4-5-20251101",
				Name:          "Claude Opus 4.5",
				Provider:      "anthropic",
				SupportsTools: true,
				ContextWindow: 200000,
				Description:   "Most capable Claude model",
			},
		}

	case "vertex":
		return []ModelInfo{
			{
				ID:            "gemini-1.5-pro",
				Name:          "Gemini 1.5 Pro",
				Provider:      "vertex",
				SupportsTools: true,
				ContextWindow: 2000000,
				Description:   "Google's flagship with function calling",
			},
			{
				ID:            "gemini-1.5-flash",
				Name:          "Gemini 1.5 Flash",
				Provider:      "vertex",
				SupportsTools: true,
				ContextWindow: 1000000,
				Description:   "Fast and efficient with tools",
			},
		}

	case "openrouter":
		return []ModelInfo{
			{
				ID:            "openai/gpt-4o-mini",
				Name:          "GPT-4o Mini (via OpenRouter)",
				Provider:      "openrouter",
				SupportsTools: true,
				Description:   "OpenAI model via OpenRouter",
			},
			{
				ID:            "anthropic/claude-sonnet-4-5",
				Name:          "Claude Sonnet 4.5 (via OpenRouter)",
				Provider:      "openrouter",
				SupportsTools: true,
				Description:   "Claude via OpenRouter",
			},
		}

	case "digitalocean":
		return []ModelInfo{
			{
				ID:            "gpt-4o-mini",
				Name:          "GPT-4o Mini",
				Provider:      "digitalocean",
				SupportsTools: false, // Cloud functions only
				Description:   "Agent endpoint (no local skills)",
			},
		}

	default:
		return []ModelInfo{}
	}
}

// getModelDisplayName returns a human-readable name.
func (s *ModelService) getModelDisplayName(modelID string) string {
	// Clean up provider prefixes
	name := strings.TrimPrefix(modelID, s.provider+"/")
	name = strings.TrimPrefix(name, "openai/")
	name = strings.TrimPrefix(name, "anthropic/")

	// Capitalize and format
	name = strings.ReplaceAll(name, "-", " ")
	// Use cases.Title instead of deprecated strings.Title
	caser := cases.Title(language.English)
	name = caser.String(name)

	return name
}

// getModelDescription returns model description based on ID.
func (s *ModelService) getModelDescription(modelID string) string {
	// Check static models for description
	static := s.getStaticModels()
	for _, m := range static {
		if m.ID == modelID {
			return m.Description
		}
	}

	// Generate description based on model name patterns
	lower := strings.ToLower(modelID)

	if strings.Contains(lower, "mini") {
		return "Fast and affordable"
	}
	if strings.Contains(lower, "turbo") {
		return "Optimized for speed"
	}
	if strings.Contains(lower, "fast") {
		return "High-speed model"
	}
	if strings.Contains(lower, "opus") {
		return "Most capable model"
	}
	if strings.Contains(lower, "sonnet") {
		return "Balanced performance"
	}
	if strings.Contains(lower, "uncensored") {
		return "Uncensored content"
	}

	return "Available model"
}

// sortModelsByCapability sorts models with tool support first.
func sortModelsByCapability(models []ModelInfo) {
	// Simple bubble sort: tool models first
	for i := 0; i < len(models)-1; i++ {
		for j := 0; j < len(models)-i-1; j++ {
			// If current doesn't support tools but next does, swap
			if !models[j].SupportsTools && models[j+1].SupportsTools {
				models[j], models[j+1] = models[j+1], models[j]
			}
		}
	}
}

// FormatModelList returns a formatted string for display.
func FormatModelList(models []ModelInfo, highlightToolModels bool) string {
	var toolModels []string
	var otherModels []string

	for _, m := range models {
		line := fmt.Sprintf("  %s", m.ID)
		if m.Description != "" {
			line += fmt.Sprintf(" - %s", m.Description)
		}
		if m.ContextWindow > 0 {
			line += fmt.Sprintf(" (%dk context)", m.ContextWindow/1000)
		}

		if m.SupportsTools {
			if highlightToolModels {
				toolModels = append(toolModels, "✓ "+line)
			} else {
				toolModels = append(toolModels, line)
			}
		} else {
			otherModels = append(otherModels, line+" (no skills)")
		}
	}

	var result strings.Builder

	if len(toolModels) > 0 {
		if highlightToolModels {
			result.WriteString("Function Calling Enabled (Skills Available):\n")
		}
		for _, m := range toolModels {
			result.WriteString(m + "\n")
		}
	}

	if len(otherModels) > 0 {
		if len(toolModels) > 0 {
			result.WriteString("\n")
		}
		if highlightToolModels {
			result.WriteString("Other Models (Skills Disabled):\n")
		}
		for _, m := range otherModels {
			result.WriteString(m + "\n")
		}
	}

	return result.String()
}
