// Package providers handles LLM provider model listing and management.
package providers

import (
	"sort"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// ModelService handles model listing and metadata.
type ModelService struct {
	apiKey   string
	baseURL  string
	provider string
	detector *ModelDetection
}

// NewModelService creates a new model service for a provider.
func NewModelService(apiKey, baseURL, provider string) *ModelService {
	return &ModelService{
		apiKey:   apiKey,
		baseURL:  baseURL,
		provider: provider,
		detector: NewModelDetection(provider),
	}
}

// ModelInfosFromCatalog turns a catalog into display rows, tool-capable
// models first. Tool support comes from the catalog when it says, else from
// the name heuristic.
func ModelInfosFromCatalog(provider string, cat []CatalogModel) []ModelInfo {
	s := NewModelService("", "", provider)
	result := make([]ModelInfo, 0, len(cat))
	for _, m := range cat {
		tools := s.detector.SupportsTools(m.ID)
		if m.Tools != nil {
			tools = *m.Tools
		}
		desc := s.getModelDescription(m.ID)
		if m.Default {
			desc = "Provider default — " + desc
		}
		result = append(result, ModelInfo{
			ID:                     m.ID,
			Name:                   s.getModelDisplayName(m.ID),
			Provider:               provider,
			SupportsTools:          tools,
			Description:            desc,
			OrchestratesServerSide: OrchestratesServerSide(provider, m.ID),
		})
	}
	sortModelsByCapability(result)
	return result
}

// StaticModels is the offline model list for a provider, used when no
// catalog is loaded.
func StaticModels(provider string) []ModelInfo {
	return NewModelService("", "", provider).getStaticModels()
}

// getStaticModels returns hardcoded model list when API isn't available.
func (s *ModelService) getStaticModels() []ModelInfo {
	switch s.provider {
	case "grok":
		models := []ModelInfo{
			{
				ID:            "grok-4.20-0309-non-reasoning",
				Name:          "Grok 4.20 (non-reasoning)",
				Provider:      "grok",
				SupportsTools: true,
				ContextWindow: 1000000,
				Description:   "Default — reliable tool calling, no reasoning-token burn, never routes to the cost-prohibitive grok-4.3 (#51)",
			},
			{
				ID:            "grok-build-0.1",
				Name:          "Grok Build 0.1",
				Provider:      "grok",
				SupportsTools: true,
				ContextWindow: 256000,
				Description:   "Grok code variant (256K). Note: still emits reasoning tokens.",
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
		for i := range models {
			models[i].OrchestratesServerSide = OrchestratesServerSide(models[i].Provider, models[i].ID)
		}
		return models

	case "openai":
		models := []ModelInfo{
			{
				ID:            "gpt-4.1-nano",
				Name:          "GPT-4o Mini",
				Provider:      "openai",
				SupportsTools: true,
				ContextWindow: 128000,
				Description:   "Fast, affordable, smart for everyday tasks",
			},
			{
				ID:            "gpt-4.1",
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
		for i := range models {
			models[i].OrchestratesServerSide = OrchestratesServerSide(models[i].Provider, models[i].ID)
		}
		return models

	case "venice":
		models := []ModelInfo{
			{
				ID:            "venice-uncensored-1-2",
				Name:          "Venice Uncensored 1.2",
				Provider:      "venice",
				SupportsTools: true,
				Description:   "NSFW uncensored chat, Venice's default (offline fallback)",
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
		for i := range models {
			models[i].OrchestratesServerSide = OrchestratesServerSide(models[i].Provider, models[i].ID)
		}
		return models

	case "anthropic":
		models := []ModelInfo{
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
		for i := range models {
			models[i].OrchestratesServerSide = OrchestratesServerSide(models[i].Provider, models[i].ID)
		}
		return models

	case "vertex":
		models := []ModelInfo{
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
		for i := range models {
			models[i].OrchestratesServerSide = OrchestratesServerSide(models[i].Provider, models[i].ID)
		}
		return models

	case "openrouter":
		models := []ModelInfo{
			{
				ID:            "openai/gpt-4.1-nano",
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
		for i := range models {
			models[i].OrchestratesServerSide = OrchestratesServerSide(models[i].Provider, models[i].ID)
		}
		return models

	case "sakana":
		models := []ModelInfo{
			{
				ID:            "fugu",
				Name:          "Fugu",
				Provider:      "sakana",
				SupportsTools: true,
				ContextWindow: 1000000,
				Description:   "Default — 1M context, high-effort deep reasoning",
			},
			{
				ID:            "fugu-ultra",
				Name:          "Fugu Ultra",
				Provider:      "sakana",
				SupportsTools: true,
				ContextWindow: 1000000,
				Description:   "1M context, deep reasoning with reasoning summaries",
			},
			{
				ID:            "fugu-ultra-20260615",
				Name:          "Fugu Ultra (2026-06-15)",
				Provider:      "sakana",
				SupportsTools: true,
				ContextWindow: 1000000,
				Description:   "Dated alias pin of fugu-ultra",
			},
			{
				ID:            "fugu-ultra-v1.0",
				Name:          "Fugu Ultra v1.0",
				Provider:      "sakana",
				SupportsTools: true,
				ContextWindow: 1000000,
				Description:   "Versioned Ultra release — high/xhigh reasoning effort",
			},
			{
				ID:            "fugu-ultra-v1.1",
				Name:          "Fugu Ultra v1.1",
				Provider:      "sakana",
				SupportsTools: true,
				ContextWindow: 1000000,
				Description:   "Latest Ultra — adds 'max' reasoning effort above xhigh",
			},
		}
		for i := range models {
			models[i].OrchestratesServerSide = OrchestratesServerSide(models[i].Provider, models[i].ID)
		}
		return models

	case "digitalocean":
		models := []ModelInfo{
			{
				ID:            "gpt-4.1-nano",
				Name:          "GPT-4o Mini",
				Provider:      "digitalocean",
				SupportsTools: false, // Cloud functions only
				Description:   "Agent endpoint (no local skills)",
			},
		}
		for i := range models {
			models[i].OrchestratesServerSide = OrchestratesServerSide(models[i].Provider, models[i].ID)
		}
		return models

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
	sort.SliceStable(models, func(i, j int) bool {
		return models[i].SupportsTools && !models[j].SupportsTools
	})
}
