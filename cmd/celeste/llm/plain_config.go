package llm

import "github.com/whykusanagi/celeste-cli/cmd/celeste/config"

// Plain returns a copy of c for a one-shot completion that carries its own
// system prompt (a summary, an oracle question): no xAI collections or
// features, and SkipPersonaPrompt cleared, because the Google backend sends
// no system instruction at all when it is set. c is not modified.
func (c *Config) Plain() *Config {
	p := *c
	p.SkipPersonaPrompt = false
	p.Collections = nil
	p.XAIFeatures = nil
	return &p
}

// PlainConfigFrom is ConfigFrom(cfg).Plain(): the client config for a
// non-persona one-shot client built from the user's configuration.
func PlainConfigFrom(cfg *config.Config) *Config {
	return ConfigFrom(cfg).Plain()
}
