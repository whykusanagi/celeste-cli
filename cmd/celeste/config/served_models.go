package config

import (
	"context"
	"os"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/providers"
)

// ModelPinned reports whether model resolution is off for this config:
// "pin_model": true in the config, or CELESTE_PIN_MODEL=1 in the
// environment. A pinned model is sent as configured, served or not.
func (c *Config) ModelPinned() bool {
	return c.PinModel || os.Getenv("CELESTE_PIN_MODEL") == "1"
}

// ResolveServedModels replaces Model, AgentModel and SmallModel with what
// the provider serves now, but only those celeste is sure are gone
// (providers.ResolveFromMemory). It loads the provider's catalog first (the
// cache, or one bounded fetch) and checks unlisted models where the provider
// can answer, so it may block on the network. It changes this in-memory
// config only; nothing is saved. The notes say what was replaced.
func (c *Config) ResolveServedModels(ctx context.Context) (notes []string) {
	if c.ModelPinned() {
		return nil
	}
	provider := providers.DetectProvider(c.BaseURL)
	providers.PrepareModels(ctx, provider, c.BaseURL, c.APIKey, c.Model, c.AgentModel, c.SmallModel)
	notes, _ = c.ResolveServedModelsCached()
	return notes
}

// ResolveServedModelsCached is ResolveServedModels from what this process
// already knows, with no I/O, for the TUI's Update. pending is true when a
// ResolveServedModels (in a tea.Cmd) would know more.
func (c *Config) ResolveServedModelsCached() (notes []string, pending bool) {
	if c.ModelPinned() {
		return nil, false
	}
	provider := providers.DetectProvider(c.BaseURL)
	model, note, p := providers.ResolveFromMemory(provider, c.BaseURL, c.APIKey, c.Model)
	c.Model, pending = model, p
	if note != "" {
		notes = append(notes, note)
	}
	for _, field := range []*string{&c.AgentModel, &c.SmallModel} {
		if *field == "" {
			continue
		}
		resolved, note, p := providers.ResolveFromMemory(provider, c.BaseURL, c.APIKey, *field)
		*field = resolved
		pending = pending || p
		if note != "" {
			notes = append(notes, note)
		}
	}
	return notes, pending
}

// ResolveServedModel resolves one model on its own endpoint (an
// orchestrator lane's), unless this config pins models. It may block on the
// network.
func (c *Config) ResolveServedModel(ctx context.Context, baseURL, apiKey, model string) (resolved, note string) {
	if c.ModelPinned() || model == "" {
		return model, ""
	}
	provider := providers.DetectProvider(baseURL)
	providers.PrepareModels(ctx, provider, baseURL, apiKey, model)
	resolved, note, _ = providers.ResolveFromMemory(provider, baseURL, apiKey, model)
	return resolved, note
}
