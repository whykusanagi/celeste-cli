package config

import (
	"context"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/providers"
)

// ResolveServedModels replaces Model, AgentModel and SmallModel with what the provider
// serves now when either is retired (providers.ResolveModel), loading the
// provider's catalog (the cache, or one fetch bounded by its timeout). It
// changes this in-memory config only; nothing is saved. The notes say what
// was replaced.
func (c *Config) ResolveServedModels(ctx context.Context) (notes []string) {
	provider := providers.DetectProvider(c.BaseURL)
	cat, ok := providers.LoadCatalog(ctx, provider, c.BaseURL, c.APIKey)
	model, note := providers.ResolveModel(provider, c.Model, cat, ok)
	c.Model = model
	if note != "" {
		notes = append(notes, note)
	}
	for _, field := range []*string{&c.AgentModel, &c.SmallModel} {
		if *field == "" {
			continue
		}
		resolved, note := providers.ResolveModel(provider, *field, cat, ok)
		*field = resolved
		if note != "" {
			notes = append(notes, note)
		}
	}
	return notes
}
